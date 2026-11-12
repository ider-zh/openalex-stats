package main

import (
	"openalexstats/internal/model"
	"openalexstats/internal/results"
	"reflect"
	"testing"
)

func TestOverlayDoesNotAliasOrOverwriteKnown(t *testing.T) {
	valid := map[string]bool{"CN": true, "US": true}
	p := patch{ID: 1, Year: 2000, Countries: []string{"US", "CN"}}
	w := model.Work{ID: 1, Year: 2000}
	if e := apply(&w, p, valid); e != nil {
		t.Fatal(e)
	}
	w.Countries[0] = "changed"
	if !reflect.DeepEqual(p.Countries, []string{"US", "CN"}) {
		t.Fatal("shared patch aliased")
	}
	for _, p := range []patch{{ID: 1, Year: 2000, Countries: []string{"CN", "CN"}}, {ID: 1, Year: 2000, Countries: []string{"ZZ"}}, {ID: 2, Year: 2000, Countries: []string{"CN"}}, {ID: 1, Year: 1999, Countries: []string{"CN"}}, {ID: 1, Year: 2000, XPAC: true, Countries: []string{"CN"}}} {
		w := model.Work{ID: 1, Year: 2000}
		if apply(&w, p, valid) == nil {
			t.Fatalf("invalid patch accepted %+v", p)
		}
	}
	known := model.Work{ID: 1, Year: 2000, Countries: []string{"US"}}
	if apply(&known, p, valid) == nil || !reflect.DeepEqual(known.Countries, []string{"US"}) {
		t.Fatal("known country overwritten")
	}
}
func TestSixScenarioFullReaggregationPreservesRawTotalsAndEarlyCitations(t *testing.T) {
	dict := results.Dictionary{Countries: []results.Label{{ID: "CN"}, {ID: "US"}, {ID: "CA"}}, Layers: []results.Layer{
		{Scheme: "topics", Level: "field", Labels: []results.Label{{ID: "F"}}}, {Scheme: "topics", Level: "subfield", Labels: []results.Label{{ID: "S"}}},
		{Scheme: "concepts", Level: "0", Labels: []results.Label{{ID: "A"}, {ID: "B"}}}, {Scheme: "concepts", Level: "1", Labels: []results.Label{{ID: "C"}}}}}
	sc := results.DefaultScenarios()
	before, e := results.NewDense(dict, sc, 2000, 2001)
	if e != nil {
		t.Fatal(e)
	}
	after, e := results.NewDense(dict, sc, 2000, 2001)
	if e != nil {
		t.Fatal(e)
	}
	works := []model.Work{{ID: 1, Year: 1995, FieldID: "F", SubfieldID: "S", Concepts: []model.Concept{{ID: "A", Level: 0, Score: .8}, {ID: "B", Level: 0, Score: .4}, {ID: "C", Level: 1, Score: .7}}}, {ID: 2, Year: 2001, Countries: []string{"US"}, FieldID: "F", SubfieldID: "S"}, {ID: 3, Year: 2000}}
	counts := [][]uint64{{2, 3}, {1, 4}, {5, 0}}
	patches := map[uint64]patch{1: {ID: 1, Year: 1995, Countries: []string{"CN", "US"}}, 3: {ID: 3, Year: 2000, Countries: []string{"CA"}}}
	valid := map[string]bool{"CN": true, "US": true, "CA": true}
	for i, w := range works {
		if e = before.AddSeries(w, counts[i]); e != nil {
			t.Fatal(e)
		}
		if p, ok := patches[w.ID]; ok {
			if e = apply(&w, p, valid); e != nil {
				t.Fatal(e)
			}
		}
		if e = after.AddSeries(w, counts[i]); e != nil {
			t.Fatal(e)
		}
	}
	bs, as := before.Inputs(), after.Inputs()
	for i, a := range as {
		b := bs[i]
		if a.Quality.PublicationWorks != 2 || a.Quality.CitationEvents != 15 || a.Quality.MissingCountryPublications != 0 || a.Quality.MissingCountryCitations != 0 {
			t.Fatalf("raw/recovery totals %+v", a.Quality)
		}
		if b.Quality.MissingCountryPublications != 1 || b.Quality.MissingCountryCitations != 10 {
			t.Fatalf("baseline %+v", b.Quality)
		}
		if !reflect.DeepEqual(a.Quality.MissingDisciplineCitations, b.Quality.MissingDisciplineCitations) {
			t.Fatal("changed classification instead of country")
		}
		found := false
		for _, r := range a.Rows {
			if r.Country == "CN" && r.Year == 2000 && r.Scheme == "topics" && r.Level == "field" {
				want := 1.
				if a.Scenario.CountryMode == "whole" {
					want = 2
				}
				if r.Citations != want || r.Publications != 0 {
					t.Fatalf("early target country weighting %+v want %g", r, want)
				}
				found = true
			}
		}
		if !found {
			t.Fatal("lost pre2000 cited target")
		}
	}
	if _, _, e = results.Derive(as); e != nil {
		t.Fatal(e)
	}
}
