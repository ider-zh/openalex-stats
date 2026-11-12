package production

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"openalexstats/internal/cache"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/graph"
	"openalexstats/internal/model"
	"openalexstats/internal/policy"
	"openalexstats/internal/scan"
	"openalexstats/internal/store"
)

type EdgeStats struct {
	graph.Stats
	// These are reference-edge counts, not distinct target-work counts.
	MissingTargetWorks  uint64 `json:"missing_target_references"`
	ExcludedTargetWorks uint64 `json:"excluded_target_references"`
	FutureTargetEdges   uint64 `json:"future_target_edges"`
}

func (s *EdgeStats) add(o EdgeStats) {
	s.SourceRecords += o.SourceRecords
	s.MemberSourceRecords += o.MemberSourceRecords
	s.ValidYearSourceRecords += o.ValidYearSourceRecords
	s.InvalidSourceIDs += o.InvalidSourceIDs
	s.OutsideSourceCorpus += o.OutsideSourceCorpus
	s.InvalidSourceYears += o.InvalidSourceYears
	s.OutsideSourceYearWindow += o.OutsideSourceYearWindow
	s.ReferenceSlots += o.ReferenceSlots
	s.InvalidReferenceIDs += o.InvalidReferenceIDs
	s.DuplicateReferences += o.DuplicateReferences
	s.OutsideTargetCorpus += o.OutsideTargetCorpus
	s.RetainedEdges += o.RetainedEdges
	s.SelfLoops += o.SelfLoops
	s.MissingTargetWorks += o.MissingTargetWorks
	s.ExcludedTargetWorks += o.ExcludedTargetWorks
	s.FutureTargetEdges += o.FutureTargetEdges
}

type EdgePart struct {
	Version         int                 `json:"version"`
	Key             string              `json:"key"`
	Source          string              `json:"source"`
	Artifact        checkpoint.Artifact `json:"artifact"`
	PartitionCounts []uint64            `json:"partition_counts"`
	Stats           EdgeStats           `json:"stats"`
	Scan            scan.Stats          `json:"scan"`
}
type EdgeManifest struct {
	Version     int        `json:"version"`
	Key         string     `json:"key"`
	Partitions  int        `json:"partitions"`
	YearStart   int        `json:"year_start"`
	YearEnd     int        `json:"year_end"`
	Parts       []EdgePart `json:"parts"`
	Stats       EdgeStats  `json:"stats"`
	CompletedAt time.Time  `json:"completed_at"`
	ReusedParts int        `json:"reused_parts"`
}
type EdgeOptions struct {
	Directory, Key                          string
	Workers, Partitions, YearStart, YearEnd int
	Scope                                   policy.Scope
	ExcludeSelfLoops                        bool
	// AllIndex distinguishes absent snapshot targets from explicitly excluded Q.
	AllIndex           *graph.Index
	Columns            *store.Columns
	MaxPartBufferBytes uint64
}
type Progress func(stage string, detail any)

// ProjectEdges stores one durable source-file artifact, bucketed by target
// ordinal. Target reducers later read each byte once across all partitions.
func ProjectEdges(ctx context.Context, paths []string, index *graph.Index, opts EdgeOptions, progress Progress) (EdgeManifest, error) {
	out := EdgeManifest{Version: 1, Key: opts.Key, Partitions: opts.Partitions, YearStart: opts.YearStart, YearEnd: opts.YearEnd, Parts: make([]EdgePart, len(paths))}
	if opts.Workers < 1 || opts.Partitions < 1 || opts.Partitions > 256 || opts.YearEnd < opts.YearStart || opts.YearEnd-opts.YearStart > 255 {
		return out, fmt.Errorf("invalid edge projection settings")
	}
	if progress == nil {
		progress = func(string, any) {}
	}
	if opts.MaxPartBufferBytes == 0 {
		opts.MaxPartBufferBytes = 1 << 30
	}
	if e := os.MkdirAll(opts.Directory, 0755); e != nil {
		return out, e
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var remaining atomic.Int64
	remaining.Store(int64(len(paths)))
	progress("edge_queue", map[string]any{"workers": min(opts.Workers, len(paths)), "queue_capacity": 0, "unfinished_parts": len(paths), "note": "unbuffered dispatch; decoder work is bounded by workers"})
	errs := make([]error, len(paths))
	reused := make([]bool, len(paths))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < opts.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					errs[i] = ctx.Err()
					continue
				}
				partPath := filepath.Join(opts.Directory, fmt.Sprintf("source-%05d.edges", i))
				metaPath := partPath + ".json"
				info, e := os.Stat(paths[i])
				if e != nil {
					errs[i] = e
					cancel()
					continue
				}
				partKey, e := checkpoint.Key(struct {
					Key, Source    string
					Size, Modified int64
				}{opts.Key, paths[i], info.Size(), info.ModTime().UnixNano()})
				if e != nil {
					errs[i] = e
					cancel()
					continue
				}
				var old EdgePart
				if checkpoint.ReadJSON(metaPath, &old) == nil && old.Version == 1 && old.Key == partKey && len(old.PartitionCounts) == opts.Partitions && old.Artifact.Path == partPath && old.Artifact.Verify() == nil {
					out.Parts[i] = old
					reused[i] = true
					progress("edges_reused", map[string]any{"part": i, "edges": old.Stats.RetainedEdges, "unfinished_parts": remaining.Add(-1)})
					continue
				}
				part, e := projectPart(ctx, paths[i], partPath, partKey, index, opts)
				if e == nil {
					e = checkpoint.JSON(metaPath, part)
				}
				if e != nil {
					errs[i] = e
					cancel()
					continue
				}
				out.Parts[i] = part
				progress("edges_written", map[string]any{"part": i, "edges": part.Stats.RetainedEdges, "bytes": part.Artifact.Bytes, "unfinished_parts": remaining.Add(-1)})
			}
		}()
	}
	for i := range paths {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			for _, e := range errs {
				if e != nil {
					return out, e
				}
			}
			return out, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return out, e
		}
	}
	for i, p := range out.Parts {
		out.Stats.add(p.Stats)
		if reused[i] {
			out.ReusedParts++
		}
	}
	out.CompletedAt = time.Now().UTC()
	if e := checkpoint.JSON(filepath.Join(opts.Directory, "manifest.json"), out); e != nil {
		return out, e
	}
	return out, nil
}

func projectPart(ctx context.Context, path, destination, key string, index *graph.Index, opts EdgeOptions) (EdgePart, error) {
	part := EdgePart{Version: 1, Key: key, Source: path, PartitionCounts: make([]uint64, opts.Partitions)}
	var records []uint64
	var scratch []uint64
	var rowQuality scan.RowQuality
	stats, e := cache.Scan(ctx, []string{path}, scan.Options{ReadReferences: true, ReaderWorkers: 1, OnQuality: func(q scan.RowQuality) { rowQuality = q }}, func(w model.Work) error {
		part.Stats.SourceRecords++
		if !opts.Scope.Includes(w) || (opts.Scope.Corpus == "core" && rowQuality.NullXPAC) {
			part.Stats.OutsideSourceCorpus++
			return nil
		}
		part.Stats.MemberSourceRecords++
		if _, ok := index.Lookup(w.ID); !ok {
			return fmt.Errorf("source Q disagrees with dimension index: W%d", w.ID)
		}
		if w.Year <= 0 {
			part.Stats.InvalidSourceYears++
			return nil
		}
		if w.Year < opts.YearStart || w.Year > opts.YearEnd {
			part.Stats.OutsideSourceYearWindow++
			return nil
		}
		part.Stats.ValidYearSourceRecords++
		part.Stats.ReferenceSlots += uint64(len(w.References)) + uint64(rowQuality.InvalidReferenceIDs)
		part.Stats.InvalidReferenceIDs += uint64(rowQuality.InvalidReferenceIDs)
		refs := w.References
		if !slices.IsSorted(refs) {
			scratch = append(scratch[:0], refs...)
			slices.Sort(scratch)
			refs = scratch
		}
		var previous uint64
		for j, id := range refs {
			if j%4096 == 0 {
				if e := ctx.Err(); e != nil {
					return e
				}
			}
			if id == 0 {
				part.Stats.InvalidReferenceIDs++
				continue
			}
			if id == previous {
				part.Stats.DuplicateReferences++
				continue
			}
			previous = id
			if id == w.ID {
				part.Stats.SelfLoops++
				if opts.ExcludeSelfLoops {
					continue
				}
			}
			ordinal, ok := index.Lookup(id)
			if !ok {
				part.Stats.OutsideTargetCorpus++
				if opts.AllIndex != nil {
					if _, exists := opts.AllIndex.Lookup(id); exists {
						part.Stats.ExcludedTargetWorks++
					} else {
						part.Stats.MissingTargetWorks++
					}
				}
				continue
			}
			if opts.Columns != nil && opts.Columns.YearAt(ordinal) > w.Year {
				part.Stats.FutureTargetEdges++
			}
			if uint64(len(records)+1)*16 > opts.MaxPartBufferBytes {
				return fmt.Errorf("source edge partition exceeds buffer budget; increase per-part budget or split source row groups")
			}
			records = append(records, uint64(ordinal)<<8|uint64(w.Year-opts.YearStart))
			part.PartitionCounts[ordinal%opts.Partitions]++
			part.Stats.RetainedEdges++
		}
		return nil
	})
	part.Scan = stats
	if e != nil {
		return part, e
	}
	// Stable counting distribution is linear, avoiding a comparison sort over
	// billions of edges. Two temporary numeric arrays fit the explicit budget.
	starts := make([]uint64, opts.Partitions)
	for p := 1; p < opts.Partitions; p++ {
		starts[p] = starts[p-1] + part.PartitionCounts[p-1]
	}
	cursor := append([]uint64(nil), starts...)
	bucketed := make([]uint64, len(records))
	for _, record := range records {
		p := (record >> 8) % uint64(opts.Partitions)
		bucketed[cursor[p]] = record
		cursor[p]++
	}
	records = nil
	part.Artifact, e = checkpoint.Write(destination, func(writer io.Writer) error {
		b := bufio.NewWriterSize(writer, 1<<20)
		buf := make([]byte, 64<<10)
		for start := 0; start < len(bucketed); {
			if e := ctx.Err(); e != nil {
				return e
			}
			n := min(len(buf)/8, len(bucketed)-start)
			for j := 0; j < n; j++ {
				binary.LittleEndian.PutUint64(buf[j*8:(j+1)*8], bucketed[start+j])
			}
			if _, e := b.Write(buf[:n*8]); e != nil {
				return e
			}
			start += n
		}
		return b.Flush()
	})
	return part, e
}
