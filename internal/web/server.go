// Package web serves immutable, real statistics artifacts and an embedded UI.
// It does not launch computations or synthesize placeholder statistical data.
package web

import (
	"embed"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

//go:embed static/* static/vendor/*
var assets embed.FS

type server struct {
	data   *dataset
	corpus string
}

// New loads query-ready artifacts once and returns a concurrency-safe Handler.
// A directory with no completed result returns a useful not-ready page. A
// malformed completed result is an error. Restart the service to select a newly
// completed immutable run or another data directory.
func New(dataDir string) (http.Handler, error) {
	if dataDir == "" {
		return nil, errors.New("web: data directory is required")
	}
	root, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	d, err := loadDataset(root)
	if err != nil {
		return nil, err
	}
	corpus := d.run.Config.Scope.Corpus
	if corpus != "all" {
		corpus = "core"
	}
	return &server{data: d, corpus: corpus}, nil
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.URL.Path == "/api/catalog" {
		serveCatalog(w, r, Catalog{DefaultCorpus: s.corpus, Datasets: []CatalogDataset{s.catalogDataset()}})
		return
	}
	var err error
	r, err = selectCorpus(r, s.corpus)
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid_parameter", err.Error())
		return
	}
	w.Header().Set("X-OpenAlex-Corpus", s.corpus)
	s.serveSelected(w, r)
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
}
func (s *server) serveSelected(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		apiError(w, http.StatusMethodNotAllowed, "method_not_allowed", "只支持 GET 或 HEAD。")
		return
	}
	switch r.URL.Path {
	case "/":
		s.asset(w, r, "index.html", "text/html; charset=utf-8")
	case "/static/app.js":
		s.asset(w, r, "app.js", "text/javascript; charset=utf-8")
	case "/static/bump.js":
		s.asset(w, r, "bump.js", "text/javascript; charset=utf-8")
	case "/static/onboarding.js":
		s.asset(w, r, "onboarding.js", "text/javascript; charset=utf-8")
	case "/static/onboarding.css":
		s.asset(w, r, "onboarding.css", "text/css; charset=utf-8")
	case "/static/app.css":
		s.asset(w, r, "app.css", "text/css; charset=utf-8")
	case "/static/vendor/echarts.min.js":
		s.asset(w, r, "vendor/echarts.min.js", "text/javascript; charset=utf-8")
	case "/api/meta":
		if len(r.URL.Query()) != 0 {
			apiError(w, http.StatusBadRequest, "invalid_parameter", "metadata接口不接受参数。")
			return
		}
		s.metadata(w, r)
	case "/api/series", "/api/series.csv":
		if !s.data.ready {
			apiError(w, http.StatusServiceUnavailable, "not_ready", s.data.reason)
			return
		}
		q, err := s.data.parseQuery(r.URL.Query())
		if err != nil {
			apiError(w, http.StatusBadRequest, "invalid_parameter", err.Error())
			return
		}
		q.Corpus = s.corpus
		response := s.data.query(q)
		if r.URL.Path == "/api/series.csv" {
			writeCSV(w, response)
		} else {
			writeJSON(w, response)
		}
	case "/download":
		s.download(w, r)
	default:
		if strings.HasPrefix(r.URL.Path, "/static/flags/") {
			s.flag(w, r)
			return
		}
		http.NotFound(w, r)
	}
}

func (s *server) asset(w http.ResponseWriter, r *http.Request, name, contentType string) {
	b, err := fs.ReadFile(assets, "static/"+name)
	if err != nil {
		http.Error(w, "asset unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	if strings.HasPrefix(name, "vendor/") {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	if r.Method != http.MethodHead {
		_, _ = w.Write(b)
	}
}

func (s *server) metadata(w http.ResponseWriter, r *http.Request) {
	d := s.data
	if !d.ready {
		writeJSON(w, map[string]any{"ready": false, "reason": d.reason, "status": d.run.Status, "corpus": s.corpus})
		return
	}
	partial := []int{}
	for year := d.start; year <= d.end; year++ {
		if d.partialYears[year] {
			partial = append(partial, year)
		}
	}
	writeJSON(w, map[string]any{
		"ready": true, "status": d.run.Status, "snapshot_date": d.snapshotDate, "corpus": s.corpus, "flag_codes": flagCodes(), "max_series": MaxSeries, "max_eci_countries": len(d.countries),
		"methods": d.methodInfo, "eci_default_method": d.defaultMethod(),
		"year_start": d.start, "year_end": d.end, "default_year_end": min(2025, d.end), "partial_years": partial,
		"countries": d.countries, "layers": d.layers, "scenarios": d.scenarios, "eci_scenario_id": d.eciScenario,
		"dictionary_loaded": d.dictionaryLoaded, "name_fallbacks": d.nameFallbacks, "selected_works": d.run.SelectedWorks,
		"quality": d.run.Quality, "coverage": d.coverage, "graph": d.run.Graph,
		"scope": d.run.Config.Scope, "weighting": d.run.Config.Weighting, "notes": d.run.Notes,
		"downloads": d.downloads,
		"definitions": map[string]string{
			"publications": "所选年发表的圈定范围内文献；按本场景国家与学科权重计数。",
			"citations":    "施引与被引文献均在圈定集合内；年份取施引文献发表年，归到被引文献的国家和学科。",
			"share":        "该国在所选学科的计数量／同年该学科已知国家计数量。全计数场景的分母是归属计数，不是去重论文数。",
			"rca":          "仅在已知国家、已知学科矩阵上计算：(国家该学科／国家全部学科)÷(全球该学科／全球全部学科)。",
			"eci":          "国家整体复杂度，由所选分类层级的全部学科共同计算，选择单一学科不会改变ECI。",
			"pci":          "所选学科的全球复杂度，每年一条全球曲线，不按国家重复生成。",
		},
	})
}

func (s *server) download(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	for key, values := range q {
		if (key != "file" && key != "method") || len(values) != 1 {
			apiError(w, http.StatusBadRequest, "invalid_parameter", "只能下载已列出的统计产物。")
			return
		}
	}
	if len(q["file"]) != 1 {
		apiError(w, http.StatusBadRequest, "invalid_parameter", "必须提供唯一file参数。")
		return
	}
	name := q.Get("file")
	methodID := q.Get("method")
	if methodID == "" {
		methodID = s.data.defaultMethod()
	}
	if _, ok := s.data.method(methodID); !ok {
		apiError(w, http.StatusBadRequest, "invalid_parameter", "未知或未提供的知识复杂度方法。")
		return
	}
	root := s.data.root
	if methodID != DefaultMethod {
		if file, ok := s.data.methodFiles[methodID][name]; ok {
			root = s.data.methodRoots[methodID]
			name = file
			w.Header().Set("X-OpenAlex-Method", methodID)
		}
	}
	if root == s.data.root && !slices.Contains(downloadable, name) {
		apiError(w, http.StatusBadRequest, "invalid_parameter", "只能下载已列出的统计产物。")
		return
	}
	if !s.data.ready || (root == s.data.root && !slices.Contains(s.data.downloads, name)) {
		http.NotFound(w, r)
		return
	}
	path, err := safeFile(root, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "artifact unavailable", http.StatusInternalServerError)
		return
	}
	downloadName := name
	if root != s.data.root && !strings.HasPrefix(downloadName, methodID) {
		downloadName = methodID + "-" + downloadName
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", downloadName))
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}
func apiError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "error": message})
}
func writeCSV(w http.ResponseWriter, response Response) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\"openalex-series.csv\"")
	c := csv.NewWriter(w)
	_ = c.Write([]string{"scenario", "scheme", "level", "discipline", "metric", "basis", "country", "year", "value", "status", "partial", "corpus", "rank", "rank_universe", "series_id", "country_name", "discipline_name", "method", "country_iterations", "discipline_iterations", "source_run_id", "method_applies"})
	for _, series := range response.Series {
		for _, p := range series.Points {
			value := ""
			if p.Value != nil {
				value = strconv.FormatFloat(*p.Value, 'g', -1, 64)
			}
			rank := ""
			if p.Rank != nil {
				rank = strconv.Itoa(*p.Rank)
			}
			_ = c.Write([]string{response.Query.Scenario, response.Query.Scheme, response.Query.Level, series.Discipline, response.Query.Metric, response.Query.Basis, series.Country, strconv.Itoa(p.Year), value, p.Status, strconv.FormatBool(p.Partial), response.Query.Corpus, rank, strconv.Itoa(p.RankUniverse), series.ID, series.CountryName, series.DisciplineName, response.Query.Method, strconv.Itoa(response.Method.CountryIterations), strconv.Itoa(response.Method.DisciplineIterations), response.Method.SourceRunID, strconv.FormatBool(response.MethodApplies)})
		}
	}
	c.Flush()
}
