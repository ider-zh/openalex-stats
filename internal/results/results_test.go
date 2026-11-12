package results

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/aggregate"
	"openalexstats/internal/eci"
	"openalexstats/internal/model"
)

func near(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-10 {
		t.Fatalf("got %.12g want %.12g", got, want)
	}
}
func TestKnownMatrixDenominators(t *testing.T) {
	s := DefaultScenarios()[1]
	rows := []aggregate.Row{{Country: "CN", Year: 2000, Scheme: "topics", Level: "field", DisciplineID: "F1", Publications: 10, Citations: 4}, {Country: "CN", Year: 2000, Scheme: "topics", Level: "field", DisciplineID: "F2", Publications: 10}, {Country: "US", Year: 2000, Scheme: "topics", Level: "field", DisciplineID: "F1", Publications: 30, Citations: 8}, {Country: "US", Year: 2000, Scheme: "topics", Level: "field", DisciplineID: "F2", Citations: 2}, {Country: aggregate.Unknown, Year: 2000, Scheme: "topics", Level: "field", DisciplineID: "F1", Publications: 1000, Citations: 1000}, {Country: "CN", Year: 2000, Scheme: "topics", Level: "field", DisciplineID: aggregate.Unknown, Publications: 500, Citations: 500}}
	derived, _, err := Derive([]Input{{Scenario: s, Rows: rows}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range derived {
		near(t, r.PublicationMatrixTotal, 50)
		near(t, r.CitationMatrixTotal, 14)
		if r.Country == aggregate.Unknown || r.DisciplineID == aggregate.Unknown {
			if r.PublicationShareValid || r.PublicationRCAValid || r.CitationShareValid || r.CitationRCAValid {
				t.Fatal("unknown cell contaminated denominator")
			}
			continue
		}
		if r.Country == "CN" && r.DisciplineID == "F1" {
			near(t, r.PublicationShare, .25)
			near(t, r.PublicationRCA, .625)
			near(t, r.CitationShare, 1.0/3)
			near(t, r.CitationRCA, 7.0/6)
		}
	}
}
func TestThresholdAndWeightScenariosStaySeparate(t *testing.T) {
	work := model.Work{Year: 2000, Countries: []string{"CN", "US"}, FieldID: "F", SubfieldID: "S", Concepts: []model.Concept{{ID: "A", Level: 0, Score: .2}, {ID: "B", Level: 0, Score: .4}, {ID: "C", Level: 0, Score: .8}}}
	var inputs []Input
	eciScenarios := 0
	for _, s := range DefaultScenarios() {
		a, err := aggregate.New(s.Config())
		if err != nil {
			t.Fatal(err)
		}
		if s.ForECI() {
			eciScenarios++
		}
		if err = a.AddSeries(work, 2001, []uint64{7}, true); err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, Input{s, a.Rows(), a.Quality()})
	}
	derived, coverage, err := Derive(inputs)
	if err != nil {
		t.Fatal(err)
	}
	if eciScenarios != 1 || len(coverage) != 24 {
		t.Fatal("scenario design changed")
	}
	totals := map[string]float64{}
	labels := map[string]map[string]bool{}
	for _, r := range derived {
		if r.Scheme == "concepts" && r.Level == "0" && r.Year == 2000 {
			totals[r.ScenarioID] += r.Publications
			if labels[r.ScenarioID] == nil {
				labels[r.ScenarioID] = map[string]bool{}
			}
			labels[r.ScenarioID][r.DisciplineID] = true
		}
	}
	for _, s := range DefaultScenarios() {
		wantLabels := 3
		if s.ConceptThreshold == .3 {
			wantLabels = 2
		}
		if s.ConceptThreshold == .5 {
			wantLabels = 1
		}
		want := float64(wantLabels)
		if s.CountryMode == "whole" {
			want *= 2
		}
		near(t, totals[s.ID], want)
		if len(labels[s.ID]) != wantLabels {
			t.Fatal("threshold leaked across scenarios", s.ID, labels[s.ID])
		}
	}
	for _, c := range coverage {
		if c.PublicationWorks != 1 || c.CitationEvents != 7 {
			t.Fatal("raw work/event coverage inflated by countries", c)
		}
	}
	if _, _, err = Derive([]Input{inputs[0], inputs[0]}); err == nil {
		t.Fatal("duplicate scenario silently doubled")
	}
}
func TestDictionaryProjectionAndZeroColumns(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{}
	writeFixture(t, root, "fields", []dictionaryRow{{"F1", "Field One"}, {"F2", "Unobserved Field"}})
	writeFixture(t, root, "subfields", []subfieldDictionaryRow{{ID: "S1", Name: "Subfield", Field: &dictionaryParent{ID: "F1"}}})
	writeFixture(t, root, "countries", []dictionaryRow{{"https://openalex.org/countries/CN", "China"}, {"https://openalex.org/countries/US", "United States"}})
	writeFixture(t, root, "concepts", []conceptDictionaryRow{{"C0", "Top", 0}, {"C1", "Lower", 1}, {"C2", "Ignored", 2}})
	for _, name := range []string{"fields", "subfields", "countries", "concepts"} {
		files[name] = "s3://openalex/data/parquet/" + name + "/part.parquet"
	}
	manifestPath := writeManifest(t, root, files)
	d, err := LoadDictionary(context.Background(), manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Layers) != 4 || len(d.Layers[2].Labels) != 1 || len(d.Countries) != 2 {
		t.Fatal(d)
	}
	if len(d.Layers[1].Labels[0].ParentIDs) != 1 || d.Layers[1].Labels[0].ParentIDs[0] != "F1" {
		t.Fatal("snapshot subfield parent lost", d.Layers[1])
	}
	m, err := d.Matrix([]aggregate.Row{{Country: "CN", Year: 2000, Scheme: "topics", Level: "field", DisciplineID: "F1", Publications: 2}, {Country: aggregate.Unknown, Year: 2000, Scheme: "topics", Level: "field", DisciplineID: "F1", Publications: 99}}, "topics", "field", 2000, "publications")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Countries) != 2 || len(m.Disciplines) != 2 || m.Values[0][0] != 2 || m.Values[0][1] != 0 || m.Values[1][0] != 0 {
		t.Fatal("missing labels were not zero-filled", m)
	}
	if _, err = d.Matrix([]aggregate.Row{{Country: "CN", Year: 2000, Scheme: "topics", Level: "field", DisciplineID: "MISSING", Publications: 1}}, "topics", "field", 2000, "publications"); err == nil {
		t.Fatal("missing dictionary label disappeared silently")
	}
	observed := []aggregate.Row{{Country: "XK", Year: 2000, Scheme: "topics", Level: "field", DisciplineID: "MISSING", Publications: 1}}
	extended, e := d.Reconcile(observed)
	if e != nil {
		t.Fatal(e)
	}
	if len(extended.UnlistedLabels) != 2 || len(d.Countries) != 2 {
		t.Fatal("reconciliation must retain provenance and not mutate source dictionary")
	}
	if _, e = extended.Matrix(observed, "topics", "field", 2000, "publications"); e != nil {
		t.Fatal(e)
	}
	files["fields"] = "s3://openalex/data/parquet/../../escape.parquet"
	if _, err = LoadDictionary(context.Background(), writeManifest(t, root, files)); err == nil {
		t.Fatal("path traversal accepted")
	}
}
func writeFixture[T any](t *testing.T, root, name string, rows []T) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := parquet.WriteFile(filepath.Join(dir, "part.parquet"), rows); err != nil {
		t.Fatal(err)
	}
}
func writeManifest(t *testing.T, root string, files map[string]string) string {
	t.Helper()
	var entities []map[string]any
	for entity, url := range files {
		entities = append(entities, map[string]any{"entity": entity, "files": []map[string]string{{"url": url}}})
	}
	b, err := json.Marshal(map[string]any{"entities": entities})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "manifest.json")
	if err = os.WriteFile(p, b, 0644); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestExportPreservesUndefinedAndSchema(t *testing.T) {
	model := ModelInput{Year: 2000, Scheme: "topics", Level: "field", Metric: "publications", Result: eci.Result{Status: "disconnected", Countries: []string{"CN", "US"}, Disciplines: []string{"F1", "F2"}, ActiveCountries: []int{0, 1}, ActiveDisciplines: []int{0, 1}, ComponentCount: 2}}
	c, p, diagnostics, err := FlattenModels([]ModelInput{model})
	if err != nil {
		t.Fatal(err)
	}
	if len(c) != 2 || len(p) != 2 || c[0].Valid || p[0].Valid || diagnostics[0].Status != "disconnected" {
		t.Fatal("undefined model got fabricated scores")
	}
	dir := t.TempDir()
	rows := []Row{{ScenarioID: MainScenarioID, Country: "CN", Year: 2000, Scheme: "topics", Level: "field", DisciplineID: "F1", Publications: 2}}
	if err = Export(dir, rows, nil, DefaultScenarios(), []ModelInput{model}, Dictionary{}); err != nil {
		t.Fatal(err)
	}
	loaded, err := parquet.ReadFile[Row](filepath.Join(dir, "statistics.parquet"))
	if err != nil || len(loaded) != 1 || loaded[0].Country != "CN" {
		t.Fatalf("roundtrip: %v %+v", err, loaded)
	}
	scores, err := parquet.ReadFile[ScoreRow](filepath.Join(dir, "country_eci.parquet"))
	if err != nil || scores[0].Valid {
		t.Fatal("invalid score lost validity flag", err)
	}
	for _, name := range []string{"coverage.json", "scenarios.json", "dictionary.json", "eci.json", "model_diagnostics.json"} {
		b, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil || !json.Valid(b) {
			t.Fatal(name, e)
		}
	}
}
