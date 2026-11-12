package audit

import (
	"context"
	"math"
	"path/filepath"
	"testing"

	"github.com/parquet-go/parquet-go"

	"openalexstats/internal/aggregate"
	"openalexstats/internal/eci"
)

// The non-symmetric source fixture is independently hand-derived:
// M=[[1,0,0],[1,1,0],[0,1,1]], degrees=(1,2,2),
// T=[[1/2,1/2,0],[1/4,1/2,1/4],[0,1/4,3/4]],
// spectrum={1,(3+sqrt(5))/8,(3-sqrt(5))/8}.
// Its population-standardized right eigenvector and PCI are closed forms.
func handModelFixture() (publishedModel, auditMatrix) {
	s := math.Sqrt(5)
	lambda := (3 + s) / 8
	matrix := auditMatrix{countries: []string{"A", "B", "C", "inactive"}, disciplines: []string{"x", "y", "z", "empty"}, values: []float64{10, 1, 1, 0, 5, 5, 1, 0, 1, 5, 10, 0, 0, 0, 0, 0}}
	r := eci.Result{Status: "ok", Countries: matrix.countries, Disciplines: matrix.disciplines, ActiveCountries: []int{0, 1, 2}, ActiveDisciplines: []int{0, 1, 2}, RCARoundingTolerance: 1e-12,
		RCA:       [][]float64{{65.0 / 32, 13.0 / 44, 13.0 / 48, 0}, {195.0 / 176, 195.0 / 121, 13.0 / 44, 0}, {39.0 / 256, 195.0 / 176, 65.0 / 32, 0}, {0, 0, 0, 0}},
		Advantage: [][]bool{{true, false, false, false}, {true, true, false, false}, {false, true, true, false}, {false, false, false, false}},
		ECI:       []float64{-s / 2, (s - 3) / 4, (3 + s) / 4}, PCI: []float64{-(3 + s) / 4, (3 - s) / 4, s / 2}, Eigenvalues: []float64{(3 - s) / 8, lambda, 1}, SecondEigenvalue: lambda, EigenGap: 1 - lambda,
		Components: []eci.Component{{Countries: []int{0, 1, 2}, Disciplines: []int{0, 1, 2}}}, ComponentCount: 1, LargestComponentCountries: 3}
	return publishedModel{Year: 2000, Scheme: "topics", Level: "field", Metric: "publications", Result: r}, matrix
}
func TestIndependentModelHandEigenpairAndMaliciousScores(t *testing.T) {
	model, matrix := handModelFixture()
	check, err := checkPublishedModel(model, matrix)
	if err != nil || !check.Passed {
		t.Fatalf("hand-derived eigenpair rejected: err=%v checks=%+v", err, check.Checks)
	}
	if math.Abs(check.SecondEigenvalue-(3+math.Sqrt(5))/8) > 1e-12 || check.OperatorResidual > 1e-12 || check.PCIMaxError > 1e-12 {
		t.Fatalf("bad independent numerics %+v", check)
	}
	changes := []struct {
		name   string
		change func(*eci.Result)
	}{
		{"all_zero_eci", func(r *eci.Result) { r.ECI = []float64{0, 0, 0} }},
		{"rescaled_eci", func(r *eci.Result) {
			r.ECI = append([]float64(nil), r.ECI...)
			for i := range r.ECI {
				r.ECI[i] *= 2
			}
		}},
		{"swapped_countries", func(r *eci.Result) { r.ECI = append([]float64(nil), r.ECI...); r.ECI[0], r.ECI[1] = r.ECI[1], r.ECI[0] }},
		{"pci_flip_without_eci", func(r *eci.Result) {
			r.PCI = append([]float64(nil), r.PCI...)
			for i := range r.PCI {
				r.PCI[i] *= -1
			}
		}},
		{"fake_lambda", func(r *eci.Result) { r.SecondEigenvalue = .1 }},
		{"wrong_advantage", func(r *eci.Result) {
			r.Advantage = append([][]bool(nil), r.Advantage...)
			r.Advantage[0] = append([]bool(nil), r.Advantage[0]...)
			r.Advantage[0][1] = true
		}},
		{"wrong_active_indices", func(r *eci.Result) { r.ActiveCountries = []int{1, 0, 2} }},
	}
	for _, tc := range changes {
		t.Run(tc.name, func(t *testing.T) {
			bad := model
			tc.change(&bad.Result)
			check, err := checkPublishedModel(bad, matrix)
			if err != nil {
				t.Fatal(err)
			}
			if check.Passed {
				t.Fatal("malicious model escaped independent audit")
			}
		})
	}
	// Simultaneously changing both score axes' signs is mathematically valid and
	// is required for cross-year sign alignment, not a numerical corruption.
	model, _ = handModelFixture()
	for i := range model.Result.ECI {
		model.Result.ECI[i] *= -1
	}
	for i := range model.Result.PCI {
		model.Result.PCI[i] *= -1
	}
	if check, err := checkPublishedModel(model, matrix); err != nil || !check.Passed {
		t.Fatal("joint sign alignment rejected", err, check.Checks)
	}
}

func TestIndependentBoundaryAndUndefinedModels(t *testing.T) {
	for _, values := range [][]float64{{1, 1, 1, 1}, {.1 * .2, .1 * .9, .7 * .2, .7 * .9}} {
		state, err := deriveIndependent(auditMatrix{[]string{"A", "B"}, []string{"x", "y"}, values})
		if err != nil || state.status != "degenerate" {
			t.Fatal("proportional portfolio should be degenerate", state.status, err)
		}
		for _, row := range state.rca {
			for _, value := range row {
				if value != 1 {
					t.Fatal("roundoff boundary not snapped", value)
				}
			}
		}
	}
	matrix := auditMatrix{[]string{"A", "B"}, []string{"x", "y"}, []float64{10, 0, 0, 10}}
	state, err := deriveIndependent(matrix)
	if err != nil || state.status != "disconnected" {
		t.Fatal("disconnected graph not identified", err)
	}
	r := publishedModel{Year: 2000, Scheme: "concepts", Level: "0", Metric: "citations", Result: resultFromIndependent(state, matrix)}
	if check, err := checkPublishedModel(r, matrix); err != nil || !check.Passed {
		t.Fatal("undefined model rejected", err, check.Checks)
	}
	r.Result.ECI = []float64{0, 0}
	if check, _ := checkPublishedModel(r, matrix); check.Passed {
		t.Fatal("invented undefined scores accepted")
	}
}

// Metadata for the SQL-oracle fixture contains no valid complexity scores: every
// slot is hand-known empty/disconnected/degenerate. This helper only assembles
// RCA/status metadata from tiny cube cells; it never calls a production solver.
func writeOracleModelFixtures(t *testing.T, dir string, rows []aggregate.Row) {
	t.Helper()
	countries := []string{"CN", "GB", "US"}
	labels := map[string][]string{"topics/field": {"https://openalex.org/fields/11", "https://openalex.org/fields/17"}, "topics/subfield": {"s"}, "concepts/0": {"https://openalex.org/C86803240", "CX"}, "concepts/1": {"https://openalex.org/C86803240", "LX", "unused"}}
	dictionary := auditDictionary{}
	for _, c := range countries {
		dictionary.Countries = append(dictionary.Countries, auditLabel{c})
	}
	for _, layer := range auditLayers {
		l := struct {
			Scheme string       `json:"scheme"`
			Level  string       `json:"level"`
			Labels []auditLabel `json:"labels"`
		}{Scheme: layer[0], Level: layer[1]}
		for _, id := range labels[layer[0]+"/"+layer[1]] {
			l.Labels = append(l.Labels, auditLabel{id})
		}
		dictionary.Layers = append(dictionary.Layers, l)
	}
	writeJSON(t, filepath.Join(dir, "dictionary.json"), dictionary)
	main, _, err := readMainAuditScenario(dir)
	if err != nil {
		t.Fatal(err)
	}
	var models []publishedModel
	for year := 2000; year <= 2026; year++ {
		for _, layer := range auditLayers {
			for _, metric := range []string{"publications", "citations"} {
				disciplines := labels[layer[0]+"/"+layer[1]]
				matrix := auditMatrix{countries, disciplines, make([]float64, len(countries)*len(disciplines))}
				for _, row := range rows {
					if row.Year != year || row.Scheme != layer[0] || row.Level != layer[1] || row.Country == aggregate.Unknown || row.DisciplineID == aggregate.Unknown {
						continue
					}
					for i, c := range countries {
						if c != row.Country {
							continue
						}
						for j, d := range disciplines {
							if d == row.DisciplineID {
								value := row.Publications
								if metric == "citations" {
									value = row.Citations
								}
								matrix.values[i*len(disciplines)+j] += value
							}
						}
					}
				}
				state, err := deriveIndependent(matrix)
				if err != nil {
					t.Fatal(err)
				}
				if state.status == "ok" {
					t.Fatal("SQL fixture unexpectedly requires a computed score")
				}
				models = append(models, publishedModel{Year: year, Scheme: layer[0], Level: layer[1], Metric: metric, ScenarioID: main.ID, Result: resultFromIndependent(state, matrix)})
			}
		}
	}
	writeJSON(t, filepath.Join(dir, "eci.json"), models)
	writeScoreFixtures(t, dir, models)
}
func writeScoreFixtures(t *testing.T, dir string, models []publishedModel) {
	t.Helper()
	for _, kind := range []string{"country", "discipline"} {
		name := "country_eci.parquet"
		var rows []exportedScore
		for _, m := range models {
			ids, active, scores := m.Result.Countries, m.Result.ActiveCountries, m.Result.ECI
			if kind == "discipline" {
				name = "discipline_pci.parquet"
				ids, active, scores = m.Result.Disciplines, m.Result.ActiveDisciplines, m.Result.PCI
			}
			for ordinal, id := range ids {
				row := exportedScore{ScenarioID: modelScenarioID(m), Year: m.Year, Scheme: m.Scheme, Level: m.Level, Metric: m.Metric, ID: id, ModelStatus: m.Result.Status, Unstable: m.Result.Unstable}
				for position, index := range active {
					if index == ordinal {
						row.Active = true
						if m.Result.Status == "ok" {
							row.Valid = true
							row.Score = scores[position]
						}
						break
					}
				}
				rows = append(rows, row)
			}
		}
		if err := parquet.WriteFile(filepath.Join(dir, name), rows); err != nil {
			t.Fatal(err)
		}
	}
}
func resultFromIndependent(s independentState, m auditMatrix) eci.Result {
	largest := 0
	for _, c := range s.components {
		largest = max(largest, len(c.Countries))
	}
	return eci.Result{Status: s.status, Countries: m.countries, Disciplines: m.disciplines, RCA: s.rca, Advantage: s.advantage, ActiveCountries: s.activeCountries, ActiveDisciplines: s.activeDisciplines, Eigenvalues: s.eigenvalues, SecondEigenvalue: s.lambda, EigenGap: s.gap, Unstable: len(s.eigenvalues) > 0 && s.gap < 1e-6, Components: s.components, ComponentCount: len(s.components), LargestComponentCountries: largest, RCARoundingTolerance: 1e-12}
}
func TestAll216ModelSlotsAndUnknownExclusion(t *testing.T) {
	cfg := oracleFixture(t, "unused")
	file := filepath.Join(cfg.WorkDir, "model-audit.json")
	report, err := AuditModels(context.Background(), cfg.ResultDir, file)
	if err != nil || !report.Passed || len(report.Models) != 216 || report.UnknownRowsExcluded == 0 {
		t.Fatalf("full model fixture failed: err=%v report=%+v", err, report)
	}
	if len(report.Exports) != 2 || !report.Exports[0].Passed || !report.Exports[1].Passed {
		t.Fatal("score export audit omitted", report.Exports)
	}
	scorePath := filepath.Join(cfg.ResultDir, "country_eci.parquet")
	scores, err := parquet.ReadFile[exportedScore](scorePath)
	if err != nil {
		t.Fatal(err)
	}
	scores[0].Score = .25
	if err := parquet.WriteFile(scorePath, scores); err != nil {
		t.Fatal(err)
	}
	bad, err := AuditModels(context.Background(), cfg.ResultDir, file)
	if err == nil || bad.Passed || bad.FailedExports == 0 {
		t.Fatal("invalid score export tampering escaped", err, bad.Exports)
	}
	scores[0].Score = 0
	if err := parquet.WriteFile(scorePath, scores); err != nil {
		t.Fatal(err)
	}
	var models []publishedModel
	if err := readJSON(filepath.Join(cfg.ResultDir, "eci.json"), &models); err != nil {
		t.Fatal(err)
	}
	models = models[:len(models)-1]
	writeJSON(t, filepath.Join(cfg.ResultDir, "eci.json"), models)
	report, err = AuditModels(context.Background(), cfg.ResultDir, file)
	if err == nil || report.Passed {
		t.Fatal("missing model slot passed")
	}
}

func TestWholeYearJointAxisFlipFailsPublishedOrientation(t *testing.T) {
	first, matrix := handModelFixture()
	second, _ := handModelFixture()
	second.Year = 2001
	models := []publishedModel{first, second}
	checks := make([]ModelCheck, 2)
	for i, m := range models {
		checks[i], _ = checkPublishedModel(m, matrix)
	}
	auditOrientations(models, checks)
	if !checks[0].Passed || !checks[1].Passed {
		t.Fatal("correct orientation rejected", checks)
	}
	for i := range second.Result.ECI {
		second.Result.ECI[i] *= -1
	}
	for i := range second.Result.PCI {
		second.Result.PCI[i] *= -1
	}
	models[1] = second
	for i, m := range models {
		checks[i], _ = checkPublishedModel(m, matrix)
		if !checks[i].Passed {
			t.Fatal("joint flip should preserve per-year equations")
		}
	}
	auditOrientations(models, checks)
	if checks[1].Passed || checks[1].TemporalCovariance >= 0 {
		t.Fatal("whole-year joint sign corruption escaped", checks[1])
	}
	models[0] = second
	models[0].Year = 2000
	checks[0], _ = checkPublishedModel(models[0], matrix)
	auditOrientations(models[:1], checks[:1])
	if checks[0].Passed {
		t.Fatal("first-year negative diversity orientation escaped")
	}
}

func TestOrientationTieAndExplicitFallback(t *testing.T) {
	model := publishedModel{Year: 2000, Scheme: "topics", Level: "field", Metric: "publications", Result: eci.Result{Status: "ok", Countries: []string{"A", "B"}, ActiveCountries: []int{0, 1}, ECI: []float64{1, -1}}}
	checks := []ModelCheck{{diversity: []float64{1, 1}}}
	auditOrientations([]publishedModel{model}, checks)
	if !checks[0].Passed || checks[0].OrientationFallback == "" {
		t.Fatal("tie did not report deterministic fallback", checks[0])
	}
	model.Result.ECI = []float64{-1, 1}
	checks = []ModelCheck{{diversity: []float64{1, 1}}}
	auditOrientations([]publishedModel{model}, checks)
	if checks[0].Passed {
		t.Fatal("negative label tie escaped")
	}
	model.Result.ECI = []float64{1, -1}
	model.Result.Countries = []string{"A", "B"}
	next := model
	next.Year = 2001
	next.Result.Countries = []string{"A", "C"}
	checks = []ModelCheck{{diversity: []float64{2, 1}}, {diversity: []float64{2, 1}}}
	auditOrientations([]publishedModel{model, next}, checks)
	if !checks[1].Passed || checks[1].CommonCountries != 1 || checks[1].OrientationRule != "fallback_diversity_or_label_tie" || checks[1].OrientationFallback == "" {
		t.Fatal("insufficient overlap falsely claimed temporal proof", checks[1])
	}
}

func TestValidScoreExportTamperingFails(t *testing.T) {
	model, matrix := handModelFixture()
	models := []publishedModel{model}
	check, _ := checkPublishedModel(model, matrix)
	checks := []ModelCheck{check}
	dir := t.TempDir()
	writeScoreFixtures(t, dir, models)
	exports, err := auditScoreExports(context.Background(), dir, models, checks)
	if err != nil || !exports[0].Passed || !exports[1].Passed {
		t.Fatal(err, exports)
	}
	path := filepath.Join(dir, "discipline_pci.parquet")
	rows, err := parquet.ReadFile[exportedScore](path)
	if err != nil {
		t.Fatal(err)
	}
	rows[0].Score += .2
	if err := parquet.WriteFile(path, rows); err != nil {
		t.Fatal(err)
	}
	exports, err = auditScoreExports(context.Background(), dir, models, checks)
	if err != nil || exports[1].Passed || exports[1].Mismatches != 1 || checks[0].Passed {
		t.Fatal("PCI export corruption escaped", err, exports, checks[0])
	}
}
