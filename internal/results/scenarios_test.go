package results

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/aggregate"
	"openalexstats/internal/eci"
	"openalexstats/internal/model"
)

func TestResolveScenariosDefaultsAndValidation(t *testing.T) {
	defaults, err := ResolveScenarios(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(defaults, DefaultScenarios()) {
		t.Fatal("default scenario contract changed")
	}
	original := DefaultScenarios()
	copied, err := ResolveScenarios(original)
	if err != nil {
		t.Fatal(err)
	}
	copied[0].ID = "mutated"
	if original[0].ID == "mutated" {
		t.Fatal("resolved scenarios alias input")
	}
	invalid := [][]Scenario{{{ID: "empty-owner", CountryMode: "whole", ConceptMode: "fractional", ConceptThreshold: .3, Main: true}}, {{ID: "bad-mode", CountryMode: "", ConceptMode: "fractional", ConceptThreshold: .3, Main: true}}, {{ID: " padded ", CountryMode: "fractional", ConceptMode: "fractional", ConceptThreshold: .3, Main: true}}}
	for _, threshold := range []float64{math.NaN(), math.Inf(1), -.1, 1.1} {
		invalid = append(invalid, []Scenario{{ID: "bad-threshold", CountryMode: "fractional", ConceptMode: "fractional", ConceptThreshold: threshold, Main: true}})
	}
	two := DefaultScenarios()
	two[0].Main = true
	invalid = append(invalid, two)
	duplicate := DefaultScenarios()
	duplicate[0].ID = duplicate[1].ID
	invalid = append(invalid, duplicate)
	for i, scenarios := range invalid {
		if _, err = ResolveScenarios(scenarios); err == nil {
			t.Fatalf("accepted invalid scenario definition %d", i)
		}
	}
}
func TestDenseArbitraryThresholds(t *testing.T) {
	scenarios := []Scenario{{ID: "fractional-main-02", CountryMode: "fractional", ConceptMode: "fractional", ConceptThreshold: .2, Main: true}, {ID: "whole-04", CountryMode: "whole", ConceptMode: "fractional", ConceptThreshold: .4}}
	resolved, err := ResolveScenarios(scenarios)
	if err != nil {
		t.Fatal(err)
	}
	d, err := NewDense(denseTestDictionary(), resolved, 2000, 2002)
	if err != nil {
		t.Fatal(err)
	}
	works := []model.Work{{Year: 2000, Countries: []string{"CN", "US"}, FieldID: "F1", SubfieldID: "S1", Concepts: []model.Concept{{ID: "A", Level: 0, Score: .1}, {ID: "A", Level: 0, Score: .25}, {ID: "B", Level: 0, Score: .35}, {ID: "C", Level: 0, Score: .6}, {ID: "A", Level: 1, Score: .3}}}, {Year: 1990, Concepts: []model.Concept{{ID: "B", Level: 1, Score: .4}}}}
	counts := [][]uint64{{1, 2, 0}, {0, 4, 1}}
	for i, w := range works {
		if err = d.AddSeries(w, counts[i]); err != nil {
			t.Fatal(err)
		}
	}
	var expected []Input
	for _, s := range resolved {
		a, _ := aggregate.New(s.Config())
		for i, w := range works {
			if err = a.AddSeries(w, 2000, counts[i], w.Year >= 2000 && w.Year <= 2002); err != nil {
				t.Fatal(err)
			}
		}
		expected = append(expected, Input{s, a.Rows(), a.Quality()})
	}
	compareInputs(t, d.Inputs(), expected)
}
func TestExportBindsDynamicModelScenario(t *testing.T) {
	scenarios := []Scenario{{ID: "custom-main-04", CountryMode: "fractional", ConceptMode: "fractional", ConceptThreshold: .4, Main: true}, {ID: "custom-whole-02", CountryMode: "whole", ConceptMode: "fractional", ConceptThreshold: .2}}
	result := eci.Result{Status: "empty", Countries: []string{"CN"}, Disciplines: []string{"F1"}, RCA: [][]float64{{1}}, Advantage: [][]bool{{true}}, ActiveCountries: []int{0}, ActiveDisciplines: []int{0}}
	models := []ModelInput{{Year: 2000, Scheme: "topics", Level: "field", Metric: "publications", Result: result}}
	rows := []Row{{ScenarioID: scenarios[0].ID, CountryMode: "fractional", ConceptMode: "fractional", ConceptThreshold: .4, Main: true, Country: "CN", Year: 2000, Scheme: "topics", Level: "field", DisciplineID: "F1", Publications: 1}}
	dir := t.TempDir()
	modelWithExtra := []map[string]any{{"year": 2000, "scheme": "topics", "level": "field", "metric": "publications", "result": result, "extra_provenance": "preserved"}}
	if err := Export(dir, rows, nil, scenarios, modelWithExtra, Dictionary{}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "eci.json"))
	if err != nil {
		t.Fatal(err)
	}
	var actual []ModelInput
	if err = json.Unmarshal(b, &actual); err != nil {
		t.Fatal(err)
	}
	if actual[0].ScenarioID != scenarios[0].ID || !reflect.DeepEqual(actual[0].Result, result) {
		t.Fatal("model scenario was not explicit, or numerical payload changed")
	}
	var extra []map[string]json.RawMessage
	if err = json.Unmarshal(b, &extra); err != nil || string(extra[0]["extra_provenance"]) != `"preserved"` {
		t.Fatal("unrelated model metadata was lost", err)
	}
	scores, err := parquet.ReadFile[ScoreRow](filepath.Join(dir, "country_eci.parquet"))
	if err != nil || scores[0].ScenarioID != scenarios[0].ID {
		t.Fatal("long score table has wrong scenario", err)
	}
	models[0].ScenarioID = MainScenarioID
	if err = Export(t.TempDir(), rows, nil, scenarios, models, Dictionary{}); err == nil {
		t.Fatal("model explicitly bound to wrong scenario was accepted")
	}
	models[0].ScenarioID = scenarios[0].ID
	if err = Export(t.TempDir(), rows, nil, scenarios, models, Dictionary{}); err != nil {
		t.Fatal(err)
	}
}
