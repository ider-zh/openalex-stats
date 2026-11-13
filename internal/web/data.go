package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/aggregate"
	"openalexstats/internal/eci"
	"openalexstats/internal/results"
)

type Label struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	ParentIDs   []string `json:"parent_ids,omitempty"`
}
type Layer struct {
	Scheme string  `json:"scheme"`
	Level  string  `json:"level"`
	Labels []Label `json:"labels"`
}
type Dictionary struct {
	Countries []Label `json:"countries"`
	Layers    []Layer `json:"layers"`
}
type modelResult struct {
	ScenarioID string     `json:"scenario_id,omitempty"`
	Year       int        `json:"year"`
	Scheme     string     `json:"scheme"`
	Level      string     `json:"level"`
	Metric     string     `json:"metric"`
	Result     eci.Result `json:"result"`
}
type runRecord struct {
	RunID         string `json:"run_id"`
	Version       string `json:"version"`
	Status        string `json:"status"`
	SelectedWorks int    `json:"selected_works"`
	Config        struct {
		YearStart int `json:"year_start"`
		YearEnd   int `json:"year_end"`
		Scope     struct {
			Corpus          string   `json:"corpus"`
			CorpusYearStart int      `json:"corpus_year_start"`
			Blacklist       []string `json:"blacklist"`
		} `json:"scope"`
		Weighting aggregate.Config `json:"weighting"`
		Input     struct {
			SnapshotDate string `json:"snapshot_date"`
		} `json:"input"`
	} `json:"config"`
	Quality aggregate.Quality `json:"quality"`
	Graph   struct {
		RetainedEdges uint64 `json:"retained_edges"`
	} `json:"graph"`
	Notes []string `json:"notes"`
}
type pointKey struct {
	scenario, scheme, level, discipline, country string
	year                                         int
}
type marginKey struct {
	scenario, scheme, level, label string
	year                           int
}
type modelKey struct {
	scenario, scheme, level, metric string
	year                            int
}
type values struct{ p, c float64 }

func (v values) get(basis string) float64 {
	if basis == "citations" {
		return v.c
	}
	return v.p
}

type dataset struct {
	root              string
	run               runRecord
	rawRun            json.RawMessage
	ready             bool
	reason            string
	start, end        int
	snapshotDate      string
	partialYears      map[int]bool
	rows              map[pointKey]values
	countryMargins    map[marginKey]values
	disciplineMargins map[marginKey]values
	totalMargins      map[marginKey]values
	models            map[modelKey]eci.Result
	methodModels      map[string]map[modelKey]eci.Result
	methodInfo        []MethodInfo
	methodRoots       map[string]string
	methodFiles       map[string]map[string]string
	profiles          map[string]*profileDataset
	profileInfos      []ComplexityProfileInfo
	defaultProfileID  string
	countries         []Label
	countrySet        map[string]bool
	layers            []Layer
	disciplineSet     map[string]map[string]bool
	scenarios         []results.Scenario
	scenarioSet       map[string]bool
	eciScenario       string
	coverage          json.RawMessage
	dictionaryLoaded  bool
	nameFallbacks     int
	downloads         []string
}

var downloadable = []string{"statistics.parquet", "cube.parquet", "country_eci.parquet", "discipline_pci.parquet", "rca.parquet", "eci.json", "model_diagnostics.json", "run.json", "coverage.json", "dictionary.json", "scenarios.json"}

func loadDataset(root string) (*dataset, error) {
	d := &dataset{root: root, reason: "统计结果尚未就绪。页面只展示实际结果，不生成示意数据。", countries: []Label{}, rows: map[pointKey]values{},
		countryMargins: map[marginKey]values{}, disciplineMargins: map[marginKey]values{}, totalMargins: map[marginKey]values{}, models: map[modelKey]eci.Result{},
		countrySet: map[string]bool{}, disciplineSet: map[string]map[string]bool{}, scenarioSet: map[string]bool{}, partialYears: map[int]bool{}}
	b, err := readSafe(root, "run.json")
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &d.run); err != nil {
		return nil, fmt.Errorf("web: invalid run.json: %w", err)
	}
	d.rawRun = append([]byte(nil), b...)
	if d.run.Status != "complete" && d.run.Status != "pilot_complete" && d.run.Status != "input_complete" && d.run.Status != "full_snapshot_complete" {
		d.reason = "当前结果状态为 " + d.run.Status + "，尚未发布完整的本次运行结果。"
		return d, nil
	}
	d.start, d.end = d.run.Config.YearStart, d.run.Config.YearEnd
	if d.start < 1 || d.end < d.start || d.end-d.start > 200 {
		return nil, errors.New("web: invalid result year range")
	}
	d.snapshotDate = d.run.Config.Input.SnapshotDate
	if len(d.snapshotDate) >= 10 {
		var year int
		_, _ = fmt.Sscanf(d.snapshotDate[:4], "%d", &year)
		if year > 0 && d.snapshotDate[5:10] != "12-31" {
			d.partialYears[year] = true
		}
	} else if d.end == 2026 {
		// This project's June 2026 snapshot can also be addressed by explicit
		// file paths, whose pilot manifest may not contain a snapshot date.
		d.partialYears[2026] = true
	}
	statsPath, statsErr := safeFile(root, "statistics.parquet")
	if statsErr == nil {
		err = readParquet[results.Row](statsPath, func(r results.Row) error {
			if r.ScenarioID == "" {
				return errors.New("web: statistics row has no scenario_id")
			}
			if !d.scenarioSet[r.ScenarioID] {
				d.scenarioSet[r.ScenarioID] = true
				d.scenarios = append(d.scenarios, results.Scenario{ID: r.ScenarioID, CountryMode: r.CountryMode, ConceptMode: r.ConceptMode, ConceptThreshold: r.ConceptThreshold, Main: r.Main})
			}
			return d.addRow(r.ScenarioID, aggregate.Row{Country: r.Country, Year: r.Year, Scheme: r.Scheme, Level: r.Level, DisciplineID: r.DisciplineID, Publications: r.Publications, Citations: r.Citations})
		})
	} else if errors.Is(statsErr, os.ErrNotExist) {
		cubePath, e := safeFile(root, "cube.parquet")
		if errors.Is(e, os.ErrNotExist) {
			return d, nil
		}
		if e != nil {
			return nil, e
		}
		w := d.run.Config.Weighting
		d.scenarios = []results.Scenario{{ID: "current", CountryMode: w.CountryMode, ConceptMode: w.ConceptMode, ConceptThreshold: w.ConceptThreshold, Main: true}}
		d.scenarioSet["current"] = true
		err = readParquet[aggregate.Row](cubePath, func(r aggregate.Row) error { return d.addRow("current", r) })
	} else {
		return nil, statsErr
	}
	if err != nil {
		return nil, err
	}
	if b, err := readSafe(root, "scenarios.json"); err == nil {
		var scenarios []results.Scenario
		if err := json.Unmarshal(b, &scenarios); err != nil {
			return nil, fmt.Errorf("web: invalid scenarios.json: %w", err)
		}
		for _, s := range scenarios {
			if s.ID == "" {
				return nil, errors.New("web: empty scenario ID")
			}
			if !d.scenarioSet[s.ID] {
				d.scenarioSet[s.ID] = true
				d.scenarios = append(d.scenarios, s)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, s := range d.scenarios {
		if s.Main && s.CountryMode == "fractional" {
			d.eciScenario = s.ID
			if s.ConceptMode == "whole" {
				break
			}
		}
	}
	// Legacy exports omit an owning scenario on eci.json records.
	if d.eciScenario == "" {
		for _, s := range d.scenarios {
			if s.ID == "fractional-concept-whole-0.3" || s.ID == "fractional-concept-0.3" || s.ID == "current" {
				d.eciScenario = s.ID
				break
			}
		}
	}
	if d.eciScenario == "" && len(d.scenarios) > 0 {
		d.eciScenario = d.scenarios[0].ID
	}
	if b, err := readSafe(root, "eci.json"); err == nil {
		var models []modelResult
		if err := json.Unmarshal(b, &models); err != nil {
			return nil, fmt.Errorf("web: invalid eci.json: %w", err)
		}
		// If metadata and rows are legacy, prefer an explicitly published model
		// owner to an arbitrary scenario sorting order.
		hasDefault := false
		for _, m := range models {
			if m.ScenarioID == d.eciScenario || m.ScenarioID == "" {
				hasDefault = true
				break
			}
		}
		if !hasDefault {
			for _, m := range models {
				if d.scenarioSet[m.ScenarioID] {
					d.eciScenario = m.ScenarioID
					break
				}
			}
		}
		for _, m := range models {
			scenario := m.ScenarioID
			if scenario == "" {
				scenario = d.eciScenario
			}
			d.models[modelKey{scenario, m.Scheme, m.Level, m.Metric, m.Year}] = m.Result
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if b, err := readSafe(root, "coverage.json"); err == nil {
		if !json.Valid(b) {
			return nil, errors.New("web: invalid coverage.json")
		}
		d.coverage = append([]byte(nil), b...)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var dictionary Dictionary
	if b, err := readSafe(root, "dictionary.json"); err == nil {
		if err := json.Unmarshal(b, &dictionary); err != nil {
			return nil, fmt.Errorf("web: invalid dictionary.json: %w", err)
		}
		d.dictionaryLoaded = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	d.labels(dictionary)
	for _, name := range downloadable {
		if _, err := safeFile(root, name); err == nil {
			d.downloads = append(d.downloads, name)
		}
	}
	sort.Slice(d.scenarios, func(i, j int) bool {
		if d.scenarios[i].ID == d.scenarios[j].ID {
			return false
		}
		if d.scenarios[i].ID == d.eciScenario {
			return true
		}
		if d.scenarios[j].ID == d.eciScenario {
			return false
		}
		return d.scenarios[i].ID < d.scenarios[j].ID
	})
	d.ready, d.reason = true, ""
	initDatasetMethods(d)
	return d, nil
}

func (d *dataset) addRow(scenario string, r aggregate.Row) error {
	if !validLayer(r.Scheme, r.Level) || r.Country == "" || r.DisciplineID == "" || r.Year < d.start || r.Year > d.end ||
		math.IsNaN(r.Publications) || math.IsInf(r.Publications, 0) || r.Publications < 0 || math.IsNaN(r.Citations) || math.IsInf(r.Citations, 0) || r.Citations < 0 {
		return errors.New("web: invalid statistics row")
	}
	k := pointKey{scenario, r.Scheme, r.Level, r.DisciplineID, r.Country, r.Year}
	v := d.rows[k]
	v.p, v.c = v.p+r.Publications, v.c+r.Citations
	d.rows[k] = v
	layer := r.Scheme + "/" + r.Level
	if d.disciplineSet[layer] == nil {
		d.disciplineSet[layer] = map[string]bool{}
	}
	if r.DisciplineID != aggregate.Unknown {
		d.disciplineSet[layer][r.DisciplineID] = true
	}
	if r.Country != aggregate.Unknown {
		d.countrySet[r.Country] = true
	}
	if r.Country == aggregate.Unknown || r.DisciplineID == aggregate.Unknown {
		return nil
	}
	add := func(m map[marginKey]values, label string) {
		k := marginKey{scenario, r.Scheme, r.Level, label, r.Year}
		v := m[k]
		m[k] = values{v.p + r.Publications, v.c + r.Citations}
	}
	add(d.countryMargins, r.Country)
	add(d.disciplineMargins, r.DisciplineID)
	add(d.totalMargins, "")
	return nil
}

func (d *dataset) labels(dict Dictionary) {
	countryNames := map[string]string{}
	disciplineNames := map[string]map[string]string{}
	parents := map[string]map[string][]string{}
	for _, c := range dict.Countries {
		countryNames[c.ID] = c.DisplayName
		if c.ID != "" && c.ID != aggregate.Unknown {
			d.countrySet[c.ID] = true
		}
	}
	for _, l := range dict.Layers {
		key := l.Scheme + "/" + l.Level
		disciplineNames[key] = map[string]string{}
		parents[key] = map[string][]string{}
		for _, v := range l.Labels {
			disciplineNames[key][v.ID] = v.DisplayName
			parents[key][v.ID] = append([]string(nil), v.ParentIDs...)
			if v.ID != aggregate.Unknown && v.ID != "" {
				if d.disciplineSet[key] == nil {
					d.disciplineSet[key] = map[string]bool{}
				}
				d.disciplineSet[key][v.ID] = true
			}
		}
	}
	name := func(id, value string) string {
		if value == "" {
			d.nameFallbacks++
			return id
		}
		return value
	}
	for id := range d.countrySet {
		d.countries = append(d.countries, Label{ID: id, DisplayName: name(id, countryNames[id])})
	}
	sort.Slice(d.countries, func(i, j int) bool { return d.countries[i].DisplayName < d.countries[j].DisplayName })
	for _, pair := range [][2]string{{"topics", "field"}, {"topics", "subfield"}, {"concepts", "0"}, {"concepts", "1"}} {
		key := pair[0] + "/" + pair[1]
		l := Layer{Scheme: pair[0], Level: pair[1], Labels: []Label{}}
		for id := range d.disciplineSet[key] {
			l.Labels = append(l.Labels, Label{ID: id, DisplayName: name(id, disciplineNames[key][id]), ParentIDs: parents[key][id]})
		}
		sort.Slice(l.Labels, func(i, j int) bool { return l.Labels[i].DisplayName < l.Labels[j].DisplayName })
		d.layers = append(d.layers, l)
	}
}

func validLayer(scheme, level string) bool {
	return scheme == "topics" && (level == "field" || level == "subfield") || scheme == "concepts" && (level == "0" || level == "1")
}

func readParquet[T any](path string, yield func(T) error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("web: invalid result parquet: %v", p)
		}
	}()
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := parquet.NewGenericReader[T](f)
	defer r.Close()
	batch := make([]T, 1024)
	for {
		n, err := r.Read(batch)
		for _, row := range batch[:n] {
			if e := yield(row); e != nil {
				return e
			}
		}
		clear(batch)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
}

func safeFile(root, name string) (string, error) {
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
		return "", errors.New("web: invalid artifact name")
	}
	path, err := filepath.EvalSymlinks(filepath.Join(root, name))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(rel) {
		return "", errors.New("web: artifact escapes data directory")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("web: artifact is not a regular file")
	}
	return path, nil
}

func readSafe(root, name string) ([]byte, error) {
	path, err := safeFile(root, name)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}
