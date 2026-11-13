package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"openalexstats/internal/eci"
	"openalexstats/internal/pipeline"
)

func directionModel(year int, scores []float64) pipeline.ModelResult {
	return pipeline.ModelResult{Year: year, Scheme: "topics", Level: "field", Metric: "publications", Result: eci.Result{Status: "ok", Countries: []string{"A", "B", "C"}, Disciplines: []string{"x", "y"}, ActiveCountries: []int{0, 1, 2}, ActiveDisciplines: []int{0, 1}, ECI: scores, PCI: []float64{2, -2}}}
}

func cloneDirection(models []pipeline.ModelResult) []pipeline.ModelResult {
	b, _ := json.Marshal(models)
	var out []pipeline.ModelResult
	_ = json.Unmarshal(b, &out)
	return out
}

func TestPooledDirectionUsesOneWholeChainFactorAndBothSides(t *testing.T) {
	base := []pipeline.ModelResult{directionModel(2000, []float64{-1, 0, 1}), directionModel(2001, []float64{-1, 0, 1}), directionModel(2026, []float64{-1, 0, 1})}
	current := []pipeline.ModelResult{directionModel(2000, []float64{3, 0, -3}), directionModel(2001, []float64{-1, 0, 1}), directionModel(2026, []float64{-1e9, 0, 1e9})}
	before := cloneDirection(current)
	baselineBefore := cloneDirection(base)
	records := alignToBaseline(current, base, "spectral-v1", "baseline-sha")
	if len(records) != 1 || records[0].Factor != -1 || records[0].CommonPairN != 6 || !reflect.DeepEqual(records[0].CommonYears, []int{2000, 2001}) || records[0].Unavailable || records[0].BaselineModelsSHA256 != "baseline-sha" {
		t.Fatalf("wrong pooled orientation: %+v", records)
	}
	for year := range current {
		for i, v := range before[year].Result.ECI {
			if current[year].Result.ECI[i] != -v {
				t.Fatal("per-year/country factor used")
			}
		}
		for i, v := range before[year].Result.PCI {
			if current[year].Result.PCI[i] != -v {
				t.Fatal("PCI did not jointly follow ECI")
			}
		}
		if !strings.Contains(current[year].Result.SignConvention, directionPolicy) {
			t.Fatal("direction policy not disclosed")
		}
	}
	if !reflect.DeepEqual(base, baselineBefore) {
		t.Fatal("reference baseline mutated")
	}
	if *records[0].RawPooledRho >= 0 || *records[0].AlignedPooledRho <= 0 {
		t.Fatal("raw/aligned sign audit missing")
	}
}

func TestPooledDirectionDoesNotChooseOneCountryOrInputOrdering(t *testing.T) {
	base := []pipeline.ModelResult{directionModel(2000, []float64{-1, 0, 1})}
	current := []pipeline.ModelResult{directionModel(2000, []float64{1, 0, -1})}
	first := alignToBaseline(cloneDirection(current), cloneDirection(base), "spectral-v1", "source")
	permutedCurrent, permutedBase := cloneDirection(current), cloneDirection(base)
	for _, models := range [][]pipeline.ModelResult{permutedCurrent, permutedBase} {
		models[0].Result.Countries = []string{"C", "A", "B"}
		v := models[0].Result.ECI
		models[0].Result.ECI = []float64{v[2], v[0], v[1]}
	}
	second := alignToBaseline(permutedCurrent, permutedBase, "spectral-v1", "source")
	if !reflect.DeepEqual(first, second) {
		t.Fatal("label iteration order altered pooled factor")
	}
	for _, models := range [][]pipeline.ModelResult{base, current} {
		models[0].Result.Countries = []string{"SG", "US", "CN"}
	}
	third := alignToBaseline(current, base, "spectral-v1", "source")
	if first[0].Factor != third[0].Factor || *first[0].RawPooledRho != *third[0].RawPooledRho {
		t.Fatal("country identity affected direction")
	}
}

func TestPooledDirectionDisclosesWeakAndUnavailableReferences(t *testing.T) {
	base := []pipeline.ModelResult{directionModel(2000, []float64{-1, 0, 1})}
	current := []pipeline.ModelResult{directionModel(2000, []float64{1, -2, 1})}
	r := alignToBaseline(current, base, "spectral-v1", "source")[0]
	if r.Unavailable || !r.WeakReference || r.Factor != 1 || r.RawPooledRho == nil || *r.RawPooledRho != 0 {
		t.Fatal("weak zero-cov reference mishandled")
	}
	current = []pipeline.ModelResult{directionModel(2026, []float64{1, 0, -1})}
	before := cloneDirection(current)
	r = alignToBaseline(current, base, "spectral-v1", "source")[0]
	if !r.Unavailable || r.Factor != 1 || r.RawPooledRho != nil || r.AlignedPooledRho != nil || r.CommonPairN != 0 || !reflect.DeepEqual(current[0].Result.ECI, before[0].Result.ECI) {
		t.Fatal("unavailable reference invented orientation")
	}
}
