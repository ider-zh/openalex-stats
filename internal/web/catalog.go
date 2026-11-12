package web

import (
	"errors"
	"fmt"
	"net/http"
)

type CatalogDataset struct {
	ID            string `json:"id"`
	DisplayName   string `json:"display_name"`
	Status        string `json:"status"`
	SelectedWorks int    `json:"selected_works"`
}
type Catalog struct {
	DefaultCorpus         string               `json:"default_corpus"`
	Datasets              []CatalogDataset     `json:"datasets"`
	DefaultCountryVersion string               `json:"default_country_version"`
	CountryVersions       []CountryVersionInfo `json:"country_versions"`
}
type catalogServer struct {
	servers map[string]*server
	catalog Catalog
}

// NewCatalog opens fixed immutable result directories. It requires the default
// core corpus and optionally all (core plus XPAC). Both must be completed and
// their recorded scope must match the catalog key. No request chooses a path.
func NewCatalog(datasets map[string]string) (http.Handler, error) {
	return NewCatalogWithMethods(datasets, nil)
}

func NewCatalogWithMethods(datasets, methodDirectories map[string]string) (http.Handler, error) {
	if len(datasets) == 0 || datasets["core"] == "" {
		return nil, errors.New("web: catalog requires a core result directory")
	}
	for id := range datasets {
		if id != "core" && id != "all" {
			return nil, fmt.Errorf("web: unsupported corpus %q", id)
		}
	}
	for id := range methodDirectories {
		if _, ok := datasets[id]; !ok {
			return nil, fmt.Errorf("web: method directory has no matching corpus")
		}
	}
	out := &catalogServer{servers: map[string]*server{}, catalog: Catalog{DefaultCorpus: "core", DefaultCountryVersion: "original", CountryVersions: []CountryVersionInfo{countryVersionInfo("original")}}}
	for _, id := range []string{"core", "all"} {
		dir, exists := datasets[id]
		if !exists {
			continue
		}
		handler, err := NewWithMethods(dir, methodDirectories[id])
		if err != nil {
			return nil, fmt.Errorf("web corpus %s: %w", id, err)
		}
		s := handler.(*server)
		if !s.data.ready {
			return nil, fmt.Errorf("web: corpus %s is not completed", id)
		}
		if s.data.run.Config.Scope.Corpus != id {
			return nil, fmt.Errorf("web: corpus %s directory declares scope %q", id, s.data.run.Config.Scope.Corpus)
		}
		out.servers[id] = s
		out.catalog.Datasets = append(out.catalog.Datasets, s.catalogDataset())
	}
	return out, nil
}
func (s *server) catalogDataset() CatalogDataset {
	name := "主数据 · Core"
	if s.corpus == "all" {
		name = "全数据 · Core + XPAC"
	}
	return CatalogDataset{ID: s.corpus, DisplayName: name, Status: s.data.run.Status, SelectedWorks: s.data.run.SelectedWorks}
}
func serveCatalog(w http.ResponseWriter, r *http.Request, c Catalog) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		apiError(w, http.StatusMethodNotAllowed, "method_not_allowed", "只支持 GET 或 HEAD。")
		return
	}
	if len(r.URL.Query()) != 0 {
		apiError(w, http.StatusBadRequest, "invalid_parameter", "catalog接口不接受参数。")
		return
	}
	writeJSON(w, c)
}
func (s *catalogServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.URL.Path == "/api/catalog" {
		serveCatalog(w, r, s.catalog)
		return
	}
	id := s.catalog.DefaultCorpus
	if values, ok := r.URL.Query()["corpus"]; ok {
		if len(values) != 1 || values[0] == "" {
			apiError(w, http.StatusBadRequest, "invalid_parameter", "corpus必须唯一且为core或all。")
			return
		}
		id = values[0]
	}
	target, ok := s.servers[id]
	if !ok {
		apiError(w, http.StatusBadRequest, "invalid_parameter", "未知或未提供的语料版本。")
		return
	}
	target.ServeHTTP(w, r)
}
func selectCorpus(r *http.Request, id string) (*http.Request, error) {
	values, exists := r.URL.Query()["corpus"]
	if !exists {
		return r, nil
	}
	if len(values) != 1 || values[0] != id {
		return nil, errors.New("未知、重复或不匹配的corpus参数")
	}
	clone := r.Clone(r.Context())
	q := clone.URL.Query()
	q.Del("corpus")
	clone.URL.RawQuery = q.Encode()
	return clone, nil
}
