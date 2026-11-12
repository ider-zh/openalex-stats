package results

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"openalexstats/internal/aggregate"
	"openalexstats/internal/model"
)

func TestDenseWholeConceptThresholdAssignments(t *testing.T) {
	var scenarios []Scenario
	for _, country := range []string{"whole", "fractional"} {
		for _, threshold := range []float64{.2, .3, .4} {
			scenarios = append(scenarios, Scenario{ID: fmt.Sprintf("%s-%g", country, threshold), CountryMode: country, ConceptMode: "whole", ConceptThreshold: threshold})
		}
	}
	d, err := NewDense(denseTestDictionary(), scenarios, 2000, 2000)
	if err != nil {
		t.Fatal(err)
	}
	w := model.Work{Year: 2000, Countries: []string{"CN", "US", " CN "}, FieldID: "F1", SubfieldID: "S1", Concepts: []model.Concept{{ID: "A", Level: 0, Score: .1}, {ID: "A", Level: 0, Score: .9}, {ID: " B ", Level: 0, Score: .3}, {ID: "B", Level: 0, Score: .3}, {ID: "C", Level: 0, Score: .2}}}
	if err := d.AddSeries(w, []uint64{3}); err != nil {
		t.Fatal(err)
	}
	inputs := d.Inputs()
	for i, in := range inputs {
		s := scenarios[i]
		k := uint64(1)
		if s.ConceptThreshold == .2 {
			k = 3
		} else if s.ConceptThreshold == .3 {
			k = 2
		}
		q := in.Quality
		if q.PublicationAssignments["concepts.0"] != k || q.CitationAssignments["concepts.0"] != 3*k || q.PublicationAssignments["concepts.1"] != 1 || q.CitationAssignments["concepts.1"] != 3 || q.MissingDisciplinePublications["concepts.1"] != 1 {
			t.Fatalf("threshold assignments: %+v", in)
		}
		weight := 1.0
		if s.CountryMode == "fractional" {
			weight = .5
		}
		var p, c float64
		hits := 0
		for _, r := range in.Rows {
			if r.Scheme == "concepts" && r.Level == "0" {
				hits++
				p += r.Publications
				c += r.Citations
				if r.Publications != weight || r.Citations != 3*weight {
					t.Fatal(r)
				}
			}
		}
		if hits != 2*int(k) || p != float64(2*k)*weight || c != float64(6*k)*weight {
			t.Fatalf("conservation %s: hits=%d p=%v c=%v", s.ID, hits, p, c)
		}
		a, err := aggregate.New(s.Config())
		if err != nil {
			t.Fatal(err)
		}
		if err := a.AddSeries(w, 2000, []uint64{3}, true); err != nil {
			t.Fatal(err)
		}
		compareInputs(t, []Input{in}, []Input{{Scenario: s, Rows: a.Rows(), Quality: a.Quality()}})
	}
	inputs[0].Quality.PublicationAssignments["concepts.0"] = 99
	if d.Inputs()[0].Quality.PublicationAssignments["concepts.0"] != 3 {
		t.Fatal("Inputs assignment maps alias Dense")
	}
}

func TestDenseAssignmentOverflowIsAtomicAcrossScenarios(t *testing.T) {
	s := []Scenario{{ID: "whole", CountryMode: "whole", ConceptMode: "whole", ConceptThreshold: .3}, {ID: "fractional", CountryMode: "fractional", ConceptMode: "fractional", ConceptThreshold: .3}}
	d, err := NewDense(denseTestDictionary(), s, 2000, 2000)
	if err != nil {
		t.Fatal(err)
	}
	w := model.Work{Year: 2000, Countries: []string{"CN", "US"}, Concepts: []model.Concept{{ID: "A", Level: 0, Score: .9}, {ID: "B", Level: 0, Score: .8}}}
	if err := d.AddSeries(w, []uint64{math.MaxUint64/2 + 1}); err == nil {
		t.Fatal("assignment multiplication overflow accepted")
	}
	for _, in := range d.Inputs() {
		if len(in.Rows) != 0 || in.Quality.PublicationWorks != 0 {
			t.Fatal("overflow partially applied")
		}
	}
	// Force the second scenario to reject an otherwise ordinary addition. The
	// first scenario must remain untouched as well.
	d.quality[1].citationAssignments[2] = math.MaxUint64
	before := d.Inputs()
	if err := d.AddSeries(w, []uint64{1}); err == nil {
		t.Fatal("quality addition overflow accepted")
	}
	if !reflect.DeepEqual(d.Inputs(), before) {
		t.Fatal("scenario overflow partially applied")
	}
	other, _ := NewDense(denseTestDictionary(), s, 2000, 2000)
	if err := other.AddSeries(w, []uint64{1}); err != nil {
		t.Fatal(err)
	}
	if err := d.Merge(other); err == nil {
		t.Fatal("quality merge overflow accepted")
	}
	if !reflect.DeepEqual(d.Inputs(), before) {
		t.Fatal("failed merge partially applied")
	}
}

func TestDenseExplicitFractionalConceptsKeepRawAssignments(t *testing.T) {
	for _, country := range []string{"whole", "fractional"} {
		t.Run(country, func(t *testing.T) {
			s := Scenario{ID: "legacy-" + country, CountryMode: country, ConceptMode: "fractional", ConceptThreshold: .3}
			d, err := NewDense(denseTestDictionary(), []Scenario{s}, 2000, 2000)
			if err != nil {
				t.Fatal(err)
			}
			w := model.Work{Year: 2000, Countries: []string{"CN", "US"}, Concepts: []model.Concept{{ID: "A", Level: 0, Score: .9}, {ID: "B", Level: 0, Score: .8}, {ID: "B", Level: 0, Score: .7}}}
			if err := d.AddSeries(w, []uint64{3}); err != nil {
				t.Fatal(err)
			}
			in := d.Inputs()[0]
			if in.Quality.PublicationAssignments["concepts.0"] != 2 || in.Quality.CitationAssignments["concepts.0"] != 6 {
				t.Fatal(in.Quality)
			}
			weight := .5
			if country == "fractional" {
				weight = .25
			}
			for _, r := range in.Rows {
				if r.Scheme == "concepts" && r.Level == "0" && (r.Publications != weight || r.Citations != 3*weight) {
					t.Fatal(r)
				}
			}
			a, err := aggregate.New(s.Config())
			if err != nil {
				t.Fatal(err)
			}
			if err := a.AddSeries(w, 2000, []uint64{3}, true); err != nil {
				t.Fatal(err)
			}
			compareInputs(t, []Input{in}, []Input{{Scenario: s, Rows: a.Rows(), Quality: a.Quality()}})
		})
	}
}
