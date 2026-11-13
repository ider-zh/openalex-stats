package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/complexityfilter"
	"openalexstats/internal/pipeline"
	"openalexstats/internal/results"
)

type annualInput struct {
	Year   int
	Scheme string
	Level  string
	Metric string
	Input  complexityfilter.Input
}

type sourceIdentity struct {
	RunID                  string                `json:"run_id"`
	CompletionSHA256       string                `json:"completion_sha256"`
	MethodCompletionSHA256 string                `json:"method_completion_sha256"`
	StatisticsSHA256       string                `json:"statistics_sha256"`
	DictionarySHA256       string                `json:"dictionary_sha256"`
	ScenariosSHA256        string                `json:"scenarios_sha256"`
	SpectralModelsSHA256   string                `json:"spectral_models_sha256"`
	ReflectionModelsSHA256 string                `json:"reflection_models_sha256"`
	Inputs                 []checkpoint.Artifact `json:"inputs"`
}

type sourceData struct {
	Identity      sourceIdentity
	Inputs        []annualInput
	Baseline      map[string][]pipeline.ModelResult
	NetworkHashes []string
}

type sourceRow struct {
	Scenario     string  `parquet:"scenario_id"`
	Country      string  `parquet:"country"`
	Year         int     `parquet:"year"`
	Scheme       string  `parquet:"scheme"`
	Level        string  `parquet:"level"`
	Discipline   string  `parquet:"discipline_id"`
	Publications float64 `parquet:"publications"`
	Citations    float64 `parquet:"citations"`
}

type inputKey struct {
	Year          int
	Scheme, Level string
}

type group struct {
	publications, citations int
	labels                  map[string]int
	seen                    []uint64
}

func modelKey(year int, scheme, level, metric string) string {
	return fmt.Sprintf("%d/%s/%s/%s", year, scheme, level, metric)
}

func readJSON(path string, value any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, value)
}

// loadSource reads counts once, using the published fixed dictionary order.
// Whole-country support includes UNKNOWN subject via primary Field, while X
// excludes UNKNOWN country and subject exactly as the production model does.
func loadSource(ctx context.Context, root, methodRoot, corpus string) (sourceData, error) {
	var out sourceData
	var completion struct {
		RunID     string                `json:"run_id"`
		Run       checkpoint.Artifact   `json:"run"`
		Artifacts []checkpoint.Artifact `json:"artifacts"`
	}
	var run struct {
		RunID  string `json:"run_id"`
		Status string `json:"status"`
		Config struct {
			YearStart int `json:"year_start"`
			YearEnd   int `json:"year_end"`
			Scope     struct {
				Corpus string `json:"corpus"`
			} `json:"scope"`
		} `json:"config"`
	}
	if err := readJSON(filepath.Join(root, "completion.json"), &completion); err != nil {
		return out, err
	}
	if err := readJSON(filepath.Join(root, "run.json"), &run); err != nil {
		return out, err
	}
	if run.RunID == "" || run.RunID != completion.RunID || run.Status != "full_snapshot_complete" || run.Config.Scope.Corpus != corpus || run.Config.YearStart < 1 || run.Config.YearEnd < run.Config.YearStart {
		return out, fmt.Errorf("source must be completed matching-corpus snapshot")
	}
	out.Identity.RunID = run.RunID
	owned := map[string]checkpoint.Artifact{}
	for _, a := range append(completion.Artifacts, completion.Run) {
		name := filepath.Base(a.Path)
		if _, exists := owned[name]; exists {
			return out, fmt.Errorf("ambiguous source owner %s", name)
		}
		owned[name] = a
	}
	for _, name := range []string{"completion.json", "run.json", "statistics.parquet", "dictionary.json", "scenarios.json", "eci.json"} {
		a, err := checkpoint.Hash(filepath.Join(root, name))
		if err != nil {
			return out, err
		}
		if name != "completion.json" {
			expected, ok := owned[name]
			if !ok || expected.Bytes != a.Bytes || expected.SHA256 != a.SHA256 {
				return out, fmt.Errorf("source artifact mismatch %s", name)
			}
		}
		a.Path = "base/" + name
		out.Identity.Inputs = append(out.Identity.Inputs, a)
		switch name {
		case "completion.json":
			out.Identity.CompletionSHA256 = a.SHA256
		case "statistics.parquet":
			out.Identity.StatisticsSHA256 = a.SHA256
		case "dictionary.json":
			out.Identity.DictionarySHA256 = a.SHA256
		case "scenarios.json":
			out.Identity.ScenariosSHA256 = a.SHA256
		case "eci.json":
			out.Identity.SpectralModelsSHA256 = a.SHA256
		}
	}
	var dictionary results.Dictionary
	var scenarios []results.Scenario
	if err := readJSON(filepath.Join(root, "dictionary.json"), &dictionary); err != nil {
		return out, err
	}
	if err := readJSON(filepath.Join(root, "scenarios.json"), &scenarios); err != nil {
		return out, err
	}
	mainID, wholeID := "", ""
	for _, s := range scenarios {
		if s.ForECI() {
			if mainID != "" || s.ID != results.MainScenarioID {
				return out, fmt.Errorf("expected one hit-count main complexity scenario")
			}
			mainID = s.ID
		}
		if s.CountryMode == "whole" && s.ConceptMode == "whole" && s.ConceptThreshold == .3 {
			if wholeID != "" {
				return out, fmt.Errorf("ambiguous whole-country support scenario")
			}
			wholeID = s.ID
		}
	}
	if mainID == "" || wholeID == "" {
		return out, fmt.Errorf("source lacks main or whole-country support scenario")
	}
	layers := [][2]string{{"topics", "field"}, {"topics", "subfield"}, {"concepts", "0"}, {"concepts", "1"}}
	groups := map[inputKey]*group{}
	wholeP, wholeC := map[int][]uint64{}, map[int][]uint64{}
	countryIndex := map[string]int{}
	for year := run.Config.YearStart; year <= run.Config.YearEnd; year++ {
		for _, layer := range layers {
			g := &group{labels: map[string]int{}}
			for _, metric := range []string{"publications", "citations"} {
				m, err := dictionary.Matrix(nil, layer[0], layer[1], year, metric)
				if err != nil {
					return out, err
				}
				if wholeP[year] == nil {
					wholeP[year], wholeC[year] = make([]uint64, len(m.Countries)), make([]uint64, len(m.Countries))
				}
				if len(countryIndex) == 0 {
					for i, code := range m.Countries {
						countryIndex[code] = i
					}
				}
				index := len(out.Inputs)
				out.Inputs = append(out.Inputs, annualInput{year, layer[0], layer[1], metric, complexityfilter.Input{Matrix: m, Metric: metric, WholePublications: wholeP[year], WholeCitations: wholeC[year]}})
				if metric == "publications" {
					g.publications = index
					g.seen = make([]uint64, (len(m.Countries)*len(m.Disciplines)+63)/64)
					for j, id := range m.Disciplines {
						g.labels[id] = j
					}
				} else {
					g.citations = index
				}
			}
			groups[inputKey{year, layer[0], layer[1]}] = g
		}
	}
	f, err := os.Open(filepath.Join(root, "statistics.parquet"))
	if err != nil {
		return out, err
	}
	defer f.Close()
	reader := parquet.NewGenericReader[sourceRow](f)
	defer reader.Close()
	batch := make([]sourceRow, 8192)
	for {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		n, err := reader.Read(batch)
		for _, row := range batch[:n] {
			if row.Country == "UNKNOWN" || (row.Scenario != mainID && row.Scenario != wholeID) {
				continue
			}
			i, ok := countryIndex[row.Country]
			if !ok || wholeP[row.Year] == nil {
				return out, fmt.Errorf("count outside fixed source dictionary or years")
			}
			if row.Scenario == wholeID && row.Scheme == "topics" && row.Level == "field" {
				p, e := wholeInteger(row.Publications)
				if e != nil {
					return out, e
				}
				c, e := wholeInteger(row.Citations)
				if e != nil {
					return out, e
				}
				if math.MaxUint64-wholeP[row.Year][i] < p || math.MaxUint64-wholeC[row.Year][i] < c {
					return out, fmt.Errorf("country support sum overflow")
				}
				wholeP[row.Year][i] += p
				wholeC[row.Year][i] += c
			}
			if row.Scenario != mainID || row.Discipline == "UNKNOWN" {
				continue
			}
			g := groups[inputKey{row.Year, row.Scheme, row.Level}]
			if g == nil {
				return out, fmt.Errorf("unexpected count taxonomy")
			}
			j, ok := g.labels[row.Discipline]
			if !ok {
				return out, fmt.Errorf("count outside fixed source taxonomy")
			}
			cell := i*len(g.labels) + j
			if g.seen[cell/64]&(1<<uint(cell%64)) != 0 {
				return out, fmt.Errorf("duplicate source model matrix cell")
			}
			g.seen[cell/64] |= 1 << uint(cell%64)
			out.Inputs[g.publications].Input.Matrix.Values[i][j] = row.Publications
			out.Inputs[g.citations].Input.Matrix.Values[i][j] = row.Citations
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, err
		}
	}
	if err := loadBaselines(&out, root, methodRoot); err != nil {
		return out, err
	}
	return out, nil
}

func wholeInteger(value float64) (uint64, error) {
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) || value >= math.Exp2(53) || math.Abs(value-math.Round(value)) > 1e-8 {
		return 0, fmt.Errorf("whole national scale must be an exactly supported nonnegative integer")
	}
	return uint64(math.Round(value)), nil
}

func loadBaselines(out *sourceData, root, methodRoot string) error {
	var manifest struct {
		BaseRunID        string `json:"base_run_id"`
		CompletionSHA256 string `json:"source_completion_sha256"`
	}
	var completion struct {
		Status         string                `json:"status"`
		BaseRunID      string                `json:"base_run_id"`
		ManifestSHA256 string                `json:"manifest_sha256"`
		Artifacts      []checkpoint.Artifact `json:"artifacts"`
	}
	if err := readJSON(filepath.Join(methodRoot, "manifest.json"), &manifest); err != nil {
		return err
	}
	if err := readJSON(filepath.Join(methodRoot, "completion.json"), &completion); err != nil {
		return err
	}
	if manifest.BaseRunID != out.Identity.RunID || completion.BaseRunID != out.Identity.RunID || manifest.CompletionSHA256 != out.Identity.CompletionSHA256 || completion.Status != "complete" {
		return fmt.Errorf("baseline method is not bound to source")
	}
	for _, name := range []string{"completion.json", "manifest.json", "java-reflections-v1.json"} {
		a, err := checkpoint.Hash(filepath.Join(methodRoot, name))
		if err != nil {
			return err
		}
		if name == "completion.json" {
			out.Identity.MethodCompletionSHA256 = a.SHA256
		} else {
			found := false
			for _, e := range completion.Artifacts {
				if filepath.Base(e.Path) == name {
					found = e.Bytes == a.Bytes && e.SHA256 == a.SHA256
				}
			}
			if !found || (name == "manifest.json" && completion.ManifestSHA256 != a.SHA256) {
				return fmt.Errorf("baseline method checksum mismatch %s", name)
			}
		}
		if name == "java-reflections-v1.json" {
			out.Identity.ReflectionModelsSHA256 = a.SHA256
		}
		a.Path = "methods/" + name
		out.Identity.Inputs = append(out.Identity.Inputs, a)
	}
	order := map[string]int{}
	for i, in := range out.Inputs {
		order[modelKey(in.Year, in.Scheme, in.Level, in.Metric)] = i
	}
	out.Baseline = map[string][]pipeline.ModelResult{}
	out.NetworkHashes = make([]string, len(order))
	for _, source := range [][2]string{{"spectral-v1", filepath.Join(root, "eci.json")}, {"java-reflections-v1", filepath.Join(methodRoot, "java-reflections-v1.json")}} {
		f, err := os.Open(source[1])
		if err != nil {
			return err
		}
		decoder := json.NewDecoder(f)
		token, err := decoder.Token()
		if err != nil || token != json.Delim('[') {
			f.Close()
			return fmt.Errorf("invalid baseline models array")
		}
		models := make([]pipeline.ModelResult, len(order))
		seen := map[int]bool{}
		for decoder.More() {
			var m pipeline.ModelResult
			if err = decoder.Decode(&m); err != nil {
				f.Close()
				return err
			}
			i, ok := order[modelKey(m.Year, m.Scheme, m.Level, m.Metric)]
			if !ok || seen[i] {
				f.Close()
				return fmt.Errorf("unexpected or duplicate baseline model")
			}
			hash := complexityfilter.NetworkSHA256(m.Result)
			if source[0] == "spectral-v1" {
				out.NetworkHashes[i] = hash
			} else if out.NetworkHashes[i] != hash {
				f.Close()
				return fmt.Errorf("baseline methods do not share source network")
			}
			m.Result.RCA, m.Result.Advantage, m.Result.Components = nil, nil, nil
			models[i] = m
			seen[i] = true
		}
		_, err = decoder.Token()
		f.Close()
		if err != nil || len(seen) != len(order) {
			return fmt.Errorf("incomplete baseline models")
		}
		out.Baseline[source[0]] = models
	}
	return nil
}
