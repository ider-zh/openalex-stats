package audit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openalexstats/internal/aggregate"
	"openalexstats/internal/cache"
	"openalexstats/internal/graph"
	"openalexstats/internal/policy"
	"openalexstats/internal/production"
	"openalexstats/internal/results"
)

func TestCoreAndAllAreExplicitSupportedScopes(t *testing.T) {
	for _, corpus := range []string{"core", "all"} {
		run := controlledRun()
		scope, err := policy.New(corpus, 0, auditedBlacklist)
		if err != nil {
			t.Fatal(err)
		}
		run.Config.Scope = scope
		if err := supported(run, auditScenarios("whole")); err != nil {
			t.Fatal(corpus, err)
		}
	}
	cfg := Config{ResultDir: "result", WorkDir: "work", Threads: 1, MemoryLimit: "128MB"}
	all := buildSQLForCorpus(cfg, []string{"normalized.parquet"}, auditedBlacklist, "all", auditScenarios("whole"))
	if !strings.Contains(all, "id > 0 AND (TRUE) AND is_paratext IS NOT TRUE") || strings.Contains(all, "id > 0 AND (is_xpac IS FALSE") {
		t.Fatal("all SQL retains core-only membership filter")
	}
	if strings.Count(all, "JOIN q t ON") != 1 {
		t.Fatal("all must reconstruct one jointly induced graph")
	}
	core := buildSQLForCorpus(cfg, []string{"normalized.parquet"}, auditedBlacklist, "core", auditScenarios("whole"))
	if !strings.Contains(core, "id > 0 AND (is_xpac IS FALSE AND quality_null_xpac IS FALSE)") {
		t.Fatal("core flag semantics changed")
	}
}

// Purely hand-counted graph: core IDs1/2, XPAC IDs3/4. ID5 is a
// blacklisted XPAC dataset; ID6 a blacklisted core editorial. ID1 is pre2000.
// Core induced edges:2->1. XPAC induced edges:3->4,4->3. Joint all additionally
// has core->XPAC2->3/4 and XPAC->core3->1/2,4->1, for8 edges, not1+2.
func crossCorpusFixture(t *testing.T, duckdb, corpus string) Config {
	t.Helper()
	root := t.TempDir()
	cfg := Config{ResultDir: filepath.Join(root, "result"), CacheDir: filepath.Join(root, "cache"), WorkDir: filepath.Join(root, "audit"), DuckDB: duckdb, Threads: 1, MemoryLimit: "128MB"}
	for _, dir := range []string{cfg.ResultDir, cfg.CacheDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	bio := "https://openalex.org/C86803240"
	field := "https://openalex.org/fields/17"
	rows := []normalizedFixture{
		{ID: 1, Year: 1995, Type: "article"},
		{ID: 2, Year: 2000, Type: "article", References: []uint64{1, 3, 4, 3, 2, 5}},
		{ID: 3, Year: 2001, Type: "article", XPAC: true, References: []uint64{1, 2, 4, 3}},
		{ID: 4, Year: 2002, Type: "article", XPAC: true, References: []uint64{3, 1}},
		{ID: 5, Year: 2000, Type: "dataset", XPAC: true, References: []uint64{1}},
		{ID: 6, Year: 2000, Type: "editorial", References: []uint64{3}},
	}
	for i := range rows {
		rows[i].Countries = []string{"CN", " CN "}
		rows[i].Field = field
		rows[i].Subfield = "s"
		rows[i].ConceptIDs0 = []string{bio, bio}
		rows[i].ConceptScores0 = []float64{.9, .2}
	}
	cachePath := filepath.Join(cfg.CacheDir, "part.parquet")
	writeParquet(t, cachePath, rows)
	hash, err := hashFile(context.Background(), cachePath)
	if err != nil {
		t.Fatal(err)
	}
	run := controlledRun()
	scope, err := policy.New(corpus, 0, auditedBlacklist)
	if err != nil {
		t.Fatal(err)
	}
	run.Config.Scope = scope
	run.SourceRows = 6
	run.Config.Input.ManifestWorks = 6
	run.Config.Input.Paths = []string{filepath.Join(root, "raw.parquet")}
	selected, pubs, edges, loops, future := uint64(2), uint64(1), uint64(1), uint64(1), uint64(0)
	yearTotals := map[int][2]float64{2000: {1, 1}}
	if corpus == "all" {
		selected, pubs, edges, loops, future = 4, 3, 8, 2, 3
		yearTotals = map[int][2]float64{2000: {1, 3}, 2001: {1, 3}, 2002: {1, 2}}
	}
	run.SelectedWorks = int(selected)
	run.Inventory = production.Inventory{Rows: 6, Selected: selected, Core: production.GroupCoverage{Works: 3}, XPAC: production.GroupCoverage{Works: 3}, TypeCounts: map[string][3]uint64{"article": {2, 2, 0}, "dataset": {0, 1, 0}, "editorial": {1, 0, 0}}}
	run.Graph = production.EdgeStats{Stats: graph.Stats{SourceRecords: 6, MemberSourceRecords: selected, ValidYearSourceRecords: pubs, RetainedEdges: edges, SelfLoops: loops}, FutureTargetEdges: future}
	run.Quality = aggregate.Quality{PublicationWorks: pubs, CitationEvents: edges}
	manifest := cache.Manifest{FormatVersion: cache.FormatVersion, Complete: true, Rows: 6, SourceManifest: run.Config.Input.ManifestSHA256, Parts: []cache.Part{{InputPath: run.Config.Input.Paths[0], Path: cachePath, Rows: 6, Bytes: hash.Bytes, SHA256: hash.SHA256}}}
	writeJSON(t, filepath.Join(cfg.CacheDir, "manifest.json"), manifest)
	cacheKey, err := cacheIdentity(manifest, run.Config.Input.Paths)
	if err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(cfg.ResultDir, "methodology.json"), map[string]any{"run_id": run.RunID, "normalized_cache_key": cacheKey})
	var cube []aggregate.Row
	for year, counts := range yearTotals {
		for _, layer := range []struct{ scheme, level, id string }{{"topics", "field", field}, {"topics", "subfield", "s"}, {"concepts", "0", bio}, {"concepts", "1", aggregate.Unknown}} {
			cube = append(cube, aggregate.Row{Country: "CN", Year: year, Scheme: layer.scheme, Level: layer.level, DisciplineID: layer.id, Publications: counts[0], Citations: counts[1]})
		}
	}
	writeParquet(t, filepath.Join(cfg.ResultDir, "cube.parquet"), cube)
	run.CubeRows = len(cube)
	scenarios := auditScenarios("whole")
	main, _ := mainAuditScenario(scenarios)
	run.Config.Weighting = main.Config()
	var statistics []results.Row
	// Every eligible Work has one deduplicated country and one qualifying label,
	// so both country modes and all thresholds have these same hand-written cells.
	for _, s := range scenarios {
		for _, r := range cube {
			statistics = append(statistics, results.Row{ScenarioID: s.ID, CountryMode: s.CountryMode, ConceptMode: s.ConceptMode, ConceptThreshold: s.ConceptThreshold, Main: s.Main, Country: r.Country, Year: r.Year, Scheme: r.Scheme, Level: r.Level, DisciplineID: r.DisciplineID, Publications: r.Publications, Citations: r.Citations})
		}
	}
	writeParquet(t, filepath.Join(cfg.ResultDir, "statistics.parquet"), statistics)
	run.StatisticsRows = len(statistics)
	writeJSON(t, filepath.Join(cfg.ResultDir, "scenarios.json"), scenarios)
	writeOracleModelFixtures(t, cfg.ResultDir, cube)
	writeJSON(t, filepath.Join(cfg.ResultDir, "run.json"), run)
	completion(t, cfg.ResultDir)
	return cfg
}
func TestDuckDBJointAllCorpusCrossXPACEdges(t *testing.T) {
	duckdb := auditDuckDB(t)
	for _, corpus := range []string{"core", "all"} {
		t.Run(corpus, func(t *testing.T) {
			cfg := crossCorpusFixture(t, duckdb, corpus)
			report, err := Run(context.Background(), cfg, nil)
			if err != nil {
				log, _ := os.ReadFile(filepath.Join(cfg.WorkDir, "duckdb.stderr.log"))
				t.Fatalf("%s oracle:%v\n%s", corpus, err, log)
			}
			if !report.Passed {
				t.Fatal("audit not passed")
			}
			if report.SelectedByCorpus["core"] != 2 || report.SelectedByCorpus["unknown"] != 0 {
				t.Fatal("wrong selected core/unknown inventory", report.SelectedByCorpus)
			}
			xpac := uint64(0)
			if corpus == "all" {
				xpac = 2
			}
			if report.SelectedByCorpus["xpac"] != xpac || report.SelectedTypeCounts["article"] != [3]uint64{2, xpac, 0} {
				t.Fatal("selected type/corpus inventory not tied to Q", report.SelectedByCorpus, report.SelectedTypeCounts)
			}
			annual, err := readAnnual(filepath.Join(cfg.WorkDir, "citation_year.json"), "edges")
			if err != nil {
				t.Fatal(err)
			}
			want := map[int]uint64{2000: 1}
			if corpus == "all" {
				want = map[int]uint64{2000: 3, 2001: 3, 2002: 2}
			}
			for year, count := range want {
				if annual[year].Count != count {
					t.Fatalf("%s year%d edges%d want%d", corpus, year, annual[year].Count, count)
				}
			}
			var total uint64
			for _, row := range annual {
				total += row.Count
			}
			expected := uint64(1)
			if corpus == "all" {
				expected = 8
				if total == 1+2 {
					t.Fatal("joint graph reduced to sum of separate core and XPAC graphs")
				}
			}
			if total != expected {
				t.Fatal(total, expected)
			}
		})
	}
}
