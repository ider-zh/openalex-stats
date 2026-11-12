package web

import (
	"fmt"
	"net/http"
)

// CountryVersionInfo describes country attribution, independently of corpus or
// complexity methodology. The enriched version only fills originally UNKNOWN
// countries; it does not claim to repair all pre-existing country assignments.
type CountryVersionInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Version     string `json:"version"`
}

func countryVersionInfo(id string) CountryVersionInfo {
	if id == "enriched" {
		return CountryVersionInfo{ID: id, DisplayName: "国家补全版", Version: "country-enriched-v3-r3"}
	}
	return CountryVersionInfo{ID: "original", DisplayName: "原始版", Version: "snapshot-20260626-original"}
}

func (s *server) version() string {
	if s.countryVersion == "" {
		return "original"
	}
	return s.countryVersion
}

type CountryVersionConfig struct {
	Datasets          map[string]string
	MethodDirectories map[string]string
}

type countryVersionServer struct {
	versions map[string]*catalogServer
	catalog  Catalog
}

// NewCountryVersionCatalog loads both immutable country versions and defaults
// to enriched. Request selectors can never supply a filesystem path.
func NewCountryVersionCatalog(original, enriched CountryVersionConfig) (http.Handler, error) {
	out := &countryVersionServer{versions: map[string]*catalogServer{}}
	for _, input := range []struct {
		id     string
		config CountryVersionConfig
	}{{"original", original}, {"enriched", enriched}} {
		h, err := NewCatalogWithMethods(input.config.Datasets, input.config.MethodDirectories)
		if err != nil {
			return nil, fmt.Errorf("web country version %s: %w", input.id, err)
		}
		c := h.(*catalogServer)
		for _, s := range c.servers {
			s.countryVersion = input.id
		}
		out.versions[input.id] = c
	}
	a, b := out.versions["original"], out.versions["enriched"]
	if len(a.servers) != len(b.servers) {
		return nil, fmt.Errorf("web: country versions must provide the same corpora")
	}
	for id, before := range a.servers {
		after := b.servers[id]
		if after == nil || before.data.run.SelectedWorks != after.data.run.SelectedWorks || before.data.snapshotDate != after.data.snapshotDate {
			return nil, fmt.Errorf("web: country version corpus %s must preserve selected works and snapshot", id)
		}
	}
	out.catalog = b.catalog
	out.catalog.DefaultCountryVersion = "enriched"
	out.catalog.CountryVersions = []CountryVersionInfo{countryVersionInfo("original"), countryVersionInfo("enriched")}
	return out, nil
}

func (s *countryVersionServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	if r.URL.Path == "/api/catalog" {
		serveCatalog(w, r, s.catalog)
		return
	}
	id := s.catalog.DefaultCountryVersion
	if v, ok := r.URL.Query()["country_version"]; ok {
		if len(v) != 1 || v[0] == "" {
			apiError(w, 400, "invalid_parameter", "country_version必须唯一且为original或enriched。")
			return
		}
		id = v[0]
	}
	target := s.versions[id]
	if target == nil {
		apiError(w, 400, "invalid_parameter", "未知或未提供的国家数据版本。")
		return
	}
	selected, err := selectCountryVersion(r, id)
	if err != nil {
		apiError(w, 400, "invalid_parameter", err.Error())
		return
	}
	target.ServeHTTP(w, selected)
}

func selectCountryVersion(r *http.Request, id string) (*http.Request, error) {
	v, ok := r.URL.Query()["country_version"]
	if !ok {
		return r, nil
	}
	if len(v) != 1 || v[0] != id {
		return nil, fmt.Errorf("未知、重复或不匹配的country_version参数")
	}
	clone := r.Clone(r.Context())
	q := clone.URL.Query()
	q.Del("country_version")
	clone.URL.RawQuery = q.Encode()
	return clone, nil
}
