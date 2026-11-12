package aggregate

import (
	"math"
	"reflect"
	"testing"

	"openalexstats/internal/model"
)

func twoConceptWork() model.Work {
	return model.Work{Year: 2000, Countries: []string{"CN", "US", "CN"}, FieldID: "F", SubfieldID: "S", Concepts: []model.Concept{{ID: "A", Level: 0, Score: .9}, {ID: " A ", Level: 0, Score: .8}, {ID: "B", Level: 0, Score: .7}, {ID: "B", Level: 0, Score: .1}}}
}

func TestConceptHitsAndRawAssignments(t *testing.T) {
	for _, countryMode := range []string{"whole", "fractional"} {
		for _, conceptMode := range []string{"whole", "fractional"} {
			t.Run(countryMode+"/"+conceptMode, func(t *testing.T) {
				a, err := New(Config{CountryMode: countryMode, ConceptMode: conceptMode, ConceptThreshold: .3})
				if err != nil {
					t.Fatal(err)
				}
				if err := a.AddSeries(twoConceptWork(), 2000, []uint64{3}, true); err != nil {
					t.Fatal(err)
				}
				weight := 1.0
				if countryMode == "fractional" {
					weight /= 2
				}
				if conceptMode == "fractional" {
					weight /= 2
				}
				hits := 0
				var publications, citations float64
				for _, r := range a.Rows() {
					if r.Scheme == "concepts" && r.Level == "0" {
						hits++
						if r.Publications != weight || r.Citations != 3*weight {
							t.Fatalf("wrong hit weight: %+v", r)
						}
						publications += r.Publications
						citations += r.Citations
					}
				}
				if hits != 4 || publications != 4*weight || citations != 12*weight {
					t.Fatalf("lost or duplicated hits: %d / %v / %v", hits, publications, citations)
				}
				q := a.Quality()
				wantP := map[string]uint64{"topics.field": 1, "topics.subfield": 1, "concepts.0": 2, "concepts.1": 1}
				wantC := map[string]uint64{"topics.field": 3, "topics.subfield": 3, "concepts.0": 6, "concepts.1": 3}
				if !reflect.DeepEqual(q.PublicationAssignments, wantP) || !reflect.DeepEqual(q.CitationAssignments, wantC) || q.PublicationWorks != 1 || q.CitationEvents != 3 || q.CitationCalls != 1 {
					t.Fatalf("quality: %+v", q)
				}
				if q.MissingDisciplinePublications["concepts.1"] != 1 || q.MissingDisciplineCitations["concepts.1"] != 3 {
					t.Fatal("UNKNOWN must count once")
				}
				q.PublicationAssignments["concepts.0"] = 99
				q.CitationAssignments["concepts.0"] = 99
				if a.Quality().PublicationAssignments["concepts.0"] != 2 || a.Quality().CitationAssignments["concepts.0"] != 6 {
					t.Fatal("quality assignment maps alias accumulator")
				}
			})
		}
	}
	// Country fractional remains the default; an omitted ConceptMode now counts
	// each eligible concept once instead of dividing by the number of concepts.
	a, err := New(Config{ConceptThreshold: .3})
	if err != nil {
		t.Fatal(err)
	}
	if a.cfg.ConceptMode != "whole" || a.cfg.CountryMode != "fractional" {
		t.Fatal(a.cfg)
	}
	if err := a.AddSeries(twoConceptWork(), 2000, []uint64{3}, true); err != nil {
		t.Fatal(err)
	}
	for _, r := range a.Rows() {
		if r.Scheme == "concepts" && r.Level == "0" && (r.Publications != .5 || r.Citations != 1.5) {
			t.Fatal(r)
		}
	}
}

func TestAssignmentOverflowRejectsWithoutMutation(t *testing.T) {
	w := twoConceptWork()
	a, _ := New(Config{})
	if err := a.AddSeries(w, 2000, []uint64{math.MaxUint64/2 + 1}, true); err == nil {
		t.Fatal("assignment multiplication overflow accepted")
	}
	if len(a.Rows()) != 0 || a.Quality().PublicationWorks != 0 || len(a.Quality().CitationAssignments) != 0 {
		t.Fatal("overflow partially applied")
	}
	if err := a.AddSeries(w, 2000, []uint64{math.MaxUint64, 1}, true); err == nil {
		t.Fatal("series events overflow accepted")
	}
	if err := a.AddCitation(w, 2000, math.MaxUint64/2); err != nil {
		t.Fatal(err)
	}
	beforeQ, beforeRows := a.Quality(), a.Rows()
	if err := a.AddSeries(w, 2000, []uint64{1}, true); err == nil {
		t.Fatal("cumulative assignment overflow accepted")
	}
	if !reflect.DeepEqual(a.Quality(), beforeQ) || !reflect.DeepEqual(a.Rows(), beforeRows) {
		t.Fatal("failed addition mutated accumulator")
	}
	b, _ := New(Config{})
	if err := b.AddSeries(w, 2000, []uint64{1}, true); err != nil {
		t.Fatal(err)
	}
	if err := a.Merge(b); err == nil {
		t.Fatal("assignment merge overflow accepted")
	}
	if !reflect.DeepEqual(a.Quality(), beforeQ) || !reflect.DeepEqual(a.Rows(), beforeRows) {
		t.Fatal("failed merge mutated accumulator")
	}
}

func TestAssignmentMergeConservesRawHits(t *testing.T) {
	a, _ := New(Config{})
	b, _ := New(Config{})
	if err := a.AddPublication(twoConceptWork()); err != nil {
		t.Fatal(err)
	}
	if err := b.AddCitation(twoConceptWork(), 2001, 3); err != nil {
		t.Fatal(err)
	}
	if err := a.Merge(b); err != nil {
		t.Fatal(err)
	}
	q := a.Quality()
	if q.PublicationAssignments["concepts.0"] != 2 || q.CitationAssignments["concepts.0"] != 6 || q.PublicationWorks != 1 || q.CitationEvents != 3 {
		t.Fatal(q)
	}
	missing, _ := New(Config{})
	if err := missing.AddSeries(model.Work{Year: 2000}, 2000, []uint64{3}, true); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"topics.field", "topics.subfield", "concepts.0", "concepts.1"} {
		q := missing.Quality()
		if q.PublicationAssignments[key] != 1 || q.CitationAssignments[key] != 3 {
			t.Fatalf("missing assignment %s: %+v", key, q)
		}
	}
}
