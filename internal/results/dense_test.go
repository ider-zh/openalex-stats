package results

import (
	"math"
	"math/rand"
	"reflect"
	"strconv"
	"testing"

	"openalexstats/internal/aggregate"
	"openalexstats/internal/model"
)

func denseTestDictionary() Dictionary {
	return Dictionary{Countries: []Label{{ID: "US", DisplayName: "United States"}, {ID: "CN", DisplayName: "China"}, {ID: "DE", DisplayName: "Germany"}}, Layers: []Layer{{"topics", "field", []Label{{ID: "F1", DisplayName: "One"}, {ID: "F2", DisplayName: "Two"}}}, {"topics", "subfield", []Label{{ID: "S1", DisplayName: "One"}, {ID: "S2", DisplayName: "Two"}}}, {"concepts", "0", []Label{{ID: "A", DisplayName: "A"}, {ID: "B", DisplayName: "B"}, {ID: "C", DisplayName: "C"}}}, {"concepts", "1", []Label{{ID: "A", DisplayName: "A"}, {ID: "B", DisplayName: "B"}, {ID: "C", DisplayName: "C"}}}}}
}
func baselineInputs(t *testing.T, works []model.Work, counts [][]uint64, start, end int) []Input {
	t.Helper()
	scenarios := DefaultScenarios()
	out := make([]Input, len(scenarios))
	for s, scenario := range scenarios {
		a, err := aggregate.New(scenario.Config())
		if err != nil {
			t.Fatal(err)
		}
		for i, w := range works {
			if err = a.AddSeries(w, start, counts[i], w.Year >= start && w.Year <= end); err != nil {
				t.Fatal(err)
			}
		}
		out[s] = Input{scenario, a.Rows(), a.Quality()}
	}
	return out
}
func compareInputs(t *testing.T, got, want []Input) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatal("input scenario count differs")
	}
	for i := range got {
		if got[i].Scenario != want[i].Scenario || !reflect.DeepEqual(got[i].Quality, want[i].Quality) {
			t.Fatalf("scenario/quality differs for %s\n got %+v\nwant %+v", got[i].Scenario.ID, got[i].Quality, want[i].Quality)
		}
		if len(got[i].Rows) != len(want[i].Rows) {
			t.Fatalf("row count differs for %s: %d != %d", got[i].Scenario.ID, len(got[i].Rows), len(want[i].Rows))
		}
		for j := range got[i].Rows {
			a, b := got[i].Rows[j], want[i].Rows[j]
			if a.Country != b.Country || a.Year != b.Year || a.Scheme != b.Scheme || a.Level != b.Level || a.DisciplineID != b.DisciplineID {
				t.Fatalf("row dimensions differ: %+v %+v", a, b)
			}
			if math.Abs(a.Publications-b.Publications) > 1e-9 || math.Abs(a.Citations-b.Citations) > 1e-9 {
				t.Fatalf("counts differ: %+v %+v", a, b)
			}
		}
	}
}

func TestDenseHandCountsAndOlderTarget(t *testing.T) {
	dictionary := denseTestDictionary()
	d, err := NewDense(dictionary, DefaultScenarios(), 2000, 2002)
	if err != nil {
		t.Fatal(err)
	}
	works := []model.Work{{Year: 1995, Countries: []string{" CN ", "US", "CN"}, FieldID: " F1 ", SubfieldID: "S1", Concepts: []model.Concept{{ID: "A", Level: 0, Score: .1}, {ID: "A", Level: 0, Score: .8}, {ID: "A", Level: 1, Score: .2}, {ID: "B", Level: 0, Score: .4}}}, {Year: 2001, Concepts: []model.Concept{{ID: "A", Level: 0, Score: math.NaN()}, {ID: "B", Level: 1, Score: math.Inf(1)}}}}
	counts := [][]uint64{{5, 0, 7}, {0, 2, 0}}
	for i, w := range works {
		if err = d.AddSeries(w, counts[i]); err != nil {
			t.Fatal(err)
		}
	}
	compareInputs(t, d.Inputs(), baselineInputs(t, works, counts, 2000, 2002))
	for _, input := range d.Inputs() {
		if input.Quality.PublicationWorks != 1 || input.Quality.CitationEvents != 14 {
			t.Fatal("old target publication leaked into window or citations disappeared")
		}
	}
}
func TestDenseRandomSixScenarioAgreementAndMerge(t *testing.T) {
	random := rand.New(rand.NewSource(70519))
	const start, end = 2000, 2026
	const n = 800
	works := make([]model.Work, n)
	counts := make([][]uint64, n)
	countries := []string{"CN", "US", "DE", "", " CN ", aggregate.Unknown}
	fields := []string{"F1", "F2", "", " F1 ", aggregate.Unknown}
	subfields := []string{"S1", "S2", "", aggregate.Unknown}
	conceptIDs := []string{"A", "B", "C", "", " A ", aggregate.Unknown}
	scores := []float64{-.1, 0, .2, .3, .4, .5, .9, 1, 1.5, math.NaN(), math.Inf(1)}
	for i := range works {
		w := model.Work{ID: uint64(i + 1), Year: 1990 + random.Intn(45), FieldID: fields[random.Intn(len(fields))], SubfieldID: subfields[random.Intn(len(subfields))]}
		for j := 0; j < random.Intn(8); j++ {
			w.Countries = append(w.Countries, countries[random.Intn(len(countries))])
		}
		for j := 0; j < random.Intn(14); j++ {
			level := random.Intn(4) - 1
			w.Concepts = append(w.Concepts, model.Concept{ID: conceptIDs[random.Intn(len(conceptIDs))], Level: level, Score: scores[random.Intn(len(scores))]})
		}
		works[i] = w
		counts[i] = make([]uint64, end-start+1)
		for y := range counts[i] {
			if random.Intn(3) == 0 {
				counts[i][y] = uint64(random.Intn(20))
			}
		}
	}
	scenarios := DefaultScenarios()
	dense, err := NewDense(denseTestDictionary(), scenarios, start, end)
	if err != nil {
		t.Fatal(err)
	}
	parts := make([]*Dense, 4)
	for i := range parts {
		parts[i], err = NewDense(denseTestDictionary(), scenarios, start, end)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, w := range works {
		if err = dense.AddSeries(w, counts[i]); err != nil {
			t.Fatal(err)
		}
		if err = parts[i%len(parts)].AddSeries(w, counts[i]); err != nil {
			t.Fatal(err)
		}
	}
	expected := baselineInputs(t, works, counts, start, end)
	compareInputs(t, dense.Inputs(), expected)
	merged, _ := NewDense(denseTestDictionary(), scenarios, start, end)
	for _, part := range parts {
		if err = merged.Merge(part); err != nil {
			t.Fatal(err)
		}
	}
	compareInputs(t, merged.Inputs(), expected)
}
func TestDenseUnknownUniverseAndValidation(t *testing.T) {
	d, err := NewDense(denseTestDictionary(), DefaultScenarios(), 2000, 2002)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []model.Work{{Year: 2000, Countries: []string{"ZZ"}}, {Year: 2000, FieldID: "unknown-real-field"}, {Year: 2000, Concepts: []model.Concept{{ID: "unknown-real-concept", Level: 0, Score: .1}}}} {
		if err = d.AddSeries(w, []uint64{1, 2, 3}); err == nil {
			t.Fatal("unknown real ID was silently dropped")
		}
	}
	for _, input := range d.Inputs() {
		if len(input.Rows) != 0 || input.Quality.PublicationWorks != 0 {
			t.Fatal("failed work partially mutated statistics")
		}
	}
	if err = d.AddSeries(model.Work{Year: 2000}, []uint64{1}); err == nil {
		t.Fatal("citation vector silently truncated")
	}
	if err = d.AddSeries(model.Work{Year: 2000}, []uint64{math.MaxUint64, 1, 0}); err == nil {
		t.Fatal("overflow accepted")
	}
	other, _ := NewDense(denseTestDictionary(), DefaultScenarios(), 2001, 2002)
	if d.Merge(other) == nil || d.Merge(d) == nil || d.Merge(nil) == nil {
		t.Fatal("incompatible merge accepted")
	}
	bad := DefaultScenarios()
	bad[1].ID = bad[0].ID
	if _, err = NewDense(denseTestDictionary(), bad, 2000, 2002); err == nil {
		t.Fatal("duplicate scenarios accepted")
	}
}
func TestDenseHotPathNoAllocations(t *testing.T) {
	d, err := NewDense(denseTestDictionary(), DefaultScenarios(), 2000, 2026)
	if err != nil {
		t.Fatal(err)
	}
	work := denseBenchmarkWork()
	counts := make([]uint64, 27)
	for y := range counts {
		counts[y] = uint64(y + 1)
	}
	allocations := testing.AllocsPerRun(200, func() {
		if err := d.AddSeries(work, counts); err != nil {
			t.Fatal(err)
		}
	})
	if allocations != 0 {
		t.Fatalf("dense hot path allocated %.1f objects/work", allocations)
	}
}
func TestDenseFullUniverseEstimate(t *testing.T) {
	dictionary := Dictionary{}
	for c := 0; c < 247; c++ {
		dictionary.Countries = append(dictionary.Countries, Label{ID: strconv.Itoa(c), DisplayName: "country"})
	}
	for i, key := range [][2]string{{"topics", "field"}, {"topics", "subfield"}, {"concepts", "0"}, {"concepts", "1"}} {
		layer := Layer{Scheme: key[0], Level: key[1]}
		for j := 0; j < []int{26, 252, 19, 284}[i]; j++ {
			layer.Labels = append(layer.Labels, Label{ID: strconv.Itoa(j), DisplayName: "label"})
		}
		dictionary.Layers = append(dictionary.Layers, layer)
	}
	estimate, err := EstimateDenseBytes(dictionary, DefaultScenarios(), 2000, 2026)
	if err != nil {
		t.Fatal(err)
	}
	expected := uint64(6 * 27 * 248 * 585 * 16)
	if estimate < expected || estimate > expected+1<<20 {
		t.Fatalf("unexpected estimate %d, cube=%d", estimate, expected)
	}
}
func denseBenchmarkWork() model.Work {
	return model.Work{Year: 2000, Countries: []string{"CN", "US", "CN", "DE"}, FieldID: "F1", SubfieldID: "S1", Concepts: []model.Concept{{ID: "A", Level: 0, Score: .2}, {ID: "B", Level: 0, Score: .7}, {ID: "C", Level: 0, Score: .6}, {ID: "C", Level: 0, Score: .9}, {ID: "A", Level: 1, Score: .8}, {ID: "B", Level: 1, Score: .3}, {ID: "C", Level: 1, Score: .5}}}
}
func BenchmarkSixScenarios(b *testing.B) {
	work := denseBenchmarkWork()
	counts := make([]uint64, 27)
	for i := range counts {
		counts[i] = uint64(i + 1)
	}
	scenarios := DefaultScenarios()
	b.Run("map_add_series", func(b *testing.B) {
		aggs := make([]*aggregate.Aggregator, len(scenarios))
		for i, s := range scenarios {
			aggs[i], _ = aggregate.New(s.Config())
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, a := range aggs {
				if err := a.AddSeries(work, 2000, counts, true); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
	b.Run("dense_add_series", func(b *testing.B) {
		d, err := NewDense(denseTestDictionary(), scenarios, 2000, 2026)
		if err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if err = d.AddSeries(work, counts); err != nil {
				b.Fatal(err)
			}
		}
	})
}
