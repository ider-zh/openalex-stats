package graph

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"openalexstats/internal/model"
)

func input(works ...model.Work) <-chan model.Work {
	ch := make(chan model.Work, len(works))
	for _, w := range works {
		ch <- w
	}
	close(ch)
	return ch
}

func mustIndex(t *testing.T, ids ...uint64) *Index {
	t.Helper()
	i, err := NewIndex(ids)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func TestInducedGraphByCitingYear(t *testing.T) {
	// Q includes an old work and a work with missing year: target eligibility is
	// independent of whether the target can itself emit a dated citation.
	idx := mustIndex(t, 3, 1, 10, 2, 4)
	refs := []uint64{2, 2, 99, 0, 3, 1}
	result, err := Count(context.Background(), idx, input(
		model.Work{ID: 1, Year: 2000, References: refs},
		model.Work{ID: 2, Year: 2001, References: []uint64{3, 1}},
		model.Work{ID: 3, Year: 1999, References: []uint64{1}},
		model.Work{ID: 4, References: []uint64{2}},
		model.Work{ID: 10, Year: 2003, References: []uint64{2}},
		model.Work{ID: 5, Year: 2000, References: []uint64{1}},
		model.Work{ID: 0, Year: 2001, References: []uint64{2}},
	), Config{Workers: 4, YearStart: 2000, YearEnd: 2002})
	if err != nil {
		t.Fatal(err)
	}
	type edgeCount struct {
		id uint64
		y  int
		n  uint64
	}
	want := []edgeCount{{3, 2000, 1}, {3, 2001, 1}, {1, 2000, 1}, {1, 2001, 1}, {2, 2000, 1}}
	var got []edgeCount
	err = result.ForEachNonZero(func(id uint64, year int, n uint64) error {
		got = append(got, edgeCount{id, year, n})
		if result.Count(id, year) != n {
			t.Fatalf("Count disagrees for W%d/%d", id, year)
		}
		return nil
	})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v, err %v", got, want, err)
	}
	if !slices.Equal(refs, []uint64{2, 2, 99, 0, 3, 1}) {
		t.Fatal("graph mutated the producer's references")
	}
	s := result.Stats()
	if s.SourceRecords != 7 || s.MemberSourceRecords != 5 || s.ValidYearSourceRecords != 2 ||
		s.InvalidSourceIDs != 1 || s.OutsideSourceCorpus != 1 || s.InvalidSourceYears != 1 ||
		s.OutsideSourceYearWindow != 2 || s.ReferenceSlots != 8 || s.InvalidReferenceIDs != 1 ||
		s.DuplicateReferences != 1 || s.OutsideTargetCorpus != 1 || s.RetainedEdges != 5 || s.SelfLoops != 1 {
		t.Fatalf("unexpected audit stats: %+v", s)
	}
	if s.ReferenceSlots != s.InvalidReferenceIDs+s.DuplicateReferences+s.OutsideTargetCorpus+s.RetainedEdges {
		t.Fatal("reference accounting does not reconcile")
	}
	if result.Count(99, 2000) != 0 || result.Count(1, 1999) != 0 || result.Count(1, 2003) != 0 {
		t.Fatal("out-of-domain lookup must be zero")
	}
	if start, end := result.YearRange(); start != 2000 || end != 2002 {
		t.Fatalf("unexpected range %d:%d", start, end)
	}
	ordinal, _ := idx.Lookup(1)
	if !slices.Equal(result.YearCounts(ordinal), []uint64{1, 1, 0}) || result.YearCounts(-1) != nil || result.YearCounts(idx.Len()) != nil {
		t.Fatal("incorrect ordinal year vector")
	}
}

func TestWorkerCountDoesNotChangeCounts(t *testing.T) {
	const n = 1000
	ids := make([]uint64, n)
	works := make([]model.Work, n)
	for i := range ids {
		ids[i] = uint64(i + 1)
		works[i] = model.Work{ID: ids[i], Year: 2000 + i%3, References: []uint64{2, 1, 1, 1001}}
	}
	idx := mustIndex(t, ids...)
	var baseline Stats
	for _, workers := range []int{1, 2, 8, 32} {
		result, err := Count(context.Background(), idx, input(works...), Config{Workers: workers, YearStart: 2000, YearEnd: 2002})
		if err != nil {
			t.Fatal(err)
		}
		for y, want := range []uint64{334, 333, 333} {
			for _, id := range []uint64{1, 2} {
				if got := result.Count(id, 2000+y); got != want {
					t.Fatalf("workers=%d W%d year=%d got %d want %d", workers, id, 2000+y, got, want)
				}
			}
		}
		if result.Stats().RetainedEdges != 2000 || result.Stats().DuplicateReferences != 1000 || result.Stats().OutsideTargetCorpus != 1000 {
			t.Fatalf("wrong star-graph totals %+v", result.Stats())
		}
		if workers == 1 {
			baseline = result.Stats()
		} else if result.Stats() != baseline {
			t.Fatalf("worker scheduling changed stats: %+v vs %+v", result.Stats(), baseline)
		}
	}
}

func TestDuplicateSourceIsFatalEvenWhenUndated(t *testing.T) {
	idx := mustIndex(t, 1, 2)
	for _, year := range []int{0, 1990, 2000} {
		result, err := Count(context.Background(), idx, input(
			model.Work{ID: 1, Year: year, References: []uint64{2}},
			model.Work{ID: 1, Year: year, References: []uint64{2}},
		), Config{Workers: 2, YearStart: 2000, YearEnd: 2000})
		var duplicate *DuplicateSourceError
		if result != nil || !errors.As(err, &duplicate) || duplicate.ID != 1 {
			t.Fatalf("duplicate source silently accepted: result=%v err=%v", result, err)
		}
	}
}

func TestIndexValidationAndOwnership(t *testing.T) {
	for _, ids := range [][]uint64{{0}, {1, 1}} {
		if _, err := NewIndex(ids); err == nil {
			t.Fatalf("accepted invalid IDs %v", ids)
		}
	}
	ids := []uint64{7, 9}
	i := mustIndex(t, ids...)
	ids[0] = 10
	if _, found := i.Lookup(7); !found || i.IDs()[0] != 7 {
		t.Fatal("index borrowed mutable caller storage")
	}
	if n, err := EstimateIndexBytes(2); err != nil || n != i.EstimatedBytes() {
		t.Fatalf("index estimate mismatch %d %v", n, err)
	}
	if _, err := EstimateIndexBytes(-1); err == nil {
		t.Fatal("accepted negative corpus size")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewIndexContext(ctx, []uint64{1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("index ignores canceled context: %v", err)
	}
}

func TestMemoryLimitAndDimensionOverflow(t *testing.T) {
	idx := mustIndex(t, 1, 2)
	// Two IDs * three years * eight bytes + one seen bitset word = 56.
	if _, err := Count(context.Background(), idx, input(), Config{YearStart: 2000, YearEnd: 2002, MaxCounterBytes: 55}); err == nil {
		t.Fatal("memory limit ignored")
	}
	result, err := Count(context.Background(), idx, input(), Config{Workers: 1, YearStart: 2000, YearEnd: 2002, MaxCounterBytes: 56})
	if err != nil || result.Stats().CounterBytes != 48 || result.Stats().SeenSourceBytes != 8 {
		t.Fatalf("exact memory limit rejected: %v", err)
	}
	if _, _, err := requiredBytes(2, ^uint64(0)); err == nil {
		t.Fatal("dimension overflow accepted")
	}
	if _, err := Count(context.Background(), idx, input(), Config{YearStart: 2002, YearEnd: 2000}); err == nil {
		t.Fatal("backwards window accepted")
	}
}

func TestCancellationAndCallbackFailure(t *testing.T) {
	idx := mustIndex(t, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if result, err := Count(ctx, idx, make(chan model.Work), Config{Workers: 2, YearStart: 2000, YearEnd: 2000}); result != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked input ignored cancellation: result=%v err=%v", result, err)
	}
	result, err := Count(context.Background(), idx, input(model.Work{ID: 1, Year: 2000, References: []uint64{1}}), Config{Workers: 1, YearStart: 2000, YearEnd: 2000})
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("stop")
	if err := result.ForEachNonZero(func(uint64, int, uint64) error { return stop }); !errors.Is(err, stop) {
		t.Fatalf("callback error lost: %v", err)
	}
	if err := result.ForEachNonZero(nil); err == nil {
		t.Fatal("nil callback accepted")
	}
}
