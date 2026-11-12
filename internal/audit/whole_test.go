package audit

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/results"
)

func auditDuckDB(t *testing.T) string {
	t.Helper()
	if value := os.Getenv("OA_DUCKDB"); value != "" {
		return value
	}
	p, err := exec.LookPath("duckdb")
	if err != nil {
		t.Skip("DuckDB CLI unavailable; OA_DUCKDB enables SQL integration")
	}
	return p
}
func TestBothHistoricalAndWholeScenarioInventories(t *testing.T) {
	for _, mode := range []string{"fractional", "whole"} {
		scenarios := auditScenarios(mode)
		if err := supported(controlledRun(), scenarios); err != nil {
			t.Fatal(mode, err)
		}
		main, err := mainAuditScenario(scenarios)
		if err != nil || main.ConceptMode != mode {
			t.Fatal(main, err)
		}
		scenarios[2] = scenarios[1]
		if err := supported(controlledRun(), scenarios); err == nil {
			t.Fatal("duplicate accepted")
		}
	}
	mixed := auditScenarios("whole")
	mixed[0] = auditScenarios("fractional")[0]
	if err := supported(controlledRun(), mixed); err == nil {
		t.Fatal("mixed inventory accepted by controlled full oracle")
	}
}
func TestIndependentSQLWholeConceptAssignmentContract(t *testing.T) {
	cfg := Config{ResultDir: "results", WorkDir: "audit", Threads: 2, MemoryLimit: "128MB"}
	whole := buildSQL(cfg, []string{"cache.parquet"}, auditedBlacklist, auditScenarios("whole"))
	for _, needle := range []string{"THEN 1.0 ELSE 0.0 END AS biology_weight", "greatest(1,len(countries))", "greatest(1,len(concepts0_t3))", "greatest(1,len(concepts1_t5))", "sum(t.country_assignments * t.concepts0_t3_assignments)", "invalid_whole_integers", "fractional-concept-whole-0.3"} {
		if !strings.Contains(whole, needle) {
			t.Errorf("whole SQL missing %q", needle)
		}
	}
	if strings.Contains(whole, "1.0/len(concepts0_t3)") {
		t.Fatal("whole Concepts still divided by labels")
	}
	legacy := buildSQL(cfg, []string{"cache.parquet"}, auditedBlacklist, auditScenarios("fractional"))
	if !strings.Contains(legacy, "1.0/len(concepts0_t3)") {
		t.Fatal("legacy fractional lost")
	}
	if strings.Count(whole, "JOIN q t ON") != 1 {
		t.Fatal("reference graph independently joined more than once")
	}
}
func TestDynamicMainOwnerAndExportOwner(t *testing.T) {
	cfg := oracleFixtureMode(t, "unused", "whole")
	report, err := AuditModels(context.Background(), cfg.ResultDir, "")
	if err != nil || !report.Passed || report.ScenarioID != "fractional-concept-whole-0.3" {
		t.Fatal(err, report.ScenarioID)
	}
	var models []publishedModel
	if err := readJSON(filepath.Join(cfg.ResultDir, "eci.json"), &models); err != nil {
		t.Fatal(err)
	}
	models[0].ScenarioID = legacyMainScenario
	writeJSON(t, filepath.Join(cfg.ResultDir, "eci.json"), models)
	if r, e := AuditModels(context.Background(), cfg.ResultDir, ""); e == nil || r.Passed {
		t.Fatal("wrong historical model owner accepted")
	}
	models[0].ScenarioID = report.ScenarioID
	writeJSON(t, filepath.Join(cfg.ResultDir, "eci.json"), models)
	path := filepath.Join(cfg.ResultDir, "discipline_pci.parquet")
	scores, e := parquet.ReadFile[exportedScore](path)
	if e != nil {
		t.Fatal(e)
	}
	scores[0].ScenarioID = legacyMainScenario
	writeParquet(t, path, scores)
	if r, e := AuditModels(context.Background(), cfg.ResultDir, ""); e == nil || r.Passed || r.FailedExports == 0 {
		t.Fatal("wrong export owner accepted")
	}
}
func TestDuckDBWholeConceptAssignmentsHandGraph(t *testing.T) {
	cfg := oracleFixtureMode(t, auditDuckDB(t), "whole")
	report, err := Run(context.Background(), cfg, nil)
	if err != nil {
		log, _ := os.ReadFile(filepath.Join(cfg.WorkDir, "duckdb.stderr.log"))
		t.Fatalf("whole oracle: %v\n%s", err, log)
	}
	if !report.Passed {
		t.Fatal("whole audit failed")
	}
	citations, err := readAnnual(filepath.Join(cfg.WorkDir, "citation_year.json"), "edges")
	if err != nil {
		t.Fatal(err)
	}
	// Year2000: 2->1 (CN+US, two L0, two L1 at0/one at.3),
	// 2->6 (GB, one L0, UNKNOWN L1). Repeated CN/Concept IDs count once.
	a := citations[2000]
	for key, want := range map[string]uint64{"country_assignments": 3, "concepts0_t3_assignments": 3, "country_concepts0_t3_assignments": 5, "concepts1_t0_assignments": 3, "country_concepts1_t0_assignments": 5, "concepts1_t3_assignments": 2, "country_concepts1_t3_assignments": 3} {
		if a.Assignments[key] != want {
			t.Errorf("%s=%d want%d", key, a.Assignments[key], want)
		}
	}
	var outputs []results.Row
	path := filepath.Join(cfg.ResultDir, "statistics.parquet")
	outputs, err = parquet.ReadFile[results.Row](path)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	changedIndex := -1
	for i := range outputs {
		r := &outputs[i]
		if r.CountryMode == "whole" && r.ConceptMode == "whole" && r.Scheme == "concepts" && r.Level == "0" && r.Year == 2000 && r.Citations > 0 {
			r.Citations += .25
			changedIndex = i
			found = true
			break
		}
	}
	if !found {
		t.Fatal("no whole cell")
	}
	writeParquet(t, path, outputs)
	completion(t, cfg.ResultDir)
	report, err = Run(context.Background(), cfg, nil)
	integerFailed := false
	for _, check := range report.Checks {
		if strings.Contains(check.Name, "all_countrywhole_conceptwhole") && !check.Passed {
			integerFailed = true
		}
	}
	if err == nil || report.Passed || !integerFailed {
		t.Fatal("fractional contamination escaped exact whole-cell check", err)
	}
	outputs[changedIndex].Citations -= .25
	changedIndex = -1
	for i := range outputs {
		if outputs[i].Scheme == "concepts" && outputs[i].Level == "1" && outputs[i].DisciplineID != "UNKNOWN" {
			changedIndex = i
			break
		}
	}
	if changedIndex < 0 {
		t.Fatal("no level1 label to tamper")
	}
	outputs[changedIndex].DisciplineID = "outside-fixed-taxonomy"
	writeParquet(t, path, outputs)
	completion(t, cfg.ResultDir)
	report, err = Run(context.Background(), cfg, nil)
	dictionaryFailed := false
	for _, check := range report.Checks {
		if strings.Contains(check.Name, "all_four_layers_labels_match_fixed_dictionary") && !check.Passed {
			dictionaryFailed = true
		}
	}
	if err == nil || report.Passed || !dictionaryFailed {
		t.Fatal("nonrepresentative level1 unknown dictionary label escaped", err)
	}
}
