package production

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/aggregate"
	"openalexstats/internal/cache"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/pipeline"
	"openalexstats/internal/policy"
	"openalexstats/internal/results"
)

type testLabel struct {
	ID    string `parquet:"id"`
	Name  string `parquet:"display_name"`
	Level int32  `parquet:"level"`
}
type runTopicID struct {
	ID string `parquet:"id"`
}
type runTopic struct {
	Field    runTopicID `parquet:"field"`
	Subfield runTopicID `parquet:"subfield"`
}
type runAuthor struct {
	Countries []string `parquet:"countries,list"`
}
type runConcept struct {
	ID    string  `parquet:"id"`
	Level int32   `parquet:"level"`
	Score float32 `parquet:"score"`
}
type runWork struct {
	ID         string       `parquet:"id"`
	Year       int32        `parquet:"publication_year"`
	Type       string       `parquet:"type"`
	XPAC       bool         `parquet:"is_xpac"`
	Paratext   bool         `parquet:"is_paratext"`
	Retracted  bool         `parquet:"is_retracted"`
	Authors    []runAuthor  `parquet:"authorships,list"`
	Topic      runTopic     `parquet:"primary_topic"`
	Concepts   []runConcept `parquet:"concepts,list"`
	References []string     `parquet:"referenced_works,list"`
}

func setupCompleteRunFixture(t *testing.T) (pipeline.Input, string) {
	t.Helper()
	root := t.TempDir()
	type entity struct {
		Entity      string           `json:"entity"`
		RecordCount int64            `json:"record_count"`
		Files       []map[string]any `json:"files"`
	}
	var entities []entity
	write := func(name string, rows []testLabel) {
		t.Helper()
		dir := filepath.Join(root, name)
		if e := os.MkdirAll(dir, 0755); e != nil {
			t.Fatal(e)
		}
		if e := parquet.WriteFile(filepath.Join(dir, "part.parquet"), rows); e != nil {
			t.Fatal(e)
		}
		entities = append(entities, entity{name, int64(len(rows)), []map[string]any{{"url": "s3://openalex/data/parquet/" + name + "/part.parquet"}}})
	}
	write("countries", []testLabel{{ID: "https://openalex.org/countries/CN", Name: "China"}, {ID: "https://openalex.org/countries/US", Name: "United States"}, {ID: "https://openalex.org/countries/GB", Name: "United Kingdom"}})
	write("fields", []testLabel{{ID: "BIO", Name: "Biology"}, {ID: "COMP", Name: "Computer science"}})
	write("subfields", []testLabel{{ID: "BIO-S", Name: "Genetics"}, {ID: "COMP-S", Name: "AI"}})
	write("concepts", []testLabel{{ID: "BIO-C0", Name: "Biology", Level: 0}, {ID: "COMP-C0", Name: "Computing", Level: 0}, {ID: "BIO-C1", Name: "Genetics", Level: 1}, {ID: "COMP-C1", Name: "AI", Level: 1}})
	work := func(id string, y int32, typ, country, field string, refs ...string) runWork {
		return runWork{ID: id, Year: y, Type: typ, Authors: []runAuthor{{Countries: []string{country}}}, Topic: runTopic{runTopicID{field}, runTopicID{field + "-S"}}, Concepts: []runConcept{{field + "-C0", 0, .9}, {field + "-C1", 1, .4}}, References: refs}
	}
	rows := []runWork{work("W1", 1995, "article", "US", "BIO"), work("W2", 2001, "article", "CN", "COMP", "W1", "W1", "W2", "W3", "W99"), work("W3", 2001, "dataset", "US", "BIO"), work("W4", 2002, "article", "GB", "BIO", "W1", "W2"), work("W5", 2003, "preprint", "CN", "COMP", "W2")}
	// A cross-country work hits two same-level subjects. Repeated BIO is one
	// hit, while each distinct retained label counts once in the new default.
	rows[1].Authors = []runAuthor{{Countries: []string{"CN", "US", "CN"}}}
	rows[1].Concepts = append(rows[1].Concepts, runConcept{"BIO-C0", 0, .8}, runConcept{"BIO-C0", 0, .7})
	if e := os.MkdirAll(filepath.Join(root, "works"), 0755); e != nil {
		t.Fatal(e)
	}
	if e := parquet.WriteFile(filepath.Join(root, "works", "part.parquet"), rows); e != nil {
		t.Fatal(e)
	}
	entities = append(entities, entity{"works", 5, []map[string]any{{"url": "s3://openalex/data/parquet/works/part.parquet"}}})
	manifest := filepath.Join(root, "manifest.json")
	b, e := json.Marshal(map[string]any{"date": "2026-06-26", "entities": entities})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(manifest, b, 0600); e != nil {
		t.Fatal(e)
	}
	input, e := pipeline.Resolve(manifest, nil, 0)
	if e != nil {
		t.Fatal(e)
	}
	return input, manifest
}

func TestProductionCustomMainThreshold(t *testing.T) {
	input, manifest := setupCompleteRunFixture(t)
	root := t.TempDir()
	ctx := context.Background()
	cacheDir := filepath.Join(root, "cache")
	if _, err := cache.Prepare(ctx, input.Paths, cache.Options{Directory: cacheDir, BuildID: "projection", ManifestSHA256: input.ManifestSHA256, ExpectedRows: 5, Workers: 2}, nil); err != nil {
		t.Fatal(err)
	}
	scope, _ := policy.New("core", 0, policy.DefaultBlacklist)
	scenarios := []results.Scenario{{ID: "custom-main-0.8", CountryMode: "fractional", ConceptMode: "fractional", ConceptThreshold: .8, Main: true}, {ID: "sensitivity-0.2", CountryMode: "fractional", ConceptMode: "fractional", ConceptThreshold: .2}}
	cfg := Config{Base: pipeline.Config{Input: input, Scope: scope, YearStart: 2000, YearEnd: 2003, Workers: 3, Readers: 2, MaxMemoryBytes: 1 << 30, OutputDir: filepath.Join(root, "result")}, CacheDirectory: cacheDir, WorkDirectory: filepath.Join(root, "work"), DictionaryManifest: manifest, BuildID: "custom-test", Scenarios: scenarios, Partitions: 3}
	r, err := Run(ctx, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.Graph.RetainedEdges != 4 || r.SelectedWorks != 4 || r.Config.Weighting.ConceptThreshold != .8 {
		t.Fatalf("configuration changed Q/graph or was ignored: %+v", r)
	}
	rows, err := parquet.ReadFile[aggregate.Row](filepath.Join(cfg.Base.OutputDir, "cube.parquet"))
	if err != nil {
		t.Fatal(err)
	}
	var pubs, citations float64
	for _, row := range rows {
		if row.Scheme == "concepts" && row.Level == "1" {
			if row.DisciplineID != aggregate.Unknown {
				t.Fatalf("lower-score tag survived main threshold: %+v", row)
			}
			pubs += row.Publications
			citations += row.Citations
		}
	}
	if pubs != 3 || citations != 4 {
		t.Fatalf("UNKNOWN conservation lost: %g/%g", pubs, citations)
	}
	var models []results.ModelInput
	if err = checkpoint.ReadJSON(filepath.Join(cfg.Base.OutputDir, "eci.json"), &models); err != nil {
		t.Fatal(err)
	}
	for _, m := range models {
		if m.ScenarioID != "custom-main-0.8" {
			t.Fatal("custom model mislabeled", m.ScenarioID)
		}
	}
}
func TestProductionEndToEndCompletenessAndResume(t *testing.T) {
	ctx := context.Background()
	input, manifest := setupCompleteRunFixture(t)
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	if _, e := cache.Prepare(ctx, input.Paths, cache.Options{Directory: cacheDir, BuildID: "test-projection", ManifestSHA256: input.ManifestSHA256, ExpectedRows: 5, Workers: 2}, nil); e != nil {
		t.Fatal(e)
	}
	scope, _ := policy.New("core", 0, policy.DefaultBlacklist)
	cfg := Config{Base: pipeline.Config{Input: input, Scope: scope, YearStart: 2000, YearEnd: 2003, Workers: 3, Readers: 2, MaxMemoryBytes: 1 << 30, OutputDir: filepath.Join(root, "result")}, CacheDirectory: cacheDir, WorkDirectory: filepath.Join(root, "work"), DictionaryManifest: manifest, BuildID: "production-test", Partitions: 3, EdgeWorkers: 2, CountWorkers: 2}
	report, e := Run(ctx, cfg, nil)
	if e != nil {
		t.Fatal(e)
	}
	if report.Status != "full_snapshot_complete" || report.SourceRows != 5 || report.SelectedWorks != 4 || report.Graph.RetainedEdges != 4 || report.Graph.SelfLoops != 1 || report.Graph.DuplicateReferences != 1 || report.Graph.MissingTargetWorks != 1 || report.Graph.ExcludedTargetWorks != 1 || report.Models != 32 {
		t.Fatalf("incorrect report: %+v", report)
	}
	if !report.Validation.RawRowConservation || !report.Validation.FractionalCubeConservation {
		t.Fatal("missing conservation evidence")
	}
	if !report.Validation.AssignmentConservation || !report.Validation.WholeCountIntegrality || report.Quality.PublicationAssignments["concepts.0"] != 4 || report.Quality.CitationAssignments["concepts.0"] != 6 {
		t.Fatalf("multi-label assignment validation missing: %+v", report.Quality)
	}
	stats, e := parquet.ReadFile[results.Row](filepath.Join(cfg.Base.OutputDir, "statistics.parquet"))
	if e != nil {
		t.Fatal(e)
	}
	if len(stats) == 0 {
		t.Fatal("no real outputs")
	}
	known := false
	wholeHit, fractionalCountryHit := false, false
	for _, row := range stats {
		if row.ScenarioID == results.MainScenarioID && row.Country == "US" && row.Year == 2001 && row.Scheme == "topics" && row.Level == "field" && row.DisciplineID == "BIO" {
			known = row.Citations == 1
		}
		if row.Year == 2001 && row.Scheme == "concepts" && row.Level == "0" && row.DisciplineID == "BIO-C0" && row.Country == "CN" {
			if row.ScenarioID == "whole-concept-whole-0.3" {
				wholeHit = row.Publications == 1
			}
			if row.ScenarioID == results.MainScenarioID {
				fractionalCountryHit = row.Publications == .5
			}
		}
	}
	if !known {
		t.Fatal("old target / current citation attribution lost")
	}
	if !wholeHit || !fractionalCountryHit {
		t.Fatal("distinct concept hit was split, duplicated, or country weighting lost")
	}
	prior, e := Run(ctx, cfg, nil)
	if e != nil {
		t.Fatal(e)
	}
	if prior.CompletedAt != report.CompletedAt || prior.RunID != report.RunID {
		t.Fatal("completed run was recomputed")
	}
	// A corrupt final output must be repaired, never accepted solely from a
	// completion marker. Source/target checkpoints remain idempotently reusable.
	if e = os.WriteFile(filepath.Join(cfg.Base.OutputDir, "statistics.parquet"), []byte("damaged"), 0600); e != nil {
		t.Fatal(e)
	}
	repaired, e := Run(ctx, cfg, nil)
	if e != nil {
		t.Fatal(e)
	}
	if repaired.EdgePartsReused != 1 || repaired.CountPartitionsReused != 3 || !repaired.DimensionReused || repaired.Graph.RetainedEdges != 4 {
		t.Fatalf("recovery failed: %+v", repaired)
	}
	// A distinct dictionary manifest cannot silently reuse a completed run,
	// even when the caller deliberately keeps the same executable/build ID.
	content, e := os.ReadFile(manifest)
	if e != nil {
		t.Fatal(e)
	}
	alternate := filepath.Join(filepath.Dir(manifest), "dictionary-alternate.json")
	if e = os.WriteFile(alternate, append(content, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
	cfg.DictionaryManifest = alternate
	if _, e = Run(ctx, cfg, nil); e == nil {
		t.Fatal("changed dictionary manifest reused immutable results")
	}
	cfg.DictionaryManifest = manifest
	if e = parquet.WriteFile(filepath.Join(filepath.Dir(manifest), "fields", "part.parquet"), []testLabel{{ID: "BIO", Name: "Revised biology"}, {ID: "COMP", Name: "Computer science"}}); e != nil {
		t.Fatal(e)
	}
	if _, e = Run(ctx, cfg, nil); e == nil {
		t.Fatal("changed dictionary contents reused immutable results")
	}
}
