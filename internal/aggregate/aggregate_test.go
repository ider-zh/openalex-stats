package aggregate

import (
	"math"
	"openalexstats/internal/model"
	"reflect"
	"testing"
)

func TestAddSeriesMatchesIndividualCalls(t *testing.T) {
	w := model.Work{Year: 2000, Countries: []string{"US", "CN", "CN"}, FieldID: "F", SubfieldID: "S", Concepts: []model.Concept{{ID: "A", Level: 0, Score: .9}, {ID: "B", Level: 0, Score: .8}, {ID: "B", Level: 0, Score: .7}}}
	for _, mode := range []string{"whole", "fractional"} {
		a, _ := New(Config{CountryMode: mode})
		b, _ := New(Config{CountryMode: mode})
		counts := []uint64{1, 0, 8, 99}
		if err := a.AddSeries(w, 2001, counts, true); err != nil {
			t.Fatal(err)
		}
		_ = b.AddPublication(w)
		for i, c := range counts {
			if c > 0 {
				_ = b.AddCitation(w, 2001+i, c)
			}
		}
		if !reflect.DeepEqual(a.Rows(), b.Rows()) || !reflect.DeepEqual(a.Quality(), b.Quality()) {
			t.Fatal("series changed counting semantics")
		}
	}
}

func BenchmarkAddSeries(b *testing.B) {
	w := model.Work{Year: 2000, Countries: []string{"CN", "US", "DE"}, FieldID: "F", SubfieldID: "S"}
	for level := 0; level < 2; level++ {
		for _, id := range []string{"A", "B", "C", "D"} {
			w.Concepts = append(w.Concepts, model.Concept{ID: id, Level: level, Score: 1})
		}
	}
	counts := make([]uint64, 27)
	for i := range counts {
		counts[i] = uint64(i + 1)
	}
	for _, series := range []bool{false, true} {
		name := "individual"
		if series {
			name = "series"
		}
		b.Run(name, func(b *testing.B) {
			a, _ := New(Config{})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if series {
					_ = a.AddSeries(w, 2000, counts, true)
				} else {
					_ = a.AddPublication(w)
					for y, c := range counts {
						_ = a.AddCitation(w, 2000+y, c)
					}
				}
			}
		})
	}
}

func TestFractionalConservationAndTargetYear(t *testing.T) {
	a, err := New(Config{ConceptMode: "fractional", ConceptThreshold: .3})
	if err != nil {
		t.Fatal(err)
	}
	w := model.Work{ID: 1, Year: 1995, Countries: []string{"CN", "US", "CN"}, FieldID: "F", SubfieldID: "S", Concepts: []model.Concept{{ID: "C1", Level: 0, Score: .9}, {ID: "C1", Level: 0, Score: .8}, {ID: "C2", Level: 0, Score: .7}, {ID: "C3", Level: 1, Score: .2}}}
	if err = a.AddPublication(w); err != nil {
		t.Fatal(err)
	}
	if err = a.AddCitation(w, 2005, 7); err != nil {
		t.Fatal(err)
	}
	pub, cit := map[string]float64{}, map[string]float64{}
	for _, r := range a.Rows() {
		key := r.Scheme + r.Level
		pub[key] += r.Publications
		cit[key] += r.Citations
		if r.Citations > 0 && r.Year != 2005 {
			t.Fatal("citations must use citing year")
		}
		if r.Publications > 0 && r.Year != 1995 {
			t.Fatal("publications must use publication year")
		}
	}
	for _, key := range []string{"topicsfield", "topicssubfield", "concepts0", "concepts1"} {
		if pub[key] != 1 || cit[key] != 7 {
			t.Fatalf("%s totals %v %v", key, pub[key], cit[key])
		}
	}
	q := a.Quality()
	if q.MissingDisciplinePublications["concepts.1"] != 1 || q.MissingDisciplineCitations["concepts.1"] != 7 {
		t.Fatalf("missing labels lost: %+v", q)
	}
}

func TestWholeCountingAndMissingCountry(t *testing.T) {
	a, _ := New(Config{CountryMode: "whole", ConceptMode: "whole"})
	w := model.Work{Year: 2000, Countries: []string{"CN", "US"}, Concepts: []model.Concept{{ID: "A", Level: 0, Score: 1}, {ID: "B", Level: 0, Score: 1}}}
	_ = a.AddPublication(w)
	total := 0.0
	for _, r := range a.Rows() {
		if r.Scheme == "concepts" && r.Level == "0" {
			total += r.Publications
		}
	}
	if total != 4 {
		t.Fatalf("whole country x concept counting=%v", total)
	}
	_ = a.AddCitation(model.Work{}, 2001, 3)
	q := a.Quality()
	if q.MissingCountryCitations != 3 {
		t.Fatal(q)
	}
	q.MissingDisciplineCitations["topics.field"] = 999
	if a.Quality().MissingDisciplineCitations["topics.field"] == 999 {
		t.Fatal("quality snapshot aliases state")
	}
}

func TestConfigAndYearValidation(t *testing.T) {
	for _, cfg := range []Config{{CountryMode: "bad"}, {ConceptThreshold: math.NaN()}, {ConceptThreshold: math.Inf(1)}, {ConceptThreshold: 2}} {
		if _, err := New(cfg); err == nil {
			t.Fatalf("accepted %+v", cfg)
		}
	}
	a, _ := New(Config{})
	if err := a.AddPublication(model.Work{Year: 0}); err == nil {
		t.Fatal("invalid publication year accepted")
	}
}

func TestWorkerMerge(t *testing.T) {
	a, _ := New(Config{})
	b, _ := New(Config{})
	w := model.Work{Year: 2000, Countries: []string{"CN"}, FieldID: "F", SubfieldID: "S"}
	if err := a.AddPublication(w); err != nil {
		t.Fatal(err)
	}
	if err := b.AddCitation(w, 2001, 9); err != nil {
		t.Fatal(err)
	}
	if err := a.Merge(b); err != nil {
		t.Fatal(err)
	}
	pub, cit := 0.0, 0.0
	for _, r := range a.Rows() {
		if r.Scheme == "topics" && r.Level == "field" {
			pub += r.Publications
			cit += r.Citations
		}
	}
	if pub != 1 || cit != 9 || a.Quality().PublicationWorks != 1 || a.Quality().CitationCalls != 1 {
		t.Fatal("merge lost contributions")
	}
	other, _ := New(Config{CountryMode: "whole"})
	if a.Merge(other) == nil || a.Merge(a) == nil || a.Merge(nil) == nil {
		t.Fatal("invalid merge accepted")
	}
}
