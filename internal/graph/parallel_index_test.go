package graph

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"openalexstats/internal/model"
)

func TestParallelIndexLookupOwnershipAndShards(t *testing.T) {
	ids := []uint64{999, 1, 7000000001, 2, 19, 10003, ^uint64(0), 5, 8000000009}
	baseline, err := NewIndexContext(context.Background(), ids)
	if err != nil {
		t.Fatal(err)
	}
	for _, workers := range []int{1, 2, 4, 48} {
		for _, shards := range []int{1, 2, 3, 7, 64, 128} {
			got, err := NewParallelIndexContext(context.Background(), ids, workers, shards)
			if err != nil {
				t.Fatalf("workers=%d shards=%d: %v", workers, shards, err)
			}
			if got.Len() != baseline.Len() || !reflect.DeepEqual(got.IDs(), baseline.IDs()) {
				t.Fatal("dense ordinal order changed")
			}
			for _, id := range append(append([]uint64(nil), ids...), 0, 3, 888888) {
				a, ok := baseline.Lookup(id)
				b, yes := got.Lookup(id)
				if ok != yes || (ok && a != b) {
					t.Fatalf("W%d baseline=%d/%t parallel=%d/%t", id, a, ok, b, yes)
				}
			}
			estimate, err := EstimateParallelIndexBytes(len(ids), workers, shards)
			if err != nil || estimate.FinalBytes != got.EstimatedBytes() {
				t.Fatalf("estimate mismatch: %+v %v", estimate, err)
			}
		}
	}
	owned, err := NewParallelIndexContext(context.Background(), ids, 3, 7)
	if err != nil {
		t.Fatal(err)
	}
	ids[0] = 123456
	if ordinal, ok := owned.Lookup(999); !ok || ordinal != 0 || owned.IDs()[0] != 999 {
		t.Fatal("constructor borrowed caller's mutable vector")
	}
	empty, err := NewParallelIndexContext(context.Background(), nil, 8, 64)
	if err != nil || empty.Len() != 0 {
		t.Fatalf("empty index: %v", err)
	}
	if _, ok := empty.Lookup(1); ok {
		t.Fatal("empty index returned a hit")
	}
}

func TestParallelIndexMatchesCounts(t *testing.T) {
	ids := []uint64{30, 10, 20}
	serial, err := NewIndex(ids)
	if err != nil {
		t.Fatal(err)
	}
	parallel, err := NewParallelIndexContext(context.Background(), ids, 4, 7)
	if err != nil {
		t.Fatal(err)
	}
	works := []model.Work{{ID: 10, Year: 2000, References: []uint64{20, 20, 30, 999}}, {ID: 20, Year: 2001, References: []uint64{10}}}
	config := Config{Workers: 3, YearStart: 2000, YearEnd: 2001}
	a, err := Count(context.Background(), serial, input(works...), config)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Count(context.Background(), parallel, input(works...), config)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.cells, b.cells) || a.Stats() != b.Stats() {
		t.Fatal("transparent sharding changed citation counts")
	}
}

func TestParallelIndexProductionCandidateConfigurations(t *testing.T) {
	ids := make([]uint64, 100000)
	for ordinal := range ids {
		ids[ordinal] = mixIndexID(uint64(ordinal + 1))
	}
	for _, configuration := range [][2]int{{16, 64}, {32, 256}, {48, 512}} {
		idx, err := NewParallelIndexContext(context.Background(), ids, configuration[0], configuration[1])
		if err != nil {
			t.Fatal(err)
		}
		for ordinal, id := range ids {
			got, ok := idx.Lookup(id)
			if !ok || got != ordinal {
				t.Fatalf("workers=%d shards=%d W%d ordinal=%d/%t want=%d", configuration[0], configuration[1], id, got, ok, ordinal)
			}
		}
	}
}

func TestParallelIndexZeroDuplicatesAndCancellation(t *testing.T) {
	for _, ids := range [][]uint64{{0}, {1, 2, 3, 1}, {5, 2, 3, 4, 5}} {
		for _, shards := range []int{1, 3, 64} {
			if result, err := NewParallelIndexContext(context.Background(), ids, 4, shards); err == nil || result != nil {
				t.Fatalf("invalid IDs accepted: %v", ids)
			}
		}
	}
	if _, err := NewParallelIndexContext(nil, []uint64{1}, 1, 1); err == nil {
		t.Fatal("nil context accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := NewParallelIndexContext(ctx, []uint64{1}, 4, 7); !errors.Is(err, context.Canceled) || result != nil {
		t.Fatalf("pre-cancel: %v", err)
	}
	ids := make([]uint64, 500000)
	for i := range ids {
		ids[i] = uint64(i + 1)
	}
	ctx, cancel = context.WithCancel(context.Background())
	timer := time.AfterFunc(time.Millisecond, cancel)
	defer timer.Stop()
	defer cancel()
	if result, err := NewParallelIndexContext(ctx, ids, 8, 64); !errors.Is(err, context.Canceled) || result != nil {
		t.Fatalf("active cancellation returned partial index: %v", err)
	}
}

func TestParallelIndexMemoryPlanning(t *testing.T) {
	e, err := EstimateParallelIndexBytes(1000, 8, 64)
	if err != nil {
		t.Fatal(err)
	}
	if e.OwnedIDsBytes != 8000 || e.EntryBufferBytes != 1000*uint64(unsafe.Sizeof(indexEntry{})) || e.HistogramBytes != 8*64*8 || e.PrefixBytes != 65*8 || e.FinalBytes != e.OwnedIDsBytes+e.ShardMapsBytes || e.PeakBytes != e.FinalBytes+e.EntryBufferBytes+e.HistogramBytes+e.PrefixBytes {
		t.Fatalf("wrong construction budget: %+v", e)
	}
	for _, args := range [][3]int{{-1, 1, 1}, {1, 0, 1}, {1, 1, 0}, {1, 1025, 64}, {1, 1, 65537}} {
		if _, err := EstimateParallelIndexBytes(args[0], args[1], args[2]); err == nil {
			t.Fatalf("invalid planning settings accepted: %v", args)
		}
	}
}
