package results

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
	"openalexstats/internal/eci"
)

const MainScenarioID = "fractional-concept-whole-0.3"

// LegacyMainScenarioID identifies the old within-level concept allocation.
const LegacyMainScenarioID = "fractional-concept-0.3"

type ModelInput struct {
	Year       int        `json:"year"`
	Scheme     string     `json:"scheme"`
	Level      string     `json:"level"`
	Metric     string     `json:"metric"`
	ScenarioID string     `json:"scenario_id,omitempty"`
	Result     eci.Result `json:"result"`
}
type ScoreRow struct {
	ScenarioID  string  `json:"scenario_id" parquet:"scenario_id"`
	Year        int     `json:"year" parquet:"year"`
	Scheme      string  `json:"scheme" parquet:"scheme"`
	Level       string  `json:"level" parquet:"level"`
	Metric      string  `json:"metric" parquet:"metric"`
	ID          string  `json:"id" parquet:"id"`
	Score       float64 `json:"score" parquet:"score"`
	Valid       bool    `json:"valid" parquet:"valid"`
	Active      bool    `json:"active" parquet:"active"`
	ModelStatus string  `json:"model_status" parquet:"model_status"`
	Unstable    bool    `json:"unstable" parquet:"unstable"`
}
type RCARow struct {
	ScenarioID   string  `json:"scenario_id" parquet:"scenario_id"`
	Year         int     `json:"year" parquet:"year"`
	Scheme       string  `json:"scheme" parquet:"scheme"`
	Level        string  `json:"level" parquet:"level"`
	Metric       string  `json:"metric" parquet:"metric"`
	Country      string  `json:"country" parquet:"country"`
	DisciplineID string  `json:"discipline_id" parquet:"discipline_id"`
	RCA          float64 `json:"rca" parquet:"rca"`
	Valid        bool    `json:"valid" parquet:"valid"`
}
type Diagnostic struct {
	Year                      int             `json:"year"`
	Scheme                    string          `json:"scheme"`
	Level                     string          `json:"level"`
	Metric                    string          `json:"metric"`
	ScenarioID                string          `json:"scenario_id"`
	Status                    string          `json:"status"`
	ActiveCountries           int             `json:"active_countries"`
	ActiveDisciplines         int             `json:"active_disciplines"`
	Components                []eci.Component `json:"components"`
	ComponentCount            int             `json:"component_count"`
	LargestComponentCountries int             `json:"largest_component_countries"`
	Eigenvalues               []float64       `json:"eigenvalues"`
	EigenGap                  float64         `json:"eigen_gap"`
	Unstable                  bool            `json:"unstable"`
	Residual                  float64         `json:"residual"`
	Message                   string          `json:"message"`
}

// FlattenModels includes all fixed dictionary labels, with explicit validity
// flags for inactive/undefined scores. Zero in an invalid row is not a score.
func FlattenModels(models []ModelInput) ([]ScoreRow, []ScoreRow, []Diagnostic, error) {
	var countries, disciplines []ScoreRow
	var diagnostics []Diagnostic
	for _, m := range models {
		if m.Year < 1 || !validLayer(m.Scheme, m.Level) || (m.Metric != "publications" && m.Metric != "citations") {
			return nil, nil, nil, fmt.Errorf("invalid model selection")
		}
		if m.ScenarioID == "" {
			m.ScenarioID = MainScenarioID
		}
		r := m.Result
		ci, di := map[int]int{}, map[int]int{}
		for i, c := range r.ActiveCountries {
			if c < 0 || c >= len(r.Countries) {
				return nil, nil, nil, fmt.Errorf("invalid active country index")
			}
			if _, ok := ci[c]; ok {
				return nil, nil, nil, fmt.Errorf("duplicate active country index")
			}
			ci[c] = i
		}
		for i, d := range r.ActiveDisciplines {
			if d < 0 || d >= len(r.Disciplines) {
				return nil, nil, nil, fmt.Errorf("invalid active discipline index")
			}
			if _, ok := di[d]; ok {
				return nil, nil, nil, fmt.Errorf("duplicate active discipline index")
			}
			di[d] = i
		}
		if r.Status == "ok" && (len(r.ECI) != len(ci) || len(r.PCI) != len(di)) {
			return nil, nil, nil, fmt.Errorf("score/index dimensions mismatch")
		}
		base := ScoreRow{ScenarioID: m.ScenarioID, Year: m.Year, Scheme: m.Scheme, Level: m.Level, Metric: m.Metric, ModelStatus: r.Status, Unstable: r.Unstable}
		for c, id := range r.Countries {
			row := base
			row.ID = id
			if i, ok := ci[c]; ok {
				row.Active = true
				if r.Status == "ok" {
					row.Score = r.ECI[i]
					row.Valid = true
				}
			}
			countries = append(countries, row)
		}
		for d, id := range r.Disciplines {
			row := base
			row.ID = id
			if i, ok := di[d]; ok {
				row.Active = true
				if r.Status == "ok" {
					row.Score = r.PCI[i]
					row.Valid = true
				}
			}
			disciplines = append(disciplines, row)
		}
		diagnostics = append(diagnostics, Diagnostic{m.Year, m.Scheme, m.Level, m.Metric, m.ScenarioID, r.Status, len(ci), len(di), r.Components, r.ComponentCount, r.LargestComponentCountries, r.Eigenvalues, r.EigenGap, r.Unstable, r.Residual, r.Diagnostic})
	}
	return countries, disciplines, diagnostics, nil
}

// Export preserves model values and adds an explicit owning scenario_id to every
// eci.json record. models can be pipeline.ModelResult or ModelInput; JSON
// adaptation avoids a package cycle and preserves unrelated model fields.
func Export(dir string, rows []Row, coverage []Coverage, scenarios []Scenario, models any, dictionary Dictionary) error {
	resolved, err := ResolveScenarios(scenarios)
	if err != nil {
		return err
	}
	scenarios = resolved
	mainID := ""
	ids := make(map[string]bool, len(scenarios))
	for _, s := range scenarios {
		ids[s.ID] = true
		if s.ForECI() {
			mainID = s.ID
		}
	}
	for _, r := range rows {
		if !ids[r.ScenarioID] {
			return fmt.Errorf("statistics row references unconfigured scenario %q", r.ScenarioID)
		}
	}
	for _, c := range coverage {
		if !ids[c.ScenarioID] {
			return fmt.Errorf("coverage references unconfigured scenario %q", c.ScenarioID)
		}
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	encoded, err := json.Marshal(models)
	if err != nil {
		return err
	}
	var modelInputs []ModelInput
	if err = json.Unmarshal(encoded, &modelInputs); err != nil {
		return fmt.Errorf("models must be an array: %w", err)
	}
	var modelRecords []map[string]json.RawMessage
	if err = json.Unmarshal(encoded, &modelRecords); err != nil {
		return fmt.Errorf("models must be an array: %w", err)
	}
	mainJSON, _ := json.Marshal(mainID)
	for i := range modelInputs {
		if modelInputs[i].ScenarioID != "" && modelInputs[i].ScenarioID != mainID {
			return fmt.Errorf("model scenario %q does not match unique ECI owner %q", modelInputs[i].ScenarioID, mainID)
		}
		modelInputs[i].ScenarioID = mainID
		if modelRecords[i] == nil {
			return fmt.Errorf("null model record")
		}
		modelRecords[i]["scenario_id"] = mainJSON
	}
	countryScores, disciplineScores, diagnostics, err := FlattenModels(modelInputs)
	if err != nil {
		return err
	}
	if err = writeParquet(filepath.Join(dir, "statistics.parquet"), rows); err != nil {
		return err
	}
	if err = writeParquet(filepath.Join(dir, "country_eci.parquet"), countryScores); err != nil {
		return err
	}
	if err = writeParquet(filepath.Join(dir, "discipline_pci.parquet"), disciplineScores); err != nil {
		return err
	}
	// Stream two RCA rows per statistics row instead of doubling the in-memory cube.
	if err = writeRCAs(filepath.Join(dir, "rca.parquet"), rows); err != nil {
		return err
	}
	for _, file := range []struct {
		name  string
		value any
	}{{"coverage.json", coverage}, {"scenarios.json", scenarios}, {"dictionary.json", dictionary}, {"eci.json", modelRecords}, {"model_diagnostics.json", diagnostics}} {
		if err = writeJSON(filepath.Join(dir, file.name), file.value); err != nil {
			return err
		}
	}
	return nil
}
func writeRCAs(path string, rows []Row) error {
	return parquetFile(path, func(w *parquet.GenericWriter[RCARow]) error {
		batch := make([]RCARow, 0, 4096)
		for _, r := range rows {
			for _, metric := range []string{"publications", "citations"} {
				v, ok := r.PublicationRCA, r.PublicationRCAValid
				if metric == "citations" {
					v, ok = r.CitationRCA, r.CitationRCAValid
				}
				batch = append(batch, RCARow{r.ScenarioID, r.Year, r.Scheme, r.Level, metric, r.Country, r.DisciplineID, v, ok})
				if len(batch) == cap(batch) {
					if _, err := w.Write(batch); err != nil {
						return err
					}
					batch = batch[:0]
				}
			}
		}
		if len(batch) > 0 {
			_, err := w.Write(batch)
			return err
		}
		return nil
	})
}
func writeParquet[T any](path string, rows []T) error {
	return parquetFile(path, func(w *parquet.GenericWriter[T]) error {
		for start := 0; start < len(rows); start += 4096 {
			if _, err := w.Write(rows[start:min(start+4096, len(rows))]); err != nil {
				return err
			}
		}
		return nil
	})
}
func parquetFile[T any](path string, write func(*parquet.GenericWriter[T]) error) error {
	f, err := os.Create(path + ".tmp")
	if err != nil {
		return err
	}
	w := parquet.NewGenericWriter[T](f, parquet.Compression(&zstd.Codec{}))
	err = write(w)
	closeErr := w.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = f.Sync()
	}
	fileErr := f.Close()
	if err != nil {
		return err
	}
	if fileErr != nil {
		return fileErr
	}
	return os.Rename(path+".tmp", path)
}
func writeJSON(path string, value any) error {
	f, err := os.Create(path + ".tmp")
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(value)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(path+".tmp", path)
}
