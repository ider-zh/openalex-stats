package web

// This exporter turns the immutable Go query dataset into bounded static
// lookup shards. The edge adapter never rebuilds a scientific model.
import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"openalexstats/internal/aggregate"
	"openalexstats/internal/checkpoint"
)

const StaticExportVersion = "openalex-static-query-v1"

type StaticExportConfig struct {
	CoreDirectory        string
	AllDirectory         string
	MethodsCoreDirectory string
	MethodsAllDirectory  string
	OutputDirectory      string
	Workers              int
	ReleaseBaseURL       string
}

type StaticArtifact struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type StaticDownload struct {
	Name   string `json:"filename"`
	Path   string `json:"path,omitempty"`
	URL    string `json:"url"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type staticTemplate map[string]any

type staticDataset struct {
	Prefix           string                               `json:"prefix"`
	RunID            string                               `json:"run_id"`
	CompletionSHA256 string                               `json:"completion_sha256"`
	Meta             json.RawMessage                      `json:"meta"`
	Templates        map[string]staticTemplate            `json:"templates"`
	Models           map[string]string                    `json:"models"`
	Downloads        map[string]StaticDownload            `json:"downloads"`
	MethodDownloads  map[string]map[string]StaticDownload `json:"method_downloads"`
	CSVHeader        []string                             `json:"csv_header"`
}

type staticSite struct {
	Version  string                   `json:"version"`
	Catalog  Catalog                  `json:"catalog"`
	Datasets map[string]staticDataset `json:"datasets"`
}

type StaticCorpusReport struct {
	Corpus            string            `json:"corpus"`
	RunID             string            `json:"run_id"`
	CompletionSHA256  string            `json:"completion_sha256"`
	InputCells        int               `json:"input_cells"`
	NonzeroCells      int               `json:"nonzero_cells"`
	CountShards       int               `json:"count_shards"`
	ModelShards       int               `json:"model_shards"`
	MethodCompletions map[string]string `json:"method_completion_sha256,omitempty"`
	Artifacts         []StaticArtifact  `json:"artifacts"`
}

type StaticExportReport struct {
	Version   string               `json:"version"`
	Corpora   []StaticCorpusReport `json:"corpora"`
	Artifacts []StaticArtifact     `json:"artifacts"`
	Files     int                  `json:"files"`
	Bytes     int64                `json:"bytes"`
}

type staticMargins struct {
	Total     [][2]float64   `json:"total"`
	Countries [][][2]float64 `json:"countries"`
}

type staticModels struct {
	// Points are [value or null, status, global competition rank or null,
	// valid global universe]. No eigenvectors or rankings run at the edge.
	Countries   [][][]any `json:"countries"`
	Disciplines [][][]any `json:"disciplines"`
}

type exportCapture struct {
	bytes.Buffer
	headers http.Header
}

func (c *exportCapture) Header() http.Header { return c.headers }
func (c *exportCapture) WriteHeader(int)     {}

// ExportStatic writes a complete deployable lookup dataset, with all scenarios,
// years, countries and taxonomy labels. It never changes production artifacts.
func ExportStatic(ctx context.Context, cfg StaticExportConfig, progress func(string, any)) (report StaticExportReport, err error) {
	if cfg.CoreDirectory == "" || cfg.OutputDirectory == "" || cfg.Workers < 1 || cfg.Workers > 32 {
		return report, fmt.Errorf("static export requires core/output directories and 1..32 workers")
	}
	if cfg.ReleaseBaseURL == "" {
		cfg.ReleaseBaseURL = "https://github.com/ider-zh/openalex-stats/releases/download/snapshot-20260626-v1"
	}
	u, e := url.Parse(cfg.ReleaseBaseURL)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return report, fmt.Errorf("invalid release base URL")
	}
	out, e := filepath.Abs(cfg.OutputDirectory)
	if e != nil {
		return report, e
	}
	// Resolve the nearest existing ancestor as well: a not-yet-created output
	// below a symlink must not bypass immutable-input protection.
	ancestor := out
	var suffix []string
	for {
		if _, statErr := os.Lstat(ancestor); statErr == nil {
			break
		} else if !os.IsNotExist(statErr) {
			return report, statErr
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return report, fmt.Errorf("cannot resolve output ancestor")
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = parent
	}
	resolved, e := filepath.EvalSymlinks(ancestor)
	if e != nil {
		return report, e
	}
	for i := len(suffix) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, suffix[i])
	}
	out = resolved
	for _, input := range []string{cfg.CoreDirectory, cfg.AllDirectory, cfg.MethodsCoreDirectory, cfg.MethodsAllDirectory} {
		if input == "" {
			continue
		}
		root, e := filepath.Abs(input)
		if e != nil {
			return report, e
		}
		if resolved, e := filepath.EvalSymlinks(root); e == nil {
			root = resolved
		}
		rel, e := filepath.Rel(root, out)
		if e != nil || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)) {
			return report, fmt.Errorf("static output must be outside immutable result directories")
		}
	}
	if e = os.MkdirAll(out, 0755); e != nil {
		return report, e
	}
	unlock, e := checkpoint.LockDirectory(out)
	if e != nil {
		return report, e
	}
	defer unlock()
	report.Version = StaticExportVersion
	site := staticSite{Version: StaticExportVersion, Catalog: Catalog{DefaultCorpus: "core", Datasets: []CatalogDataset{}}, Datasets: map[string]staticDataset{}}
	for _, pair := range [][2]string{{"core", cfg.CoreDirectory}, {"all", cfg.AllDirectory}} {
		if pair[1] == "" {
			continue
		}
		if e = ctx.Err(); e != nil {
			return report, e
		}
		if progress != nil {
			progress("static_load", map[string]any{"corpus": pair[0]})
		}
		r, control, e := exportStaticCorpus(ctx, out, pair[0], pair[1], cfg, progress)
		if e != nil {
			return report, e
		}
		report.Corpora = append(report.Corpora, r)
		report.Artifacts = append(report.Artifacts, r.Artifacts...)
		site.Datasets[pair[0]] = control
		var meta struct {
			SelectedWorks int    `json:"selected_works"`
			Status        string `json:"status"`
		}
		if e = json.Unmarshal(control.Meta, &meta); e != nil {
			return report, e
		}
		name := "主数据 · Core"
		if pair[0] == "all" {
			name = "全数据 · Core + XPAC"
		}
		site.Catalog.Datasets = append(site.Catalog.Datasets, CatalogDataset{ID: pair[0], DisplayName: name, Status: meta.Status, SelectedWorks: meta.SelectedWorks})
		runtime.GC()
	}
	a, e := exportJSON(out, "data/site.json", site, false)
	if e != nil {
		return report, e
	}
	report.Artifacts = append(report.Artifacts, a)
	sort.Slice(report.Artifacts, func(i, j int) bool { return report.Artifacts[i].Path < report.Artifacts[j].Path })
	for _, a := range report.Artifacts {
		report.Bytes += a.Bytes
	}
	report.Files = len(report.Artifacts)
	_, e = exportJSON(out, "export-manifest.json", report, false)
	return report, e
}

func exportStaticCorpus(ctx context.Context, out, corpus, input string, cfg StaticExportConfig, progress func(string, any)) (report StaticCorpusReport, control staticDataset, err error) {
	completionBytes, err := readSafe(input, "completion.json")
	if err != nil {
		return report, control, fmt.Errorf("completed immutable result required: %w", err)
	}
	var completion struct {
		Version   int                   `json:"version"`
		RunID     string                `json:"run_id"`
		Run       checkpoint.Artifact   `json:"run"`
		Artifacts []checkpoint.Artifact `json:"artifacts"`
	}
	if err = json.Unmarshal(completionBytes, &completion); err != nil || completion.Version != 1 || len(completion.RunID) != 64 {
		return report, control, fmt.Errorf("invalid completion manifest")
	}
	completionSum := sha256.Sum256(completionBytes)
	report = StaticCorpusReport{Corpus: corpus, RunID: completion.RunID, CompletionSHA256: hex.EncodeToString(completionSum[:])}
	sources := append([]checkpoint.Artifact{completion.Run}, completion.Artifacts...)
	owned := map[string]bool{}
	for _, expected := range sources {
		if err = ctx.Err(); err != nil {
			return report, control, err
		}
		name := filepath.Base(expected.Path)
		if owned[name] {
			return report, control, fmt.Errorf("duplicate source completion artifact: %s", name)
		}
		owned[name] = true
		p, e := safeFile(input, name)
		if e != nil {
			return report, control, e
		}
		actual, e := checkpoint.Hash(p)
		if e != nil {
			return report, control, e
		}
		if actual.Bytes != expected.Bytes || actual.SHA256 != expected.SHA256 {
			return report, control, fmt.Errorf("source completion mismatch: %s", name)
		}
	}
	for _, name := range []string{"run.json", "statistics.parquet", "cube.parquet", "dictionary.json", "scenarios.json", "coverage.json", "eci.json"} {
		if _, e := safeFile(input, name); e == nil && !owned[name] {
			return report, control, fmt.Errorf("query input is not owned by completion: %s", name)
		}
	}
	d, err := loadDataset(input)
	if err != nil {
		return report, control, err
	}
	if !d.ready || d.run.Config.Scope.Corpus != corpus || d.run.RunID != completion.RunID {
		return report, control, fmt.Errorf("corpus %s incomplete or mismatched", corpus)
	}
	if d.start > 65535 || d.end-d.start > 255 || len(d.countries) >= 65535 {
		return report, control, fmt.Errorf("query dictionary exceeds static matrix header")
	}
	methodDir := cfg.MethodsCoreDirectory
	if corpus == "all" {
		methodDir = cfg.MethodsAllDirectory
	}
	if err = loadDatasetMethods(d, methodDir); err != nil {
		return report, control, err
	}
	control = staticDataset{Prefix: "/data/" + corpus + "-" + completion.RunID[:12], RunID: completion.RunID, CompletionSHA256: report.CompletionSHA256, Templates: map[string]staticTemplate{}, Models: map[string]string{}, Downloads: map[string]StaticDownload{}, MethodDownloads: map[string]map[string]StaticDownload{}}
	capture := &exportCapture{headers: http.Header{}}
	(&server{data: d, corpus: corpus}).metadata(capture, nil)
	control.Meta = append(json.RawMessage(nil), capture.Bytes()...)
	csvCapture := &exportCapture{headers: http.Header{}}
	writeCSV(csvCapture, Response{})
	control.CSVHeader, err = csv.NewReader(strings.NewReader(csvCapture.String())).Read()
	if err != nil {
		return report, control, err
	}
	base := strings.TrimPrefix(control.Prefix, "/")
	countries := make([]string, 0, len(d.countries)+1)
	countryOrd := map[string]int{}
	for _, c := range d.countries {
		countryOrd[c.ID] = len(countries)
		countries = append(countries, c.ID)
	}
	countryOrd[aggregate.Unknown] = len(countries)
	countries = append(countries, aggregate.Unknown)
	type group struct{ s, l, d int }
	rows := map[group][][4]float64{}
	scenarioOrd := map[string]int{}
	for i, s := range d.scenarios {
		scenarioOrd[s.ID] = i
	}
	layerOrd := map[string]int{}
	disciplineOrd := map[string]map[string]int{}
	for i, l := range d.layers {
		key := l.Scheme + "/" + l.Level
		layerOrd[key] = i
		disciplineOrd[key] = map[string]int{}
		for j, label := range l.Labels {
			disciplineOrd[key][label.ID] = j
		}
		disciplineOrd[key][aggregate.Unknown] = len(l.Labels)
	}
	report.InputCells = len(d.rows)
	for k, v := range d.rows {
		if v.p == 0 && v.c == 0 {
			continue
		}
		layer := k.scheme + "/" + k.level
		ci, cok := countryOrd[k.country]
		di, dok := disciplineOrd[layer][k.discipline]
		si, sok := scenarioOrd[k.scenario]
		li, lok := layerOrd[layer]
		if !cok || !dok || !sok || !lok {
			return report, control, fmt.Errorf("count cell outside export dictionary")
		}
		g := group{si, li, di}
		rows[g] = append(rows[g], [4]float64{float64(ci), float64(k.year - d.start), v.p, v.c})
		report.NonzeroCells++
	}
	type job struct {
		path  string
		value any
	}
	jobs := make([]job, 0, len(rows)+48)
	for si, s := range d.scenarios {
		for li, l := range d.layers {
			years := d.end - d.start + 1
			margins := staticMargins{Total: make([][2]float64, years), Countries: make([][][2]float64, len(countries))}
			for ci, c := range countries {
				margins.Countries[ci] = make([][2]float64, years)
				for yi := 0; yi < years; yi++ {
					v := d.countryMargins[marginKey{s.ID, l.Scheme, l.Level, c, d.start + yi}]
					margins.Countries[ci][yi] = [2]float64{v.p, v.c}
				}
			}
			for yi := 0; yi < years; yi++ {
				v := d.totalMargins[marginKey{s.ID, l.Scheme, l.Level, "", d.start + yi}]
				margins.Total[yi] = [2]float64{v.p, v.c}
			}
			prefix := fmt.Sprintf("%s/counts/%d/%d", base, si, li)
			matrix := encodeStaticMatrix(d.start, years, len(countries), margins.Total, nil)
			for ci, points := range margins.Countries {
				for yi, v := range points {
					putStaticPair(matrix, years, ci, yi, v)
				}
			}
			jobs = append(jobs, job{prefix + "/margins.bin.gz", matrix})
			ids := make([]string, 0, len(l.Labels)+1)
			for _, label := range l.Labels {
				ids = append(ids, label.ID)
			}
			ids = append(ids, aggregate.Unknown)
			for di, id := range ids {
				records := rows[group{si, li, di}]
				if records == nil {
					records = [][4]float64{}
				}
				sort.Slice(records, func(i, j int) bool {
					if records[i][0] != records[j][0] {
						return records[i][0] < records[j][0]
					}
					return records[i][1] < records[j][1]
				})
				totals := make([][2]float64, years)
				for yi := 0; yi < years; yi++ {
					v := d.disciplineMargins[marginKey{s.ID, l.Scheme, l.Level, id, d.start + yi}]
					totals[yi] = [2]float64{v.p, v.c}
				}
				jobs = append(jobs, job{fmt.Sprintf("%s/%d.bin.gz", prefix, di), encodeStaticMatrix(d.start, years, len(countries), totals, records)})
				report.CountShards++
			}
			for _, method := range d.methodInfo {
				for _, metric := range []string{"publications", "citations", "share", "rca", "eci", "pci"} {
					for _, basis := range []string{"publications", "citations"} {
						q := Query{Scenario: s.ID, Scheme: l.Scheme, Level: l.Level, Metric: metric, Basis: basis, Method: method.ID}
						r := d.query(q)
						template, e := staticResponseTemplate(r)
						if e != nil {
							return report, control, e
						}
						control.Templates[strings.Join([]string{method.ID, s.ID, l.Scheme, l.Level, metric, basis}, "/")] = template
					}
				}
				for _, basis := range []string{"publications", "citations"} {
					present := false
					for y := d.start; y <= d.end; y++ {
						if _, ok := d.methodModels[method.ID][modelKey{s.ID, l.Scheme, l.Level, basis, y}]; ok {
							present = true
							break
						}
					}
					if !present {
						continue
					}
					q := Query{Scenario: s.ID, Scheme: l.Scheme, Level: l.Level, Basis: basis, Metric: "eci", Start: d.start, End: d.end, Countries: countries[:len(countries)-1], Method: method.ID}
					r := d.query(q)
					model := staticModels{Countries: make([][][]any, len(r.Series)), Disciplines: make([][][]any, len(l.Labels))}
					for i, series := range r.Series {
						model.Countries[i] = compactStaticPoints(series.Points)
					}
					q.Metric = "pci"
					q.Disciplines = ids[:len(ids)-1]
					r = d.query(q)
					for i, series := range r.Series {
						model.Disciplines[i] = compactStaticPoints(series.Points)
					}
					p := fmt.Sprintf("%s/models/%s/%d/%d/%s.json.gz", base, method.ID, si, li, basis)
					jobs = append(jobs, job{p, model})
					control.Models[strings.Join([]string{method.ID, s.ID, l.Scheme, l.Level, basis}, "/")] = "/" + p
					report.ModelShards++
				}
			}
		}
	}
	queue := make(chan job)
	result := make(chan StaticArtifact, len(jobs))
	fail := make(chan error, 1)
	workctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range queue {
				if workctx.Err() != nil {
					continue
				}
				var a StaticArtifact
				var e error
				if raw, ok := j.value.([]byte); ok {
					a, e = exportGzipBytes(out, j.path, raw)
				} else {
					a, e = exportJSON(out, j.path, j.value, true)
				}
				if e != nil {
					select {
					case fail <- e:
						cancel()
					default:
					}
					continue
				}
				result <- a
			}
		}()
	}
	for _, j := range jobs {
		if workctx.Err() != nil {
			break
		}
		queue <- j
	}
	close(queue)
	wg.Wait()
	close(result)
	select {
	case e := <-fail:
		return report, control, e
	default:
	}
	if err = ctx.Err(); err != nil {
		return report, control, err
	}
	for a := range result {
		report.Artifacts = append(report.Artifacts, a)
	}
	for _, expected := range append(sources, checkpoint.Artifact{Path: "completion.json", Bytes: int64(len(completionBytes)), SHA256: report.CompletionSHA256}) {
		name := filepath.Base(expected.Path)
		download := StaticDownload{Name: name, URL: strings.TrimSuffix(cfg.ReleaseBaseURL, "/") + "/" + corpus + "-" + name, Bytes: expected.Bytes, SHA256: expected.SHA256}
		if expected.Bytes < 25<<20 {
			p, e := safeFile(input, name)
			if e != nil {
				return report, control, e
			}
			b, e := os.ReadFile(p)
			if e != nil {
				return report, control, e
			}
			rel := base + "/artifacts/" + name
			a, e := exportBytes(out, rel, b)
			if e != nil {
				return report, control, e
			}
			report.Artifacts = append(report.Artifacts, a)
			download.Path = "/" + rel
		}
		control.Downloads[name] = download
	}
	report.MethodCompletions = map[string]string{}
	for methodID, root := range d.methodRoots {
		b, e := readSafe(root, "completion.json")
		if e != nil {
			return report, control, e
		}
		report.MethodCompletions[methodID] = bytesSHA(b)
		control.MethodDownloads[methodID] = map[string]StaticDownload{}
		for alias, name := range d.methodFiles[methodID] {
			p, e := safeFile(root, name)
			if e != nil {
				return report, control, e
			}
			a, e := checkpoint.Hash(p)
			if e != nil {
				return report, control, e
			}
			download := StaticDownload{Name: name, URL: strings.TrimSuffix(cfg.ReleaseBaseURL, "/") + "/" + corpus + "-methods-" + name, Bytes: a.Bytes, SHA256: a.SHA256}
			if a.Bytes < 25<<20 {
				raw, e := os.ReadFile(p)
				if e != nil {
					return report, control, e
				}
				rel := base + "/method-artifacts/" + methodID + "/" + name
				artifact, e := exportBytes(out, rel, raw)
				if e != nil {
					return report, control, e
				}
				report.Artifacts = append(report.Artifacts, artifact)
				download.Path = "/" + rel
			}
			control.MethodDownloads[methodID][alias] = download
		}
	}
	sort.Slice(report.Artifacts, func(i, j int) bool { return report.Artifacts[i].Path < report.Artifacts[j].Path })
	if progress != nil {
		progress("static_exported", map[string]any{"corpus": corpus, "input_cells": report.InputCells, "nonzero_cells": report.NonzeroCells, "count_shards": report.CountShards, "model_shards": report.ModelShards})
	}
	return report, control, nil
}

func compactStaticPoints(points []Point) [][]any {
	out := make([][]any, len(points))
	for i, p := range points {
		out[i] = []any{p.Value, p.Status, p.Rank, p.RankUniverse}
	}
	return out
}

func staticResponseTemplate(r Response) (staticTemplate, error) {
	b, e := json.Marshal(r)
	if e != nil {
		return nil, e
	}
	var out staticTemplate
	if e = json.Unmarshal(b, &out); e != nil {
		return nil, e
	}
	delete(out, "query")
	delete(out, "series")
	return out, nil
}

// OAQ1: 16-byte little-endian header (magic, uint16 start, uint16 years,
// uint16 countries, reserved zero bytes), then global per-year float64 pairs,
// then country-major/year-major float64 publication/citation pairs. Exact
// binary64 values avoid decimal parsing and large object graphs at the edge.
func encodeStaticMatrix(start, years, countries int, totals [][2]float64, records [][4]float64) []byte {
	b := make([]byte, 16+(countries+1)*years*16)
	copy(b, "OAQ1")
	binary.LittleEndian.PutUint16(b[4:], uint16(start))
	binary.LittleEndian.PutUint16(b[6:], uint16(years))
	binary.LittleEndian.PutUint16(b[8:], uint16(countries))
	for yi, v := range totals {
		binary.LittleEndian.PutUint64(b[16+yi*16:], math.Float64bits(v[0]))
		binary.LittleEndian.PutUint64(b[24+yi*16:], math.Float64bits(v[1]))
	}
	for _, r := range records {
		putStaticPair(b, years, int(r[0]), int(r[1]), [2]float64{r[2], r[3]})
	}
	return b
}

func putStaticPair(b []byte, years, country, year int, v [2]float64) {
	offset := 16 + years*16 + (country*years+year)*16
	binary.LittleEndian.PutUint64(b[offset:], math.Float64bits(v[0]))
	binary.LittleEndian.PutUint64(b[offset+8:], math.Float64bits(v[1]))
}

func exportGzipBytes(root, path string, b []byte) (StaticArtifact, error) {
	var buf bytes.Buffer
	w, e := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if e != nil {
		return StaticArtifact{}, e
	}
	if _, e = w.Write(b); e != nil {
		return StaticArtifact{}, e
	}
	if e = w.Close(); e != nil {
		return StaticArtifact{}, e
	}
	return exportBytes(root, path, buf.Bytes())
}

func exportJSON(root, path string, v any, compressed bool) (StaticArtifact, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return StaticArtifact{}, err
	}
	b = append(b, '\n')
	if compressed {
		return exportGzipBytes(root, path, b)
	}
	return exportBytes(root, path, b)
}

func exportBytes(root, relative string, b []byte) (StaticArtifact, error) {
	sum := sha256.Sum256(b)
	a := StaticArtifact{Path: filepath.ToSlash(relative), Bytes: int64(len(b)), SHA256: hex.EncodeToString(sum[:])}
	p := filepath.Join(root, filepath.FromSlash(relative))
	if old, err := os.ReadFile(p); err == nil {
		if !bytes.Equal(old, b) {
			return a, fmt.Errorf("immutable static output differs: %s; select a new output directory", relative)
		}
		return a, nil
	} else if !os.IsNotExist(err) {
		return a, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return a, err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".export-*.tmp")
	if err != nil {
		return a, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return a, err
	}
	if err = f.Close(); err != nil {
		return a, err
	}
	return a, os.Rename(tmp, p)
}
