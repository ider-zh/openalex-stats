package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func rulesForTest(t *testing.T) Rules {
	t.Helper()
	p := filepath.Join(t.TempDir(), "dictionary.json")
	data := `{"countries":[{"id":"US","display_name":"United States of America"},{"id":"GB","display_name":"United Kingdom"},{"id":"KR","display_name":"South Korea"},{"id":"KP","display_name":"North Korea"},{"id":"CN","display_name":"China"},{"id":"RU","display_name":"Russia"},{"id":"IR","display_name":"Iran"},{"id":"TW","display_name":"Taiwan"},{"id":"HK","display_name":"Hong Kong"},{"id":"MO","display_name":"Macao"},{"id":"VN","display_name":"Vietnam"},{"id":"CZ","display_name":"Czechia"},{"id":"TR","display_name":"Turkey"},{"id":"SY","display_name":"Syria"},{"id":"BO","display_name":"Bolivia"},{"id":"VE","display_name":"Venezuela"},{"id":"TZ","display_name":"Tanzania"},{"id":"LA","display_name":"Laos"},{"id":"MD","display_name":"Moldova"},{"id":"CD","display_name":"Democratic Republic of the Congo"},{"id":"CG","display_name":"Republic of the Congo"},{"id":"DE","display_name":"Germany"},{"id":"ES","display_name":"Spain"},{"id":"BR","display_name":"Brazil"},{"id":"IT","display_name":"Italy"},{"id":"JP","display_name":"Japan"},{"id":"PL","display_name":"Poland"},{"id":"PT","display_name":"Portugal"},{"id":"MX","display_name":"Mexico"},{"id":"GR","display_name":"Greece"},{"id":"FR","display_name":"France"},{"id":"SS","display_name":"South Sudan"},{"id":"SD","display_name":"Sudan"},{"id":"PG","display_name":"Papua New Guinea"},{"id":"GN","display_name":"Guinea"},{"id":"GE","display_name":"Georgia"}]}`
	if e := os.WriteFile(p, []byte(data), 0600); e != nil {
		t.Fatal(e)
	}
	r, e := loadRules(p)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestExactCountryAddressRules(t *testing.T) {
	r := rulesForTest(t)
	cases := []struct {
		s    string
		want []string
		amb  bool
	}{
		{"Department, Juba, South Sudan", []string{"SS"}, false},
		{"Institute, Port Moresby, Papua New Guinea", []string{"PG"}, false},
		{"University of South Africa, Campus, New York", nil, false},
		{"Department, Delhi, IN", nil, false},
		{"Department, California, CA", nil, false},
		{"Department, US", nil, false},
		{"Department, Georgia", nil, true},
		{"Department, Paris, France.", []string{"FR"}, false},
		{"Department, Boston, U.S.A.", []string{"US"}, false},
		{"Department, Taiwan, China", []string{"TW", "CN"}, true},
		{"Department, France, Email user@example.com", nil, false},
		{"Department, Paris, FRANCE", []string{"FR"}, false},
	}
	for _, c := range cases {
		hits, amb := r.textHits(c.s)
		var got []string
		for _, h := range hits {
			got = append(got, h.Country)
		}
		if !reflect.DeepEqual(got, c.want) || amb != c.amb {
			t.Errorf("%q: %v/%v want %v/%v", c.s, got, amb, c.want, c.amb)
		}
	}
}
func sp(s string) *string { return &s }
func TestEnrichmentPreservesBaselineAndSeparatesConflicts(t *testing.T) {
	e := Env{rules: rulesForTest(t), institutions: map[string][]string{"I1": {"FR"}, "I2": {"US"}}}
	cases := []struct {
		name                         string
		r                            RawWork
		old                          []string
		added                        []string
		newKnown, extended, conflict uint64
	}{
		{"lookup unknown", RawWork{Authors: []*Authorship{{Affiliations: []*Affiliation{{IDs: []*string{sp("I1")}}}}}}, nil, []string{"FR"}, 1, 0, 0},
		{"text unknown", RawWork{Authors: []*Authorship{{Raw: []*string{sp("Department, Paris, France")}}}}, nil, []string{"FR"}, 1, 0, 0},
		{"new country on another author", RawWork{Authors: []*Authorship{{Countries: []*string{sp("CN")}}, {Institutions: []*Institution{{ID: sp("I1")}}}}}, []string{"CN"}, []string{"FR"}, 0, 1, 0},
		{"contradicting labelled author", RawWork{Authors: []*Authorship{{Countries: []*string{sp("CN")}, Institutions: []*Institution{{ID: sp("I1")}}}}}, []string{"CN"}, nil, 0, 0, 1},
		{"embedded and lookup conflict", RawWork{Authors: []*Authorship{{Institutions: []*Institution{{ID: sp("I2"), Country: sp("FR")}}}}}, nil, nil, 0, 0, 1},
	}
	for _, c := range cases {
		p, s, _ := e.evaluate(c.r, 1, 2020, c.old)
		if p == nil {
			t.Fatalf("%s: missing review patch", c.name)
		}
		if !reflect.DeepEqual(p.Added, c.added) || s.NewCombined != c.newKnown || s.KnownExtended != c.extended || s.Conflicts != c.conflict {
			t.Errorf("%s %+v %+v", c.name, p, s)
		}
		for _, v := range c.old {
			if !contains(p.Conservative, v) {
				t.Errorf("removed original country %s", v)
			}
		}
	}
}
