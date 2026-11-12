package graph

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

type indexVariant struct {
	name            string
	workers, shards int
}

var benchmarkIndexVariants = []indexVariant{{"serial", 0, 0}, {"parallel_w8_s64", 8, 64}, {"parallel_w16_s64", 16, 64}, {"parallel_w32_s64", 32, 64}, {"parallel_w48_s64", 48, 64}, {"parallel_w32_s256", 32, 256}, {"parallel_w48_s512", 48, 512}}

func benchmarkIDs(b testing.TB) []uint64 {
	b.Helper()
	path := os.Getenv("OPENALEX_INDEX_IDS_FILE")
	n := 100000
	if path != "" {
		n = 1000000
	}
	if value := os.Getenv("OPENALEX_INDEX_SAMPLE_SIZE"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 50000000 {
			b.Fatal("OPENALEX_INDEX_SAMPLE_SIZE must be 1..50000000")
		}
		n = parsed
	}
	if path == "" {
		ids := make([]uint64, n)
		for i := range ids {
			ids[i] = mixIndexID(uint64(i + 1))
		}
		return ids
	}
	f, err := os.Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	var header [8]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		b.Fatal(err)
	}
	total := binary.LittleEndian.Uint64(header[:])
	info, err := f.Stat()
	if err != nil {
		b.Fatal(err)
	}
	if total > uint64((info.Size()-8)/8) || info.Size() < 8 || total == 0 {
		b.Fatal("invalid all-ids vector header/length")
	}
	if uint64(n) > total {
		n = int(total)
	}
	ids := make([]uint64, 0, n)
	// Read up to 32 nonoverlapping windows spanning the vector instead of
	// accidentally benchmarking only its earliest updated-date partition.
	windows := min(32, n)
	base, extra := n/windows, n%windows
	var consumed uint64
	for window := 0; window < windows; window++ {
		count := base
		if window < extra {
			count++
		}
		var start uint64
		if windows > 1 {
			start = uint64(window) * (total - uint64(count)) / uint64(windows-1)
		}
		if start < consumed {
			start = consumed
		}
		buffer := make([]byte, count*8)
		if _, err := f.ReadAt(buffer, int64(8+start*8)); err != nil {
			b.Fatal(err)
		}
		for i := 0; i < count; i++ {
			ids = append(ids, binary.LittleEndian.Uint64(buffer[i*8:(i+1)*8]))
		}
		consumed = start + uint64(count)
	}
	return ids
}

func buildBenchmarkIndex(ids []uint64, v indexVariant) (*Index, error) {
	if v.workers == 0 {
		return NewIndexContext(context.Background(), ids)
	}
	return NewParallelIndexContext(context.Background(), ids, v.workers, v.shards)
}

func BenchmarkIndexBuild(b *testing.B) {
	ids := benchmarkIDs(b)
	for _, v := range benchmarkIndexVariants {
		b.Run(v.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(ids)) * 8)
			b.ReportMetric(float64(len(ids)), "ids/op")
			for i := 0; i < b.N; i++ {
				idx, err := buildBenchmarkIndex(ids, v)
				if err != nil {
					b.Fatal(err)
				}
				if idx.Len() != len(ids) {
					b.Fatal("incomplete index")
				}
			}
		})
	}
}

var indexLookupSink atomic.Uint64

func BenchmarkIndexLookup(b *testing.B) {
	ids := benchmarkIDs(b)
	for _, v := range benchmarkIndexVariants {
		b.Run(v.name, func(b *testing.B) {
			idx, err := buildBenchmarkIndex(ids, v)
			if err != nil {
				b.Fatal(err)
			}
			misses := make([]uint64, 1024)
			for i := range misses {
				candidate := mixIndexID(uint64(i) + 0x123456789abcdef)
				if _, exists := idx.Lookup(candidate); exists {
					candidate = 0
				}
				misses[i] = candidate
			}
			b.Run("serial", func(b *testing.B) {
				b.ReportAllocs()
				var sum uint64
				var state uint64 = 1
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					state = state*6364136223846793005 + 1
					id := ids[state%uint64(len(ids))]
					if state&7 == 0 {
						id = misses[(state>>8)%uint64(len(misses))]
					}
					ordinal, ok := idx.Lookup(id)
					if ok {
						sum += uint64(ordinal) + 1
					}
				}
				indexLookupSink.Add(sum)
			})
			b.Run("concurrent", func(b *testing.B) {
				b.ReportAllocs()
				var seeds atomic.Uint64
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					var sum uint64
					state := seeds.Add(1) * 0x9e3779b97f4a7c15
					for pb.Next() {
						state = state*6364136223846793005 + 1
						id := ids[state%uint64(len(ids))]
						if state&7 == 0 {
							id = misses[(state>>8)%uint64(len(misses))]
						}
						ordinal, ok := idx.Lookup(id)
						if ok {
							sum += uint64(ordinal) + 1
						}
					}
					indexLookupSink.Add(sum)
				})
			})
			b.Log(fmt.Sprintf("ids=%d variant=%s estimated_final=%d", len(ids), v.name, idx.EstimatedBytes()))
		})
	}
}

func TestParallelIndexRealSample(t *testing.T) {
	if os.Getenv("OPENALEX_INDEX_IDS_FILE") == "" {
		t.Skip("explicit real-ID benchmark input not provided")
	}
	ids := benchmarkIDs(t)
	for _, v := range []indexVariant{{"serial", 0, 0}, {"parallel_w16_s64", 16, 64}, {"parallel_w48_s512", 48, 512}} {
		idx, err := buildBenchmarkIndex(ids, v)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		failure := make(chan error, 1)
		workers := min(runtime.GOMAXPROCS(0), len(ids))
		owned := idx.IDs()
		for worker := 0; worker < workers; worker++ {
			start, end := len(ids)*worker/workers, len(ids)*(worker+1)/workers
			wg.Go(func() {
				for ordinal := start; ordinal < end; ordinal++ {
					if ordinal%4096 == 0 && ctx.Err() != nil {
						return
					}
					id := ids[ordinal]
					got, ok := idx.Lookup(id)
					if !ok || got != ordinal || owned[ordinal] != id {
						select {
						case failure <- fmt.Errorf("%s W%d got=%d/%t want=%d", v.name, id, got, ok, ordinal):
						default:
						}
						cancel()
						return
					}
				}
			})
		}
		wg.Wait()
		cancel()
		select {
		case err := <-failure:
			t.Fatal(err)
		default:
		}
		if _, ok := idx.Lookup(0); ok {
			t.Fatal("invalid zero ID returned hit")
		}
		t.Logf("verified %d real IDs and original ordinals: %s", len(ids), v.name)
	}
}
