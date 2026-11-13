package main

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/complexityfilter"
	"openalexstats/internal/eci"
	"openalexstats/internal/pipeline"
	"openalexstats/internal/results"
)

func TestPresetGridAndCopiedBaseline(t *testing.T) {
	presets := complexityfilter.Presets()
	if len(presets) != 19 || validateProfiles(presets, complexityfilter.DefaultProfileID) != nil {
		t.Fatal("frozen18+baseline grid differs")
	}
	m := eci.Matrix{Countries: []string{"A", "B", "C"}, Disciplines: []string{"x", "y", "z"}, Values: [][]float64{{10, 1, 1}, {5, 5, 1}, {1, 5, 10}}}
	in := complexityfilter.Input{Matrix: m, Metric: "publications", WholePublications: []uint64{1000, 1000, 1000}, WholeCitations: []uint64{9000, 9000, 9000}}
	zero, err := complexityfilter.Compute(in, complexityfilter.Profile{ID: complexityfilter.BaselineProfileID})
	if err != nil {
		t.Fatal(err)
	}
	s, j := zero.Spectral, zero.Reflections
	for _, r := range []*eci.Result{&s, &j} {
		for i := range r.ECI {
			r.ECI[i] *= -1
		}
		for i := range r.PCI {
			r.PCI[i] *= -1
		}
		r.RCA, r.Advantage, r.Components = nil, nil, nil
	}
	source := sourceData{Inputs: []annualInput{{Year: 2000, Scheme: "topics", Level: "field", Metric: "publications", Input: in}}, NetworkHashes: []string{zero.Diagnostics.FinalNetworkSHA256}, Baseline: map[string][]pipeline.ModelResult{
		"spectral-v1":         {{Year: 2000, Scheme: "topics", Level: "field", Metric: "publications", Result: s}},
		"java-reflections-v1": {{Year: 2000, Scheme: "topics", Level: "field", Metric: "publications", Result: j}},
	}}
	out, err := computeProfiles(context.Background(), source, []complexityfilter.Profile{{ID: complexityfilter.BaselineProfileID}}, 2)
	if err != nil {
		t.Fatal(err)
	}
	for method, models := range source.Baseline {
		if !reflect.DeepEqual(out[0].models[method], models) {
			t.Fatal("baseline was reoriented or recomputed")
		}
	}
}

func TestGlobalCompetitionRanksAndInvalidNullRank(t *testing.T) {
	ranks, err := competition([]float64{1, 2, 2, -1})
	if err != nil || !reflect.DeepEqual(ranks, []int{3, 1, 1, 4}) {
		t.Fatal("competition ties changed")
	}
	m := pipeline.ModelResult{Year: 2000, Scheme: "concepts", Level: "0", Metric: "citations", Result: eci.Result{Status: "ok", Countries: []string{"A", "B", "C", "D"}, Disciplines: []string{"x", "y"}, ActiveCountries: []int{0, 1, 2}, ActiveDisciplines: []int{0, 1}, ECI: []float64{1, 2, 2}, PCI: []float64{-1, 1}}}
	c, d, mr, err := scoreExports([]pipeline.ModelResult{m}, "base", "profile", "profile-run", "spectral-v1")
	if err != nil {
		t.Fatal(err)
	}
	if *c[0].GlobalRank != 3 || *c[1].GlobalRank != 1 || *c[2].GlobalRank != 1 || c[3].GlobalRank != nil || c[3].Valid || c[0].GlobalUniverse != 3 || *d[1].GlobalRank != 1 || mr[0].CountryUniverse != 3 {
		t.Fatal("global rank or inactive validity is wrong")
	}
	m.Result.Status = "disconnected"
	c, _, mr, err = scoreExports([]pipeline.ModelResult{m}, "base", "profile", "profile-run", "spectral-v1")
	if err != nil || c[0].Valid || c[0].GlobalRank != nil || c[0].GlobalUniverse != 0 || len(mr[0].CountryRanks) != 0 {
		t.Fatal("unidentifiable network got rank")
	}
	dir := t.TempDir()
	if _, err = writeScoreArtifact(dir, "scores.parquet", c); err != nil {
		t.Fatal(err)
	}
	read, err := parquet.ReadFile[scoreRow](filepath.Join(dir, "scores.parquet"))
	if err != nil || !reflect.DeepEqual(read, c) {
		t.Fatalf("nullable score rank did not survive parquet: %v", err)
	}
}

func TestLoadSourceUsesWholePrimaryFieldIncludingUnknownSubject(t *testing.T) {
	base, method := t.TempDir(), t.TempDir()
	dict := results.Dictionary{Countries: []results.Label{{ID: "A"}, {ID: "B"}, {ID: "C"}}}
	for _, l := range [][2]string{{"topics", "field"}, {"topics", "subfield"}, {"concepts", "0"}, {"concepts", "1"}} {
		dict.Layers = append(dict.Layers, results.Layer{Scheme: l[0], Level: l[1], Labels: []results.Label{{ID: "x"}, {ID: "y"}, {ID: "z"}}})
	}
	values := [][]float64{{10, 1, 1}, {5, 5, 1}, {1, 5, 10}}
	var rows []sourceRow
	var spectral, reflections []pipeline.ModelResult
	for _, layer := range dict.Layers {
		for i, c := range dict.Countries {
			for j, d := range layer.Labels {
				row := sourceRow{Scenario: results.MainScenarioID, Country: c.ID, Year: 2000, Scheme: layer.Scheme, Level: layer.Level, Discipline: d.ID, Publications: values[i][j], Citations: 10 * values[i][j]}
				rows = append(rows, row)
				if layer.Scheme == "topics" && layer.Level == "field" {
					row.Scenario = "whole-concept-whole-0.3"
					rows = append(rows, row)
				}
			}
		}
		for _, metric := range []string{"publications", "citations"} {
			m, e := dict.Matrix(nil, layer.Scheme, layer.Level, 2000, metric)
			if e != nil {
				t.Fatal(e)
			}
			for i := range m.Values {
				for j := range m.Values[i] {
					m.Values[i][j] = values[i][j]
					if metric == "citations" {
						m.Values[i][j] *= 10
					}
				}
			}
			r, e := eci.Compute(m)
			if e != nil {
				t.Fatal(e)
			}
			jr, _, e := eci.ReflectionsFromPrepared(r, eci.DefaultReflectionConfig())
			if e != nil {
				t.Fatal(e)
			}
			spectral = append(spectral, pipeline.ModelResult{Year: 2000, Scheme: layer.Scheme, Level: layer.Level, Metric: metric, Result: r})
			reflections = append(reflections, pipeline.ModelResult{Year: 2000, Scheme: layer.Scheme, Level: layer.Level, Metric: metric, Result: jr})
		}
	}
	rows = append(rows, sourceRow{Scenario: "whole-concept-whole-0.3", Country: "A", Year: 2000, Scheme: "topics", Level: "field", Discipline: "UNKNOWN", Publications: 7, Citations: 3})
	writeJSONTest(t, filepath.Join(base, "run.json"), map[string]any{"run_id": "fixture", "status": "full_snapshot_complete", "config": map[string]any{"year_start": 2000, "year_end": 2000, "scope": map[string]any{"corpus": "core"}}})
	writeJSONTest(t, filepath.Join(base, "dictionary.json"), dict)
	writeJSONTest(t, filepath.Join(base, "scenarios.json"), results.DefaultScenarios())
	writeJSONTest(t, filepath.Join(base, "eci.json"), spectral)
	if err := parquet.WriteFile(filepath.Join(base, "statistics.parquet"), rows); err != nil {
		t.Fatal(err)
	}
	var arts []checkpoint.Artifact
	for _, name := range []string{"dictionary.json", "scenarios.json", "eci.json", "statistics.parquet"} {
		a, e := checkpoint.Hash(filepath.Join(base, name))
		if e != nil {
			t.Fatal(e)
		}
		arts = append(arts, a)
	}
	run, _ := checkpoint.Hash(filepath.Join(base, "run.json"))
	writeJSONTest(t, filepath.Join(base, "completion.json"), map[string]any{"run_id": "fixture", "run": run, "artifacts": arts})
	comp, _ := checkpoint.Hash(filepath.Join(base, "completion.json"))
	writeJSONTest(t, filepath.Join(method, "manifest.json"), map[string]any{"base_run_id": "fixture", "source_completion_sha256": comp.SHA256})
	writeJSONTest(t, filepath.Join(method, "java-reflections-v1.json"), reflections)
	ma, _ := checkpoint.Hash(filepath.Join(method, "manifest.json"))
	ja, _ := checkpoint.Hash(filepath.Join(method, "java-reflections-v1.json"))
	writeJSONTest(t, filepath.Join(method, "completion.json"), map[string]any{"status": "complete", "base_run_id": "fixture", "manifest_sha256": ma.SHA256, "artifacts": []checkpoint.Artifact{ma, ja}})
	loaded, err := loadSource(context.Background(), base, method, "core")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Inputs) != 8 {
		t.Fatal("all four layers/two bases not extracted")
	}
	for _, in := range loaded.Inputs {
		if in.Input.WholePublications[0] != 19 || in.Input.WholeCitations[0] != 123 {
			t.Fatal("whole scale missed UNKNOWN primary subject or counted Concepts hits")
		}
		if in.Input.Matrix.Values[0][0] != 10 && in.Metric == "publications" {
			t.Fatal("model X did not preserve main-scenario cell")
		}
	}
	if _, err = wholeInteger(math.NaN()); err == nil {
		t.Fatal("NaN whole count accepted")
	}
}

func writeJSONTest(t *testing.T, path string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
}
