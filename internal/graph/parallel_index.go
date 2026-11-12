package graph

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"unsafe"
)

const parallelShardOverhead uint64 = 256

type indexEntry struct {
	ID      uint64
	Ordinal uint32
}

// ParallelIndexMemory separates live index storage from temporary construction
// arrays. Estimates are conservative planning values, not RSS guarantees. The
// caller's original ids vector and unrelated runtime allocations are additional.
type ParallelIndexMemory struct {
	OwnedIDsBytes    uint64 `json:"owned_ids_bytes"`
	ShardMapsBytes   uint64 `json:"shard_maps_bytes"`
	EntryBufferBytes uint64 `json:"entry_buffer_bytes"`
	HistogramBytes   uint64 `json:"histogram_bytes"`
	PrefixBytes      uint64 `json:"prefix_bytes"`
	FinalBytes       uint64 `json:"final_bytes"`
	PeakBytes        uint64 `json:"peak_bytes"`
	EffectiveWorkers int    `json:"effective_workers"`
	Shards           int    `json:"shards"`
}

func EstimateParallelIndexBytes(n, workers, shards int) (ParallelIndexMemory, error) {
	var estimate ParallelIndexMemory
	if n < 0 || uint64(n) > math.MaxUint32 {
		return estimate, errors.New("graph: invalid corpus size for uint32 dense ordinals")
	}
	if workers < 1 || workers > 1024 || shards < 1 || shards > 65536 {
		return estimate, errors.New("graph: parallel index needs 1..1024 workers and 1..65536 shards")
	}
	workers = min(workers, max(1, n))
	estimate = ParallelIndexMemory{OwnedIDsBytes: uint64(n) * 8, ShardMapsBytes: uint64(n)*32 + uint64(shards)*parallelShardOverhead, EntryBufferBytes: uint64(n) * uint64(unsafe.Sizeof(indexEntry{})), HistogramBytes: uint64(workers) * uint64(shards) * 8, PrefixBytes: uint64(shards+1) * 8, EffectiveWorkers: workers, Shards: shards}
	estimate.FinalBytes = estimate.OwnedIDsBytes + estimate.ShardMapsBytes
	estimate.PeakBytes = estimate.FinalBytes + estimate.EntryBufferBytes + estimate.HistogramBytes + estimate.PrefixBytes
	return estimate, nil
}

// NewParallelIndexContext constructs an exact immutable index in O(N+W*S).
// Each worker counts a contiguous input range, prefix sums reserve disjoint
// scatter ranges, and each shard's map is then built by one owner. No shard
// rescans the whole corpus and no lock/atomic is used for map insertion.
func NewParallelIndexContext(ctx context.Context, ids []uint64, workers, shards int) (*Index, error) {
	if ctx == nil {
		return nil, errors.New("graph: nil index context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	estimate, err := EstimateParallelIndexBytes(len(ids), workers, shards)
	if err != nil {
		return nil, err
	}
	workers = estimate.EffectiveWorkers
	if estimate.OwnedIDsBytes > uint64(^uint(0)>>1) || estimate.EntryBufferBytes > uint64(^uint(0)>>1) || estimate.HistogramBytes > uint64(^uint(0)>>1) {
		return nil, errors.New("graph: parallel index arrays exceed platform allocation capacity")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	idx := &Index{ids: make([]uint64, len(ids)), shards: make([]map[uint64]uint32, shards), shardMask: uint64(shards - 1), shardPowerOfTwo: shards&(shards-1) == 0}
	// Each row belongs to one worker; after prefixing it becomes that worker's
	// exclusive scatter cursors, avoiding a second W*S allocation.
	hist := make([]uint64, workers*shards)
	var firstErr error
	var once sync.Once
	fail := func(e error) { once.Do(func() { firstErr = e; cancel() }) }
	ranges := func(work func(worker, start, end int)) {
		var wg sync.WaitGroup
		for worker := 0; worker < workers; worker++ {
			start, end := int(uint64(len(ids))*uint64(worker)/uint64(workers)), int(uint64(len(ids))*uint64(worker+1)/uint64(workers))
			wg.Go(func() { work(worker, start, end) })
		}
		wg.Wait()
	}
	ranges(func(worker, start, end int) {
		row := hist[worker*shards : (worker+1)*shards]
		for ordinal := start; ordinal < end; ordinal++ {
			if ordinal%4096 == 0 {
				if ctx.Err() != nil {
					return
				}
			}
			id := ids[ordinal]
			if id == 0 {
				fail(errors.New("graph: zero is not a valid work ID"))
				return
			}
			idx.ids[ordinal] = id
			row[parallelShard(id, shards)]++
		}
	})
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	starts := make([]uint64, shards+1)
	var next uint64
	for shard := 0; shard < shards; shard++ {
		if shard%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		starts[shard] = next
		for worker := 0; worker < workers; worker++ {
			offset := worker*shards + shard
			count := hist[offset]
			hist[offset] = next
			next += count
		}
	}
	starts[shards] = next
	if next != uint64(len(ids)) {
		return nil, errors.New("graph: parallel index histogram does not conserve rows")
	}
	entries := make([]indexEntry, len(ids))
	ranges(func(worker, start, end int) {
		cursor := hist[worker*shards : (worker+1)*shards]
		for ordinal := start; ordinal < end; ordinal++ {
			if ordinal%4096 == 0 {
				if ctx.Err() != nil {
					return
				}
			}
			id := idx.ids[ordinal]
			shard := parallelShard(id, shards)
			position := cursor[shard]
			entries[position] = indexEntry{id, uint32(ordinal)}
			cursor[shard]++
		}
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	jobs := make(chan int)
	var build sync.WaitGroup
	for range min(workers, shards) {
		build.Go(func() {
			for shard := range jobs {
				if ctx.Err() != nil {
					return
				}
				chunk := entries[starts[shard]:starts[shard+1]]
				if len(chunk) == 0 {
					continue
				}
				ordinals := make(map[uint64]uint32, len(chunk))
				for i, entry := range chunk {
					if i%4096 == 0 {
						if ctx.Err() != nil {
							return
						}
					}
					if _, exists := ordinals[entry.ID]; exists {
						fail(fmt.Errorf("graph: duplicate corpus work ID W%d", entry.ID))
						return
					}
					ordinals[entry.ID] = entry.Ordinal
				}
				idx.shards[shard] = ordinals
			}
		})
	}
dispatch:
	for shard := 0; shard < shards; shard++ {
		select {
		case jobs <- shard:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	build.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return idx, nil
}

// This reversible integer mixer distributes sequential OpenAlex numeric IDs as
// well as sparse IDs; bucket selection must be identical during lookup/build.
func mixIndexID(id uint64) uint64 {
	id ^= id >> 30
	id *= 0xbf58476d1ce4e5b9
	id ^= id >> 27
	id *= 0x94d049bb133111eb
	id ^= id >> 31
	return id
}
func parallelShard(id uint64, shards int) int {
	h := mixIndexID(id)
	if shards&(shards-1) == 0 {
		return int(h & uint64(shards-1))
	}
	return int(h % uint64(shards))
}
