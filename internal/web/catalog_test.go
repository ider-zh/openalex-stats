package web

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/aggregate"
)

func catalogFixture(t *testing.T) (string, string) {
	t.Helper()
	core, all := fixture(t, false), fixture(t, false)
	var run map[string]any
	b, err := os.ReadFile(filepath.Join(all, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &run); err != nil {
		t.Fatal(err)
	}
	run["selected_works"] = 60
	run["config"].(map[string]any)["scope"].(map[string]any)["corpus"] = "all"
	jsonFile(t, all, "run.json", run)
	rows, err := parquet.ReadFile[aggregate.Row](filepath.Join(all, "cube.parquet"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		rows[i].Publications *= 3
		rows[i].Citations *= 4
	}
	parquetFixture(t, filepath.Join(all, "cube.parquet"), rows)
	return core, all
}
func TestCatalogDatasetIsolationAndSelectors(t *testing.T) {
	core, all := catalogFixture(t)
	handler, err := NewCatalog(map[string]string{"core": core, "all": all})
	if err != nil {
		t.Fatal(err)
	}
	var catalog Catalog
	w := request(t, handler, "/api/catalog")
	if err = json.Unmarshal(w.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.DefaultCorpus != "core" || len(catalog.Datasets) != 2 || catalog.Datasets[0].ID != "core" || catalog.Datasets[1].SelectedWorks != 60 {
		t.Fatal("bad catalog", catalog)
	}
	for _, corpus := range []string{"core", "all"} {
		p := params("publications")
		p.Set("corpus", corpus)
		r := series(t, handler, p)
		want := 2.0
		if corpus == "all" {
			want = 6
		}
		closeValue(t, r.Series[0].Points[0].Value, want)
		if r.Query.Corpus != corpus {
			t.Fatal("series corpus missing", r.Query)
		}
		w = request(t, handler, "/api/meta?corpus="+corpus)
		var meta struct {
			Corpus string `json:"corpus"`
			Scope  struct {
				Corpus string `json:"corpus"`
			} `json:"scope"`
		}
		if err = json.Unmarshal(w.Body.Bytes(), &meta); err != nil {
			t.Fatal(err)
		}
		if meta.Corpus != corpus || meta.Scope.Corpus != corpus {
			t.Fatal("scope crossed corpus", meta)
		}
		w = request(t, handler, "/api/series.csv?"+p.Encode())
		records, e := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
		if e != nil {
			t.Fatal(e)
		}
		if records[0][11] != "corpus" || records[1][11] != corpus || records[1][8] != map[string]string{"core": "2", "all": "6"}[corpus] {
			t.Fatal("CSV crossed corpus", records)
		}
		w = request(t, handler, "/download?file=run.json&corpus="+corpus)
		if w.Code != 200 || w.Header().Get("X-OpenAlex-Corpus") != corpus {
			t.Fatal("download identity wrong", w.Code, w.Header())
		}
		var run struct {
			SelectedWorks int `json:"selected_works"`
		}
		if err = json.Unmarshal(w.Body.Bytes(), &run); err != nil {
			t.Fatal(err)
		}
		if run.SelectedWorks != map[string]int{"core": 20, "all": 60}[corpus] {
			t.Fatal("download crossed directory", run)
		}
	}
	closeValue(t, series(t, handler, params("publications")).Series[0].Points[0].Value, 2)
	for _, path := range []string{"/api/meta?corpus=unknown", "/api/meta?corpus=", "/api/meta?corpus=core&corpus=all", "/api/series?corpus=../../", "/download?corpus=all&file=..%2Fcore%2Frun.json", "/api/catalog?extra=1"} {
		if w := request(t, handler, path); w.Code != http.StatusBadRequest {
			t.Fatal("invalid selector/path accepted", path, w.Code)
		}
	}
	single, err := New(all)
	if err != nil {
		t.Fatal(err)
	}
	p := params("publications")
	p.Set("corpus", "all")
	closeValue(t, series(t, single, p).Series[0].Points[0].Value, 6)
	if w := request(t, single, "/api/meta?corpus=core"); w.Code != 400 {
		t.Fatal("single corpus alias escaped", w.Code)
	}
	for _, config := range []map[string]string{{}, {"all": all}, {"core": core, "other": all}, {"core": core, "all": core}, {"core": t.TempDir()}} {
		if _, e := NewCatalog(config); e == nil {
			t.Fatal("invalid catalog accepted", config)
		}
	}
	// The incoming query is copied, never mutated into a different request.
	incoming := httptest.NewRequest(http.MethodGet, "/api/meta?corpus=all", nil)
	handler.ServeHTTP(httptest.NewRecorder(), incoming)
	if incoming.URL.RawQuery != "corpus=all" {
		t.Fatal("selector routing mutated caller request")
	}
}
