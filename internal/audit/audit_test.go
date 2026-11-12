package audit

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/aggregate"
	"openalexstats/internal/cache"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/graph"
	"openalexstats/internal/pipeline"
	"openalexstats/internal/policy"
	"openalexstats/internal/production"
	"openalexstats/internal/results"
)

func controlledRun() production.Report {
	scope, _ := policy.New("core", 0, auditedBlacklist)
	return production.Report{Version: "production-v1", Status: "full_snapshot_complete", RunID: "fixture-run", SourceRows: 10,
		Config: pipeline.Config{Scope: scope, YearStart: 2000, YearEnd: 2026, Input: pipeline.Input{Paths: []string{"source"}, ManifestSHA256: strings.Repeat("a", 64), ManifestFiles: 1, ManifestWorks: 10}}}
}
func TestSupportedScopeIsExplicit(t *testing.T) {
	base := controlledRun()
	if err := supported(base, results.DefaultScenarios()); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*production.Report){func(r *production.Report) { r.Config.Scope.Corpus = "unsupported" }, func(r *production.Report) { r.Config.Scope.CorpusYearStart = 2000 }, func(r *production.Report) { r.Config.IncludeWorkSelfLoops = true }, func(r *production.Report) { r.Status = "pilot_complete" }, func(r *production.Report) { r.Config.YearEnd = 2025 }, func(r *production.Report) { r.Config.Scope.Blacklist = []string{"dataset"} }} {
		r := base
		change(&r)
		if err := supported(r, results.DefaultScenarios()); err == nil {
			t.Fatal("unsupported scope silently accepted")
		}
	}
	scenarios := results.DefaultScenarios()
	scenarios[0].ConceptThreshold = .4
	if err := supported(base, scenarios); err == nil {
		t.Fatal("unsupported threshold silently accepted")
	}
}
func TestSQLContainsOneIndependentReferenceJoin(t *testing.T) {
	cfg := Config{ResultDir: "result'o", WorkDir: "work'o", Threads: 2, MemoryLimit: "64MB"}
	sql := buildSQL(cfg, []string{"cache'part.parquet"}, auditedBlacklist)
	for _, text := range []string{"quality_null_xpac IS FALSE", "year BETWEEN 2000 AND 2026", "UNNEST(list_distinct(src.refs))", "t.id <> src.id", "concept_scores0[i] >= 0.3", "country_mode='fractional'", "cn_weight", "gb_biology", "cache''part.parquet"} {
		if !strings.Contains(sql, text) {
			t.Errorf("SQL missing %q", text)
		}
	}
	if strings.Count(sql, "JOIN q t ON") != 1 {
		t.Fatal("oracle must have one complete edge join")
	}
	if strings.Contains(sql, ".edges") || strings.Contains(sql, ".counts") {
		t.Fatal("oracle reads Go graph intermediates")
	}
}
func TestChecksRequireExactIntegerConservation(t *testing.T) {
	var r Report
	r.check("integer", 1e9, 1e9+1, false)
	r.check("fraction", 1, 1+1e-8, true)
	r.check("nan", 1, math.NaN(), true)
	if r.FailedChecks != 2 || r.Checks[0].Passed || !r.Checks[1].Passed || r.Checks[2].Passed {
		t.Fatalf("bad check semantics %+v", r.Checks)
	}
}

type normalizedFixture struct {
	ID                uint64    `parquet:"id"`
	Year              int32     `parquet:"year"`
	Type              string    `parquet:"type"`
	XPAC              bool      `parquet:"is_xpac"`
	Paratext          bool      `parquet:"is_paratext"`
	Retracted         bool      `parquet:"is_retracted"`
	Countries         []string  `parquet:"countries,list"`
	Field             string    `parquet:"field_id"`
	Subfield          string    `parquet:"subfield_id"`
	ConceptIDs0       []string  `parquet:"concept_ids0,list"`
	ConceptScores0    []float64 `parquet:"concept_scores0,list"`
	ConceptIDs1       []string  `parquet:"concept_ids1,list"`
	ConceptScores1    []float64 `parquet:"concept_scores1,list"`
	References        []uint64  `parquet:"references,list"`
	InvalidReferences int64     `parquet:"quality_invalid_references"`
	InvalidConcepts   int64     `parquet:"quality_invalid_concepts"`
	MissingYear       bool      `parquet:"quality_missing_year"`
	NullXPAC          bool      `parquet:"quality_null_xpac"`
}

func writeParquet[T any](t *testing.T, path string, rows []T) {
	t.Helper()
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	w := parquet.NewGenericWriter[T](f)
	if _, e = w.Write(rows); e != nil {
		t.Fatal(e)
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
}
func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	if e := checkpoint.JSON(path, value); e != nil {
		t.Fatal(e)
	}
}
func completion(t *testing.T, dir string) {
	t.Helper()
	run, e := hashFile(context.Background(), filepath.Join(dir, "run.json"))
	if e != nil {
		t.Fatal(e)
	}
	files := []FileHash{}
	for _, name := range []string{"cube.parquet", "statistics.parquet", "scenarios.json", "methodology.json", "dictionary.json", "eci.json", "country_eci.parquet", "discipline_pci.parquet"} {
		h, e := hashFile(context.Background(), filepath.Join(dir, name))
		if e != nil {
			t.Fatal(e)
		}
		files = append(files, h)
	}
	writeJSON(t, filepath.Join(dir, "completion.json"), map[string]any{"version": 1, "run_id": "fixture-run", "run": run, "artifacts": files})
}

// This fixture's expected graph is written by hand. It does not invoke the Go
// graph/aggregate kernels under audit. Only seven directed edges survive:
// 2000: 2->1,2->6; 2001:6->1,6->2; 2002:7->1,7->6; 2026:10->1.
func oracleFixture(t *testing.T, duckdb string) Config {
	return oracleFixtureMode(t, duckdb, "fractional")
}
func oracleFixtureMode(t *testing.T, duckdb, conceptMode string) Config {
	t.Helper()
	root := t.TempDir()
	cfg := Config{ResultDir: filepath.Join(root, "result"), CacheDir: filepath.Join(root, "cache"), WorkDir: filepath.Join(root, "audit"), DuckDB: duckdb, Threads: 2, MemoryLimit: "128MB"}
	if e := os.MkdirAll(cfg.ResultDir, 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(cfg.CacheDir, 0755); e != nil {
		t.Fatal(e)
	}
	bio := "https://openalex.org/C86803240"
	field17 := "https://openalex.org/fields/17"
	field11 := "https://openalex.org/fields/11"
	rows := []normalizedFixture{
		{ID: 1, Year: 1995, Type: "article", Countries: []string{" CN ", "US", "CN"}, Field: field17, Subfield: "s", ConceptIDs0: []string{bio, "CX", bio}, ConceptScores0: []float64{.9, .8, .2}, ConceptIDs1: []string{bio, "LX", bio}, ConceptScores1: []float64{.7, .2, .1}},
		{ID: 2, Year: 2000, Type: "article", Countries: []string{"CN"}, Field: field11, Subfield: "s", ConceptIDs0: []string{bio}, ConceptScores0: []float64{.9}, References: []uint64{1, 1, 2, 3, 4, 5, 6, 999}},
		{ID: 3, Year: 2001, Type: "dataset", References: []uint64{1}},
		{ID: 4, Year: 2001, Type: "article", XPAC: true, References: []uint64{1}},
		{ID: 5, Year: 2001, Type: "article", NullXPAC: true, References: []uint64{1}},
		{ID: 6, Year: 2001, Type: "article", Countries: []string{"GB"}, Field: field17, Subfield: "s", ConceptIDs0: []string{bio, bio}, ConceptScores0: []float64{.2, .4}, References: []uint64{1, 2, 6}},
		{ID: 7, Year: 2002, Type: "article", References: []uint64{1, 6}},
		{ID: 8, Type: "article", MissingYear: true, References: []uint64{1}},
		{ID: 9, Year: 2001, Type: "editorial"},
		{ID: 10, Year: 2026, Type: "article", Countries: []string{"CN"}, Field: field17, Subfield: "s", ConceptIDs0: []string{bio}, ConceptScores0: []float64{.3}, References: []uint64{1}},
	}
	cachePath := filepath.Join(cfg.CacheDir, "part.parquet")
	writeParquet(t, cachePath, rows)
	h, e := hashFile(context.Background(), cachePath)
	if e != nil {
		t.Fatal(e)
	}
	run := controlledRun()
	rawPath := filepath.Join(root, "raw.parquet")
	run.Config.Input.Paths = []string{rawPath}
	run.SelectedWorks = 6
	run.Graph = production.EdgeStats{Stats: graph.Stats{SourceRecords: 10, MemberSourceRecords: 6, ValidYearSourceRecords: 4, RetainedEdges: 7, SelfLoops: 2}, FutureTargetEdges: 1}
	run.Inventory = production.Inventory{Rows: 10, Selected: 6, Core: production.GroupCoverage{Works: 8}, XPAC: production.GroupCoverage{Works: 1}, NullXPAC: production.GroupCoverage{Works: 1}, TypeCounts: map[string][3]uint64{"article": {6, 1, 1}, "dataset": {1, 0, 0}, "editorial": {1, 0, 0}}}
	run.Quality = aggregate.Quality{PublicationWorks: 4, CitationEvents: 7}
	manifest := cache.Manifest{FormatVersion: cache.FormatVersion, Complete: true, Rows: 10, SourceManifest: run.Config.Input.ManifestSHA256, Parts: []cache.Part{{InputPath: rawPath, Path: cachePath, Bytes: h.Bytes, SHA256: h.SHA256, Rows: 10}}}
	writeJSON(t, filepath.Join(cfg.CacheDir, "manifest.json"), manifest)
	cacheKey, e := cacheIdentity(manifest, run.Config.Input.Paths)
	if e != nil {
		t.Fatal(e)
	}
	writeJSON(t, filepath.Join(cfg.ResultDir, "methodology.json"), map[string]any{"run_id": run.RunID, "normalized_cache_key": cacheKey})
	type key struct {
		country, scheme, level, id string
		year                       int
	}
	scenarios := auditScenarios(conceptMode)
	main, _ := mainAuditScenario(scenarios)
	buildRows := func(s results.Scenario) []aggregate.Row {
		cube := map[key]aggregate.Row{}
		add := func(target, year int, citation bool) {
			row := rows[target-1]
			unique := func(values []string) []string {
				seen := map[string]bool{}
				var ids []string
				for _, id := range values {
					id = strings.TrimSpace(id)
					if id != "" && !seen[id] {
						seen[id] = true
						ids = append(ids, id)
					}
				}
				if len(ids) == 0 {
					ids = []string{aggregate.Unknown}
				}
				return ids
			}
			countries := unique(row.Countries)
			concepts := func(ids []string, scores []float64) []string {
				var labels []string
				for i, id := range ids {
					if !math.IsNaN(scores[i]) && !math.IsInf(scores[i], 0) && scores[i] >= s.ConceptThreshold {
						labels = append(labels, id)
					}
				}
				return unique(labels)
			}
			field, subfield := strings.TrimSpace(row.Field), strings.TrimSpace(row.Subfield)
			if field == "" {
				field = aggregate.Unknown
			}
			if subfield == "" {
				subfield = aggregate.Unknown
			}
			layers := []struct {
				scheme, level string
				ids           []string
			}{{"topics", "field", []string{field}}, {"topics", "subfield", []string{subfield}}, {"concepts", "0", concepts(row.ConceptIDs0, row.ConceptScores0)}, {"concepts", "1", concepts(row.ConceptIDs1, row.ConceptScores1)}}
			for _, layer := range layers {
				for _, country := range countries {
					for _, id := range layer.ids {
						k := key{country, layer.scheme, layer.level, id, year}
						v := cube[k]
						v.Country = country
						v.Scheme = layer.scheme
						v.Level = layer.level
						v.DisciplineID = id
						v.Year = year
						weight := 1.0
						if s.CountryMode == "fractional" {
							weight /= float64(len(countries))
						}
						if layer.scheme == "concepts" && s.ConceptMode == "fractional" {
							weight /= float64(len(layer.ids))
						}
						if citation {
							v.Citations += weight
						} else {
							v.Publications += weight
						}
						cube[k] = v
					}
				}
			}
		}
		for _, pair := range [][2]int{{2, 2000}, {6, 2001}, {7, 2002}, {10, 2026}} {
			add(pair[0], pair[1], false)
		}
		for _, pair := range [][2]int{{1, 2000}, {6, 2000}, {1, 2001}, {2, 2001}, {1, 2002}, {6, 2002}, {1, 2026}} {
			add(pair[0], pair[1], true)
		}
		var out []aggregate.Row
		for _, r := range cube {
			out = append(out, r)
		}
		return out
	}
	cubeRows := buildRows(main)
	writeParquet(t, filepath.Join(cfg.ResultDir, "cube.parquet"), cubeRows)
	run.CubeRows = len(cubeRows)
	run.Config.Weighting = main.Config()
	var statistics []results.Row
	for _, s := range scenarios {
		for _, r := range buildRows(s) {
			statistics = append(statistics, results.Row{ScenarioID: s.ID, CountryMode: s.CountryMode, ConceptMode: s.ConceptMode, ConceptThreshold: s.ConceptThreshold, Main: s.Main, Country: r.Country, Year: r.Year, Scheme: r.Scheme, Level: r.Level, DisciplineID: r.DisciplineID, Publications: r.Publications, Citations: r.Citations})
		}
	}
	writeParquet(t, filepath.Join(cfg.ResultDir, "statistics.parquet"), statistics)
	run.StatisticsRows = len(statistics)
	writeJSON(t, filepath.Join(cfg.ResultDir, "scenarios.json"), scenarios)
	writeOracleModelFixtures(t, cfg.ResultDir, cubeRows)
	writeJSON(t, filepath.Join(cfg.ResultDir, "run.json"), run)
	completion(t, cfg.ResultDir)
	return cfg
}

func TestDuckDBOracleHandCountedFixture(t *testing.T) {
	duckdb := os.Getenv("OA_DUCKDB")
	if duckdb == "" {
		var err error
		duckdb, err = exec.LookPath("duckdb")
		if err != nil {
			t.Skip("DuckDB CLI unavailable; run with OA_DUCKDB=/path/to/duckdb for SQL integration")
		}
	}
	cfg := oracleFixture(t, duckdb)
	report, err := Run(context.Background(), cfg, nil)
	if err != nil {
		stderr, _ := os.ReadFile(filepath.Join(cfg.WorkDir, "duckdb.stderr.log"))
		t.Fatalf("oracle failed: %v\n%s", err, stderr)
	}
	if !report.Passed || report.FailedChecks != 0 || len(report.Checks) < 1800 {
		t.Fatalf("incomplete audit %+v", report)
	}
	var persisted Report
	if err := readJSON(filepath.Join(cfg.WorkDir, "audit.json"), &persisted); err != nil || !persisted.Passed {
		t.Fatalf("audit evidence not persisted: %v", err)
	}
	// Change one published cube count, and republish valid file hashes. The
	// independent implementation must still detect the semantic disagreement.
	rows, err := parquet.ReadFile[aggregate.Row](filepath.Join(cfg.ResultDir, "cube.parquet"))
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	for i := range rows {
		if rows[i].Country == "CN" && rows[i].Scheme == "topics" && rows[i].Level == "field" && rows[i].Year == 2000 {
			rows[i].Publications++
			changed = true
			break
		}
	}
	if !changed {
		t.Fatal("fixture has no cell to alter")
	}
	writeParquet(t, filepath.Join(cfg.ResultDir, "cube.parquet"), rows)
	completion(t, cfg.ResultDir)
	report, err = Run(context.Background(), cfg, nil)
	if err == nil || report.Passed || report.FailedChecks == 0 {
		t.Fatal("semantic output corruption escaped independent oracle")
	}
}
func TestHashCancellationAndUnsupportedRunEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")
	if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := hashFile(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("hash ignored cancellation: %v", err)
	}
	cfg := oracleFixture(t, "not-a-real-duckdb")
	var run production.Report
	if err := readJSON(filepath.Join(cfg.ResultDir, "run.json"), &run); err != nil {
		t.Fatal(err)
	}
	run.Config.Scope.Corpus = "unsupported"
	writeJSON(t, filepath.Join(cfg.ResultDir, "run.json"), run)
	report, err := Run(context.Background(), cfg, nil)
	if err == nil || report.Status != "failed" {
		t.Fatal("unsupported scope accepted")
	}
	var payload map[string]json.RawMessage
	if err := readJSON(filepath.Join(cfg.WorkDir, "audit.json"), &payload); err != nil {
		t.Fatal("failure did not leave audit evidence")
	}
}
