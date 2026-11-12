// Package graph counts citations whose source and target both belong to a fixed
// corpus Q. Country and discipline expansion deliberately happens afterwards.
package graph

import (
	"context"
	"errors"
	"fmt"
	"math"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"

	"openalexstats/internal/model"
)

const DefaultMaxCounterBytes uint64 = 8 << 30

// Index is an immutable, exact membership index. Dense ordinals follow the
// supplied ID order, so output does not depend on worker scheduling.
type Index struct {
	ids []uint64
	ord map[uint64]uint32
	// Parallel constructors populate independent immutable shard maps instead
	// of ord; every map stores the original global ordinal.
	shards          []map[uint64]uint32
	shardMask       uint64
	shardPowerOfTwo bool
}

func NewIndex(ids []uint64) (*Index, error) {
	return NewIndexContext(context.Background(), ids)
}

// NewIndexContext checks cancellation during construction. Map/slice allocation
// itself is a runtime operation and cannot be interrupted. Callers should check
// EstimateIndexBytes against their global memory budget before invoking it.
func NewIndexContext(ctx context.Context, ids []uint64) (*Index, error) {
	if ctx == nil {
		return nil, errors.New("graph: nil index context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if uint64(len(ids)) > math.MaxUint32 {
		return nil, errors.New("graph: corpus exceeds uint32 dense ordinal capacity")
	}
	idx := &Index{ids: make([]uint64, len(ids)), ord: make(map[uint64]uint32, len(ids))}
	for i, id := range ids {
		if i%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if id == 0 {
			return nil, errors.New("graph: zero is not a valid work ID")
		}
		if _, exists := idx.ord[id]; exists {
			return nil, fmt.Errorf("graph: duplicate corpus work ID W%d", id)
		}
		idx.ord[id] = uint32(i)
		idx.ids[i] = id
	}
	return idx, nil
}

func (i *Index) Len() int { return len(i.ids) }

// IDs returns a read-only view, avoiding a multi-gigabyte copy for full corpora.
// Callers must not mutate the returned slice.
func (i *Index) IDs() []uint64 { return i.ids }

func (i *Index) Lookup(id uint64) (int, bool) {
	if len(i.shards) != 0 {
		h := mixIndexID(id)
		var shard uint64
		if i.shardPowerOfTwo {
			shard = h & i.shardMask
		} else {
			shard = h % uint64(len(i.shards))
		}
		o, ok := i.shards[shard][id]
		return int(o), ok
	}
	o, ok := i.ord[id]
	return int(o), ok
}

// EstimatedBytes is a planning estimate, not an RSS or allocation guarantee.
// It includes the ID vector and conservative room for Go's numeric-key map.
func (i *Index) EstimatedBytes() uint64 {
	return uint64(len(i.ids))*40 + uint64(len(i.shards))*parallelShardOverhead
}

// EstimateIndexBytes is an intentionally conservative planning estimate for the
// numeric-key map and owned ID vector, not a guarantee on runtime RSS. The
// caller's original ID vector and runtime overhead remain separate allocations.
func EstimateIndexBytes(n int) (uint64, error) {
	if n < 0 || uint64(n) > math.MaxUint32 {
		return 0, errors.New("graph: invalid corpus size for uint32 dense ordinals")
	}
	return uint64(n) * 40, nil
}

type Config struct {
	Workers          int
	YearStart        int
	YearEnd          int
	MaxCounterBytes  uint64 // zero selects the conservative 8 GiB default
	ExcludeSelfLoops bool   // same-work loops, not author self-citations
}

// Stats separates raw reference slots from unique, retained directed edges.
// Reference-slot diagnostics concern only Q-member sources in the year window.
type Stats struct {
	SourceRecords           uint64 `json:"source_records"`
	MemberSourceRecords     uint64 `json:"member_source_records"`
	ValidYearSourceRecords  uint64 `json:"valid_year_source_records"`
	InvalidSourceIDs        uint64 `json:"invalid_source_ids"`
	OutsideSourceCorpus     uint64 `json:"outside_source_corpus"`
	InvalidSourceYears      uint64 `json:"invalid_source_years"`
	OutsideSourceYearWindow uint64 `json:"outside_source_year_window"`
	ReferenceSlots          uint64 `json:"reference_slots"`
	InvalidReferenceIDs     uint64 `json:"invalid_reference_ids"`
	DuplicateReferences     uint64 `json:"duplicate_references"`
	OutsideTargetCorpus     uint64 `json:"outside_target_corpus"`
	RetainedEdges           uint64 `json:"retained_edges"`
	SelfLoops               uint64 `json:"self_loops"`
	CounterBytes            uint64 `json:"counter_bytes"`
	SeenSourceBytes         uint64 `json:"seen_source_bytes"`
}

// Counts is immutable after Count returns successfully.
type Counts struct {
	index     *Index
	yearStart int
	years     int
	cells     []uint64
	stats     Stats
}

func (c *Counts) Stats() Stats { return c.stats }

func (c *Counts) YearRange() (start, end int) {
	return c.yearStart, c.yearStart + c.years - 1
}

// YearCounts returns a read-only, allocation-free view for one dense ordinal.
// The returned years run inclusively from YearRange's start to end. An invalid
// ordinal returns nil. Callers must not mutate this immutable completed result.
func (c *Counts) YearCounts(ordinal int) []uint64 {
	if ordinal < 0 || ordinal >= c.index.Len() {
		return nil
	}
	return c.cells[ordinal*c.years : (ordinal+1)*c.years]
}

func (c *Counts) Count(id uint64, year int) uint64 {
	o, ok := c.index.Lookup(id)
	if !ok || year < c.yearStart || year-c.yearStart >= c.years {
		return 0
	}
	return c.cells[int(o)*c.years+year-c.yearStart]
}

// ForEachNonZero walks corpus order followed by ascending year, without
// materializing a potentially enormous list of nonzero cells.
func (c *Counts) ForEachNonZero(yield func(id uint64, year int, count uint64) error) error {
	if yield == nil {
		return errors.New("graph: nil nonzero callback")
	}
	for o, id := range c.index.ids {
		for y := 0; y < c.years; y++ {
			if n := c.cells[o*c.years+y]; n != 0 {
				if err := yield(id, c.yearStart+y, n); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// DuplicateSourceError prevents retried shards or duplicate input records from
// silently counting the same source's references more than once.
type DuplicateSourceError struct{ ID uint64 }

func (e *DuplicateSourceError) Error() string {
	return fmt.Sprintf("graph: duplicate source work W%d; input must be exactly once", e.ID)
}

// Count consumes source works until sources closes, cancellation, or failure.
// Sources outside index are ignored; targets outside index are also ignored.
// Every Q-member source must occur at most once, even when its year is invalid.
//
// Each Work's References must remain immutable while workers use it: a scanner
// that reuses callback slices must Clone before sending. The caller owns the
// input channel and must cancel its producer if Count returns before channel
// exhaustion; Count never closes or drains caller-owned channels on error.
//
// The dense uint64 counter array is a correctness baseline. MaxCounterBytes
// covers the array plus exactly-once bitset, but excludes Index, input queues,
// worker reference scratch, runtime overhead, and country/discipline dimensions.
func Count(ctx context.Context, index *Index, sources <-chan model.Work, cfg Config) (*Counts, error) {
	if ctx == nil || index == nil || sources == nil {
		return nil, errors.New("graph: context, index and sources must be non-nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.YearStart < 1 || cfg.YearEnd < cfg.YearStart {
		return nil, errors.New("graph: invalid inclusive citation-year window")
	}
	years64 := uint64(cfg.YearEnd) - uint64(cfg.YearStart) + 1
	cellBytes, seenBytes, err := requiredBytes(uint64(index.Len()), years64)
	if err != nil {
		return nil, err
	}
	limit := cfg.MaxCounterBytes
	if limit == 0 {
		limit = DefaultMaxCounterBytes
	}
	if cellBytes > limit || seenBytes > limit-cellBytes {
		return nil, fmt.Errorf("graph: counters need %d bytes plus %d seen-source bytes, limit %d", cellBytes, seenBytes, limit)
	}
	workers := cfg.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers > 1024 {
		return nil, errors.New("graph: workers exceeds supported limit of 1024")
	}
	years := int(years64)
	result := &Counts{index: index, yearStart: cfg.YearStart, years: years, cells: make([]uint64, cellBytes/8)}
	seen := make([]uint64, seenBytes/8)
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	var firstErr error
	var failOnce sync.Once
	localStats := make([]Stats, workers)
	fail := func(err error) {
		failOnce.Do(func() { firstErr = err; cancel() })
	}
	for worker := 0; worker < workers; worker++ {
		wg.Go(func() {
			s := &localStats[worker]
			var scratch []uint64
			for {
				select {
				case <-child.Done():
					return
				case w, ok := <-sources:
					if !ok {
						return
					}
					s.SourceRecords++
					if w.ID == 0 {
						s.InvalidSourceIDs++
						continue
					}
					sourceOrd, member := index.Lookup(w.ID)
					if !member {
						s.OutsideSourceCorpus++
						continue
					}
					s.MemberSourceRecords++
					mask := uint64(1) << (sourceOrd % 64)
					if atomic.OrUint64(&seen[sourceOrd/64], mask)&mask != 0 {
						fail(&DuplicateSourceError{ID: w.ID})
						return
					}
					if w.Year <= 0 {
						s.InvalidSourceYears++
						continue
					}
					if w.Year < cfg.YearStart || w.Year > cfg.YearEnd {
						s.OutsideSourceYearWindow++
						continue
					}
					s.ValidYearSourceRecords++
					s.ReferenceSlots += uint64(len(w.References))
					refs := w.References
					if !slices.IsSorted(refs) {
						scratch = append(scratch[:0], refs...)
						slices.Sort(scratch)
						refs = scratch
					}
					var previous uint64
					for j, targetID := range refs {
						if j%1024 == 0 && child.Err() != nil {
							return
						}
						if targetID == 0 {
							s.InvalidReferenceIDs++
							continue
						}
						if targetID == previous {
							s.DuplicateReferences++
							continue
						}
						previous = targetID
						if targetID == w.ID {
							s.SelfLoops++
							if cfg.ExcludeSelfLoops {
								continue
							}
						}
						targetOrd, member := index.Lookup(targetID)
						if !member {
							s.OutsideTargetCorpus++
							continue
						}
						cell := int(targetOrd)*years + w.Year - cfg.YearStart
						if atomic.AddUint64(&result.cells[cell], 1) == 0 {
							fail(errors.New("graph: uint64 citation counter overflow"))
							return
						}
						s.RetainedEdges++
					}
				}
			}
		})
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, s := range localStats {
		result.stats.add(s)
	}
	result.stats.CounterBytes = cellBytes
	result.stats.SeenSourceBytes = seenBytes
	return result, nil
}

func requiredBytes(n, years uint64) (uint64, uint64, error) {
	maxInt := uint64(int(^uint(0) >> 1))
	if years > maxInt || (years != 0 && n > maxInt/years/8) {
		return 0, 0, errors.New("graph: counter dimensions overflow addressable memory")
	}
	return n * years * 8, ((n + 63) / 64) * 8, nil
}

func (s *Stats) add(x Stats) {
	s.SourceRecords += x.SourceRecords
	s.MemberSourceRecords += x.MemberSourceRecords
	s.ValidYearSourceRecords += x.ValidYearSourceRecords
	s.InvalidSourceIDs += x.InvalidSourceIDs
	s.OutsideSourceCorpus += x.OutsideSourceCorpus
	s.InvalidSourceYears += x.InvalidSourceYears
	s.OutsideSourceYearWindow += x.OutsideSourceYearWindow
	s.ReferenceSlots += x.ReferenceSlots
	s.InvalidReferenceIDs += x.InvalidReferenceIDs
	s.DuplicateReferences += x.DuplicateReferences
	s.OutsideTargetCorpus += x.OutsideTargetCorpus
	s.RetainedEdges += x.RetainedEdges
	s.SelfLoops += x.SelfLoops
}
