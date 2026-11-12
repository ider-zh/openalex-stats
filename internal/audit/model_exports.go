package audit

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/parquet-go/parquet-go"
)

type ExportCheck struct {
	File           string   `json:"file"`
	Passed         bool     `json:"passed"`
	ExpectedRows   int      `json:"expected_rows"`
	Rows           int      `json:"rows"`
	MissingRows    int      `json:"missing_rows"`
	UnexpectedRows int      `json:"unexpected_rows"`
	DuplicateRows  int      `json:"duplicate_rows"`
	Mismatches     int      `json:"semantic_mismatches"`
	MaxScoreError  float64  `json:"max_score_error"`
	Examples       []string `json:"examples,omitempty"`
}
type exportedScore struct {
	ScenarioID  string  `parquet:"scenario_id"`
	Year        int     `parquet:"year"`
	Scheme      string  `parquet:"scheme"`
	Level       string  `parquet:"level"`
	Metric      string  `parquet:"metric"`
	ID          string  `parquet:"id"`
	Score       float64 `parquet:"score"`
	Valid       bool    `parquet:"valid"`
	Active      bool    `parquet:"active"`
	ModelStatus string  `parquet:"model_status"`
	Unstable    bool    `parquet:"unstable"`
}

func scoreKey(row exportedScore) string {
	return modelID(row.Year, row.Scheme, row.Level, row.Metric) + "/" + row.ID
}

func auditScoreExports(ctx context.Context, root string, models []publishedModel, checks []ModelCheck) ([]ExportCheck, error) {
	byModel := map[string]int{}
	for i, m := range models {
		byModel[modelID(m.Year, m.Scheme, m.Level, m.Metric)] = i
	}
	var exports []ExportCheck
	for _, kind := range []string{"country", "discipline"} {
		name := "country_eci.parquet"
		if kind == "discipline" {
			name = "discipline_pci.parquet"
		}
		expected := map[string]exportedScore{}
		rowsByModel := map[string]int{}
		wantByModel := map[string]int{}
		badByModel := map[string]int{}
		for _, m := range models {
			r := m.Result
			ids, active, scores := r.Countries, r.ActiveCountries, r.ECI
			if kind == "discipline" {
				ids, active, scores = r.Disciplines, r.ActiveDisciplines, r.PCI
			}
			index := map[int]int{}
			for i, ordinal := range active {
				index[ordinal] = i
			}
			for ordinal, id := range ids {
				row := exportedScore{ScenarioID: modelScenarioID(m), Year: m.Year, Scheme: m.Scheme, Level: m.Level, Metric: m.Metric, ID: id, ModelStatus: r.Status, Unstable: r.Unstable}
				if i, ok := index[ordinal]; ok {
					row.Active = true
					if r.Status == "ok" && i < len(scores) {
						row.Valid = true
						row.Score = scores[i]
					}
				}
				expected[scoreKey(row)] = row
				wantByModel[modelID(m.Year, m.Scheme, m.Level, m.Metric)]++
			}
		}
		out := ExportCheck{File: name, ExpectedRows: len(expected)}
		seen := map[string]bool{}
		addExample := func(message string) {
			if len(out.Examples) < 20 {
				out.Examples = append(out.Examples, message)
			}
		}
		err := readScoreRows(ctx, filepath.Join(root, name), func(row exportedScore) error {
			out.Rows++
			key := scoreKey(row)
			mk := modelID(row.Year, row.Scheme, row.Level, row.Metric)
			want, ok := expected[key]
			if !ok {
				out.UnexpectedRows++
				addExample("unexpected " + key)
				return nil
			}
			if seen[key] {
				out.DuplicateRows++
				badByModel[mk]++
				addExample("duplicate " + key)
				return nil
			}
			seen[key] = true
			rowsByModel[mk]++
			diff := math.Abs(row.Score - want.Score)
			if math.IsNaN(diff) || math.IsInf(diff, 0) {
				diff = math.MaxFloat64
			}
			out.MaxScoreError = math.Max(out.MaxScoreError, diff)
			mismatch := row.ScenarioID != want.ScenarioID || row.Active != want.Active || row.Valid != want.Valid || row.ModelStatus != want.ModelStatus || row.Unstable != want.Unstable || !finiteModelValue(math.Abs(row.Score)) || diff > 1e-10
			if mismatch {
				out.Mismatches++
				badByModel[mk]++
				addExample("mismatch " + key)
			}
			return nil
		})
		if err != nil {
			return exports, err
		}
		for key, want := range expected {
			if !seen[key] {
				out.MissingRows++
				badByModel[modelID(want.Year, want.Scheme, want.Level, want.Metric)]++
				addExample("missing " + key)
			}
		}
		out.Passed = out.Rows == out.ExpectedRows && out.MissingRows == 0 && out.UnexpectedRows == 0 && out.DuplicateRows == 0 && out.Mismatches == 0
		for mk, i := range byModel {
			checks[i].numeric("export_"+kind+"_row_count", float64(wantByModel[mk]), float64(rowsByModel[mk]), 0)
			checks[i].numeric("export_"+kind+"_semantic_errors", 0, float64(badByModel[mk]), 0)
			checks[i].finish()
		}
		exports = append(exports, out)
	}
	return exports, nil
}
func readScoreRows(ctx context.Context, path string, yield func(exportedScore) error) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	r := parquet.NewGenericReader[exportedScore](f)
	defer r.Close()
	batch := make([]exportedScore, 2048)
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		n, e := r.Read(batch)
		for _, row := range batch[:n] {
			if err := yield(row); err != nil {
				return err
			}
		}
		clear(batch)
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return fmt.Errorf("read score export: %w", e)
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
}

func modelScenarioID(m publishedModel) string {
	if m.ScenarioID == "" {
		return legacyMainScenario
	}
	return m.ScenarioID
}
