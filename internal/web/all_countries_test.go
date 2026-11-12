package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The added dictionary labels deliberately have no model activity: requesting
// every known country must preserve those null curves, without inventing rank.
func TestECIAllKnownCountriesWithoutTruncation(t *testing.T) {
	root := fixture(t, false)
	b, err := os.ReadFile(filepath.Join(root, "dictionary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var dict Dictionary
	if err = json.Unmarshal(b, &dict); err != nil {
		t.Fatal(err)
	}
	ids := []string{"CN", "US", "GB"}
	for a := 'A'; a <= 'D'; a++ {
		for z := 'A'; z <= 'Z'; z++ {
			id := string([]rune{a, z})
			if id == "CN" {
				continue
			}
			dict.Countries = append(dict.Countries, Label{ID: id, DisplayName: id})
			ids = append(ids, id)
		}
	}
	jsonFile(t, root, "dictionary.json", dict)
	h, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	p := params("eci")
	p.Set("countries", strings.Join(append(ids, "CN", "US"), ","))
	p.Set("end", "2000")
	r := series(t, h, p)
	if len(r.Series) != len(ids) || len(r.Query.Countries) != len(ids) {
		t.Fatalf("all-country selection truncated: got %d want %d", len(r.Series), len(ids))
	}
	single := params("eci")
	single.Set("countries", "CN")
	single.Set("end", "2000")
	if !reflect.DeepEqual(r.Series[0].Points, series(t, h, single).Series[0].Points) {
		t.Fatal("country selection changed complete model rank")
	}
	last := r.Series[len(r.Series)-1].Points[0]
	if last.Value != nil || last.Rank != nil || last.Status != "inactive" {
		t.Fatal("inactive country given a fabricated score/rank", last)
	}
	for _, metric := range []string{"publications", "citations", "share", "rca"} {
		p.Set("metric", metric)
		if w := request(t, h, "/api/series?"+p.Encode()); w.Code != 400 {
			t.Fatal("ordinary metric accepted over 12 countries", metric, w.Code)
		}
	}
	p.Set("metric", "eci")
	p.Set("countries", strings.Join(ids, ",")+",ZZ")
	if w := request(t, h, "/api/series?"+p.Encode()); w.Code != 400 || !strings.Contains(w.Body.String(), "ZZ") {
		t.Fatal("unknown country accepted", w.Body.String())
	}
	var meta struct {
		MaxECICountries int     `json:"max_eci_countries"`
		MaxSeries       int     `json:"max_series"`
		Countries       []Label `json:"countries"`
	}
	w := request(t, h, "/api/meta")
	if err = json.Unmarshal(w.Body.Bytes(), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.MaxECICountries != len(meta.Countries) || meta.MaxECICountries <= 48 || meta.MaxSeries != 48 {
		t.Fatal("metric-specific limits missing", meta)
	}
}
