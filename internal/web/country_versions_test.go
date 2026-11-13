package web

import (
	"encoding/csv"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/aggregate"
)

func TestCountryVersionIsolationAndDefault(t *testing.T) {
	core, all := catalogFixture(t)
	newCore, newAll := catalogFixture(t)
	for _, dir := range []string{newCore, newAll} {
		rows, err := parquet.ReadFile[aggregate.Row](filepath.Join(dir, "cube.parquet"))
		if err != nil {
			t.Fatal(err)
		}
		for i := range rows {
			r := &rows[i]
			if r.Year == 2000 && r.Scheme == "topics" && r.Level == "field" && r.DisciplineID == "f1" {
				if r.Country == "CN" {
					r.Publications++
					r.Citations += 2
				}
				if r.Country == aggregate.Unknown {
					r.Publications--
					r.Citations -= 2
				}
			}
		}
		parquetFixture(t, filepath.Join(dir, "cube.parquet"), rows)
		b, err := os.ReadFile(filepath.Join(dir, "eci.json"))
		if err != nil {
			t.Fatal(err)
		}
		var models []modelResult
		if err = json.Unmarshal(b, &models); err != nil {
			t.Fatal(err)
		}
		models[0].Result.ECI = []float64{-1.2, 1.2}
		jsonFile(t, dir, "eci.json", models)
	}
	a := CountryVersionConfig{Datasets: map[string]string{"core": core, "all": all}}
	b := CountryVersionConfig{Datasets: map[string]string{"core": newCore, "all": newAll}}
	h, err := NewCountryVersionCatalog(a, b)
	if err != nil {
		t.Fatal(err)
	}
	var catalog Catalog
	if err = json.Unmarshal(request(t, h, "/api/catalog").Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.DefaultCountryVersion != "enriched" || len(catalog.CountryVersions) != 2 || len(catalog.Datasets) != 2 {
		t.Fatal(catalog)
	}
	for _, corpus := range []string{"core", "all"} {
		for _, version := range []string{"original", "enriched"} {
			p := params("publications")
			p.Set("corpus", corpus)
			p.Set("country_version", version)
			want := 2.0
			if corpus == "all" {
				want = 6
			}
			if version == "enriched" {
				want++
			}
			r := series(t, h, p)
			closeValue(t, r.Series[0].Points[0].Value, want)
			if r.Query.CountryVersion != version || r.Query.Corpus != corpus {
				t.Fatal(r.Query)
			}
			w := request(t, h, "/api/meta?corpus="+corpus+"&country_version="+version)
			var meta struct {
				CountryVersion string             `json:"country_version"`
				Info           CountryVersionInfo `json:"country_version_info"`
			}
			if err = json.Unmarshal(w.Body.Bytes(), &meta); err != nil {
				t.Fatal(err)
			}
			if meta.CountryVersion != version || meta.Info.ID != version || w.Header().Get("X-OpenAlex-Country-Version") != version {
				t.Fatal(meta, w.Header())
			}
			w = request(t, h, "/api/series.csv?"+p.Encode())
			records, e := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
			if e != nil {
				t.Fatal(e)
			}
			versionColumn := -1
			for i, name := range records[0] {
				if name == "country_version" {
					versionColumn = i
				}
			}
			if versionColumn < 0 || records[1][versionColumn] != version {
				t.Fatal(records)
			}
			p.Set("metric", "eci")
			r = series(t, h, p)
			wantRank := 1
			if version == "enriched" {
				wantRank = 2
			}
			if r.Series[0].Points[0].Rank == nil || *r.Series[0].Points[0].Rank != wantRank {
				t.Fatal("models crossed versions", r)
			}
			w = request(t, h, "/download?file=run.json&corpus="+corpus+"&country_version="+version)
			if w.Code != 200 || w.Header().Get("X-OpenAlex-Country-Version") != version {
				t.Fatal(w.Code, w.Header())
			}
		}
	}
	closeValue(t, series(t, h, params("publications")).Series[0].Points[0].Value, 3)
	for _, url := range []string{"/api/meta?country_version=", "/api/meta?country_version=unknown", "/api/meta?country_version=original&country_version=enriched", "/api/catalog?country_version=enriched", "/download?country_version=../../&file=run.json"} {
		if w := request(t, h, url); w.Code != 400 {
			t.Fatal("invalid version accepted", url, w.Code)
		}
	}
	incoming := httptest.NewRequest("GET", "/api/meta?country_version=enriched&corpus=all", nil)
	h.ServeHTTP(httptest.NewRecorder(), incoming)
	if incoming.URL.RawQuery != "country_version=enriched&corpus=all" {
		t.Fatal("request mutated")
	}
	if _, err = NewCountryVersionCatalog(a, CountryVersionConfig{Datasets: map[string]string{"core": newCore}}); err == nil {
		t.Fatal("mismatched corpora accepted")
	}
	single, err := New(core)
	if err != nil {
		t.Fatal(err)
	}
	if request(t, single, "/api/meta?country_version=original").Code != 200 || request(t, single, "/api/meta?country_version=enriched").Code != 400 {
		t.Fatal("single version selector")
	}
}
