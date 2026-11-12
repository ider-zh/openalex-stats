package web

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDisciplineCartesianProductAndModelSemantics(t *testing.T) {
	h, err := New(fixture(t, false))
	if err != nil {
		t.Fatal(err)
	}
	p := params("publications")
	p.Del("discipline")
	p.Set("disciplines", "f1,f2,f1")
	p.Set("end", "2000")
	r := series(t, h, p)
	if len(r.Series) != 4 || !reflect.DeepEqual(r.Query.Disciplines, []string{"f1", "f2"}) {
		t.Fatal("not a deduplicated 2x2 product", r)
	}
	for i, want := range []float64{2, 2, 6, 0} {
		closeValue(t, r.Series[i].Points[0].Value, want)
	}
	if r.Series[0].Country != "CN" || r.Series[1].Discipline != "f2" || r.Series[0].ID == r.Series[1].ID || r.Series[0].DisciplineName != "生物学" {
		t.Fatal("lost series dimensions", r.Series)
	}
	for _, metric := range []string{"citations", "share", "rca"} {
		p.Set("metric", metric)
		multi := series(t, h, p)
		for _, id := range []string{"f1", "f2"} {
			single := params(metric)
			single.Set("discipline", id)
			single.Set("end", "2000")
			one := series(t, h, single)
			for _, s := range multi.Series {
				if s.Discipline == id {
					for _, o := range one.Series {
						if s.Country == o.Country && !reflect.DeepEqual(s.Points, o.Points) {
							t.Fatalf("%s denominator/values changed with selection", metric)
						}
					}
				}
			}
		}
	}
	p.Set("metric", "eci")
	eci := series(t, h, p)
	if len(eci.Series) != 2 || eci.Series[0].ID != "CN" || eci.Series[0].Discipline != "" {
		t.Fatal("ECI incorrectly repeated by subject", eci)
	}
	closeValue(t, eci.Series[0].Points[0].Value, 1.2)
	p.Set("metric", "pci")
	pci := series(t, h, p)
	if len(pci.Series) != 2 || pci.Series[0].Country != "GLOBAL" || pci.Series[0].ID == pci.Series[1].ID {
		t.Fatal("PCI incorrectly repeated by country", pci)
	}
	closeValue(t, pci.Series[0].Points[0].Value, .7)
	closeValue(t, pci.Series[1].Points[0].Value, -.7)
	if *pci.Series[0].Points[0].Rank != 1 || *pci.Series[1].Points[0].Rank != 2 {
		t.Fatal("PCI ranks not global", pci)
	}
	p.Set("metric", "publications")
	w := request(t, h, "/api/series.csv?"+p.Encode())
	rows, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 || rows[1][3] != "f1" || rows[2][3] != "f2" || rows[1][6] != "CN" || rows[3][6] != "US" || rows[1][14] != r.Series[0].ID || rows[2][16] != "物理学" {
		t.Fatal("CSV lost per-curve dimensions", rows)
	}
	// Country and subject order may change without changing any pair's identity.
	p.Set("disciplines", "f2,f1")
	p.Set("countries", "US,CN")
	reordered := series(t, h, p)
	if reordered.Series[3].ID != r.Series[0].ID {
		t.Fatal("curve identity depends on selection order")
	}
}

func TestDisciplineValidationAndCurveLimit(t *testing.T) {
	root := fixture(t, false)
	b, err := os.ReadFile(filepath.Join(root, "dictionary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var dict Dictionary
	if err = json.Unmarshal(b, &dict); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q"} {
		dict.Layers[0].Labels = append(dict.Layers[0].Labels, Label{ID: id, DisplayName: id})
	}
	jsonFile(t, root, "dictionary.json", dict)
	h, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "f1,", "f1,s1", "unknown"} {
		p := params("publications")
		p.Del("discipline")
		p.Set("disciplines", value)
		if w := request(t, h, "/api/series?"+p.Encode()); w.Code != 400 {
			t.Fatalf("accepted %q", value)
		}
	}
	p := params("publications")
	p.Set("disciplines", "f1")
	if w := request(t, h, "/api/series?"+p.Encode()); w.Code != 400 {
		t.Fatal("ambiguous old/new parameters accepted")
	}
	p.Del("discipline")
	p.Set("disciplines", "a,b,c,d,e,f,g,h,i,j,k,l,m,n,o,p,q")
	p.Set("countries", "CN,US,GB")
	if w := request(t, h, "/api/series?"+p.Encode()); w.Code != 400 || !strings.Contains(w.Body.String(), "51") {
		t.Fatal("51 curves accepted/truncated", w.Body.String())
	}
	p.Set("disciplines", "a,b,c,d,e,f,g,h,i,j,k,l,m,n,o,p")
	if got := len(series(t, h, p).Series); got != 48 {
		t.Fatal("48 valid curves unavailable", got)
	}
	legacy := series(t, h, params("publications"))
	if legacy.Series[0].ID != "CN" || legacy.Query.Discipline != "f1" {
		t.Fatal("old URLs/IDs changed")
	}
}

func TestEmbeddedFlagsHaveConstrainedRoutes(t *testing.T) {
	h, err := New(fixture(t, false))
	if err != nil {
		t.Fatal(err)
	}
	w := request(t, h, "/static/flags/cn.svg")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/svg+xml" || !strings.Contains(w.Body.String(), "<svg") {
		t.Fatal("embedded flag missing", w.Code)
	}
	for _, path := range []string{"/static/flags/zz.svg", "/static/flags/CN.svg", "/static/flags/../../run.json", "/static/flags/LICENSE", "/static/flags/cn.svg/extra"} {
		if w := request(t, h, path); w.Code != 404 {
			t.Fatal("unconstrained flag path", path, w.Code)
		}
	}
}
