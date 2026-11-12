package production

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"openalexstats/internal/checkpoint"
)

type Counts struct {
	cells            []uint64
	years, rows      int
	Sum              uint64
	ReusedPartitions int
}

func (c *Counts) YearCounts(ordinal int) []uint64 {
	return c.cells[ordinal*c.years : (ordinal+1)*c.years]
}
func (c *Counts) Bytes() uint64 { return uint64(len(c.cells)) * 8 }

type CountOptions struct {
	Directory, Key string
	Workers        int
	MaxBytes       uint64
}
type counterPart struct {
	Version   int                 `json:"version"`
	Key       string              `json:"key"`
	Partition int                 `json:"partition"`
	Rows      int                 `json:"rows"`
	Years     int                 `json:"years"`
	Sum       uint64              `json:"sum"`
	Artifact  checkpoint.Artifact `json:"artifact"`
}

// ReduceCounts has one owner for each target partition, so the hot counter
// updates need no atomics. Completed target partitions are independently resumable.
func ReduceCounts(ctx context.Context, edges EdgeManifest, rows int, opts CountOptions, progress Progress) (*Counts, error) {
	years := edges.YearEnd - edges.YearStart + 1
	if rows < 0 || years < 1 || years > 256 || edges.Partitions < 1 || opts.Workers < 1 {
		return nil, fmt.Errorf("invalid counter dimensions")
	}
	bytes := uint64(rows) * uint64(years) * 8
	if bytes > opts.MaxBytes || bytes > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("dense counters require %d bytes, budget %d", bytes, opts.MaxBytes)
	}
	if progress == nil {
		progress = func(string, any) {}
	}
	if e := os.MkdirAll(opts.Directory, 0755); e != nil {
		return nil, e
	}
	counts := &Counts{cells: make([]uint64, bytes/8), years: years, rows: rows}
	// Bind checkpoints to actual source shard contents as well as the logical
	// graph key. Equal edge totals do not imply equal target distributions.
	type edgeIdentity struct {
		SHA256 string
		Bytes  int64
		Counts []uint64
	}
	identities := make([]edgeIdentity, len(edges.Parts))
	for i, p := range edges.Parts {
		identities[i] = edgeIdentity{p.Artifact.SHA256, p.Artifact.Bytes, p.PartitionCounts}
	}
	edgeDataKey, keyErr := checkpoint.Key(identities)
	if keyErr != nil {
		return nil, keyErr
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	expected := make([]uint64, edges.Partitions)
	for _, source := range edges.Parts {
		if len(source.PartitionCounts) != edges.Partitions {
			return nil, fmt.Errorf("edge partition shape mismatch")
		}
		for p, n := range source.PartitionCounts {
			expected[p] += n
		}
	}
	errs := make([]error, edges.Partitions)
	sums := make([]uint64, edges.Partitions)
	reused := make([]bool, edges.Partitions)
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < min(opts.Workers, edges.Partitions); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				if ctx.Err() != nil {
					errs[p] = ctx.Err()
					continue
				}
				path := filepath.Join(opts.Directory, fmt.Sprintf("target-%03d.counts", p))
				metaPath := path + ".json"
				key, e := checkpoint.Key(struct {
					Key, Edges             string
					Partition, Rows, Years int
				}{opts.Key, edges.Key + ":" + edgeDataKey, p, rows, years})
				if e != nil {
					errs[p] = e
					cancel()
					continue
				}
				var meta counterPart
				if checkpoint.ReadJSON(metaPath, &meta) == nil && meta.Version == 1 && meta.Key == key && meta.Partition == p && meta.Rows == rows && meta.Years == years && meta.Sum == expected[p] && meta.Artifact.Path == path && meta.Artifact.Verify() == nil {
					sum, e := readCounterPart(ctx, path, counts, p, edges.Partitions)
					if e == nil && sum == expected[p] {
						sums[p] = sum
						reused[p] = true
						progress("counts_reused", map[string]any{"partition": p, "sum": sum})
						continue
					}
					// Corrupt/invalid checkpoints are not trusted even if metadata is present.
					for i := p; i < rows; i += edges.Partitions {
						clear(counts.YearCounts(i))
					}
				}
				sum, e := reduceOne(ctx, edges, counts, p)
				if e == nil && sum != expected[p] {
					e = fmt.Errorf("counter partition %d sum %d != edge records %d", p, sum, expected[p])
				}
				if e == nil {
					meta = counterPart{Version: 1, Key: key, Partition: p, Rows: rows, Years: years, Sum: sum}
					meta.Artifact, e = checkpoint.Write(path, func(w io.Writer) error { return writeCounterPart(ctx, w, counts, p, edges.Partitions) })
				}
				if e == nil {
					e = checkpoint.JSON(metaPath, meta)
				}
				if e != nil {
					errs[p] = e
					cancel()
					continue
				}
				sums[p] = sum
				progress("counts_written", map[string]any{"partition": p, "sum": sum, "bytes": meta.Artifact.Bytes})
			}
		}()
	}
	for p := 0; p < edges.Partitions; p++ {
		select {
		case jobs <- p:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			for _, e := range errs {
				if e != nil {
					return nil, e
				}
			}
			return nil, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return nil, e
		}
	}
	for p, sum := range sums {
		counts.Sum += sum
		if reused[p] {
			counts.ReusedPartitions++
		}
	}
	if counts.Sum != edges.Stats.RetainedEdges {
		return nil, fmt.Errorf("whole citation graph conservation failed: counters=%d edges=%d", counts.Sum, edges.Stats.RetainedEdges)
	}
	if e := checkpoint.JSON(filepath.Join(opts.Directory, "manifest.json"), map[string]any{"version": 1, "key": opts.Key, "sum": counts.Sum, "rows": rows, "years": years, "partitions": edges.Partitions, "reused_partitions": counts.ReusedPartitions}); e != nil {
		return nil, e
	}
	return counts, nil
}

func reduceOne(ctx context.Context, edges EdgeManifest, counts *Counts, partition int) (uint64, error) {
	buf := make([]byte, 256<<10)
	var sum uint64
	for _, source := range edges.Parts {
		if e := ctx.Err(); e != nil {
			return 0, e
		}
		n := source.PartitionCounts[partition]
		if n == 0 {
			continue
		}
		var start uint64
		for p := 0; p < partition; p++ {
			start += source.PartitionCounts[p]
		}
		f, e := os.Open(source.Artifact.Path)
		if e != nil {
			return 0, e
		}
		section := io.NewSectionReader(f, int64(start*8), int64(n*8))
		remaining := n
		for remaining > 0 {
			if e = ctx.Err(); e != nil {
				_ = f.Close()
				return 0, e
			}
			take := min(remaining, uint64(len(buf)/8))
			if _, e = io.ReadFull(section, buf[:take*8]); e != nil {
				_ = f.Close()
				return 0, e
			}
			for i := uint64(0); i < take; i++ {
				record := binary.LittleEndian.Uint64(buf[i*8 : (i+1)*8])
				ordinal := record >> 8
				year := record & 255
				if ordinal >= uint64(counts.rows) || year >= uint64(counts.years) || ordinal%uint64(edges.Partitions) != uint64(partition) {
					_ = f.Close()
					return 0, fmt.Errorf("invalid bucketed edge record")
				}
				cell := ordinal*uint64(counts.years) + year
				counts.cells[cell]++
				if counts.cells[cell] == 0 {
					_ = f.Close()
					return 0, fmt.Errorf("uint64 citation overflow")
				}
				sum++
			}
			remaining -= take
		}
		if e = f.Close(); e != nil {
			return 0, e
		}
	}
	return sum, nil
}
func writeCounterPart(ctx context.Context, writer io.Writer, c *Counts, p, partitions int) error {
	w := bufio.NewWriterSize(writer, 1<<20)
	buf := make([]byte, c.years*8)
	for row := p; row < c.rows; row += partitions {
		if row%4096 < partitions {
			if e := ctx.Err(); e != nil {
				return e
			}
		}
		for y, v := range c.YearCounts(row) {
			binary.LittleEndian.PutUint64(buf[y*8:(y+1)*8], v)
		}
		if _, e := w.Write(buf); e != nil {
			return e
		}
	}
	return w.Flush()
}
func readCounterPart(ctx context.Context, path string, c *Counts, p, partitions int) (uint64, error) {
	f, e := os.Open(path)
	if e != nil {
		return 0, e
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	buf := make([]byte, c.years*8)
	var sum uint64
	for row := p; row < c.rows; row += partitions {
		if row%4096 < partitions {
			if e = ctx.Err(); e != nil {
				return 0, e
			}
		}
		if _, e = io.ReadFull(r, buf); e != nil {
			return 0, e
		}
		for y := 0; y < c.years; y++ {
			v := binary.LittleEndian.Uint64(buf[y*8 : (y+1)*8])
			c.YearCounts(row)[y] = v
			sum += v
		}
	}
	if _, e = r.ReadByte(); e != io.EOF {
		return 0, fmt.Errorf("trailing counter data")
	}
	return sum, nil
}
