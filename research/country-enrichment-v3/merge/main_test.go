package main

import "testing"

func TestGateBoundToSourceAndDomain(t *testing.T) {
	for _, x := range [][4]string{{"pubmed_baseline", "pubmed_baseline", "v1_strict_text_only", "passed"}, {"pmc_jats", "v1_strict_text_only", "v1_strict_text_only", "passed"}, {"crossref", "crossref/net_new", "crossref/net_new", "insufficient_n"}} {
		if e := gateBinding(x[0], x[1], x[2], x[3]); e == nil {
			t.Fatalf("misbound gate accepted: %v", x)
		}
	}
	if e := gateBinding("crossref", "crossref/net_new", "crossref/net_new", "passed"); e != nil {
		t.Fatal(e)
	}
}

func TestUnionPartialSourcesAndIdentity(t *testing.T) {
	m := map[uint64]*merged{}
	v := map[string]bool{"CN": true, "US": true}
	a := source{ID: 1, Year: 1995, Countries: []string{"CN"}, Route: "text"}
	if e := add(m, a, "text", v); e != nil {
		t.Fatal(e)
	}
	a.Countries[0] = "US" // reader buffer reuse must not mutate stored labels
	if m[1].Countries[0] != "CN" {
		t.Fatal("aliased caller slice")
	}
	b := source{ID: 1, Year: 1995, Countries: []string{"US"}, Route: "pubmed"}
	if e := add(m, b, "pubmed", v); e != nil {
		t.Fatal(e)
	}
	if !equal(m[1].Countries, []string{"CN", "US"}) || !m[1].Divergence {
		t.Fatal("lost supported complementary source set")
	}
	if e := add(m, b, "pubmed", v); e == nil {
		t.Fatal("duplicate source accepted")
	}
	b.Route = "pmc"
	b.Year = 1996
	if e := add(m, b, "pmc", v); e == nil {
		t.Fatal("identity conflict accepted")
	}
}

func TestRejectUnsafePatch(t *testing.T) {
	v := map[string]bool{"CN": true, "UNKNOWN": true}
	for _, r := range []source{{ID: 1, Route: "a", Original: []string{"CN"}, Countries: []string{"CN"}}, {ID: 1, Route: "a", Countries: []string{"CN", "CN"}}, {ID: 1, Route: "a", Countries: []string{"UNKNOWN"}}, {ID: 1, Route: "a", Countries: []string{"ZZ"}}, {ID: 1, Route: "a"}, {ID: 1, Route: "wrong", Countries: []string{"CN"}}} {
		if e := add(map[uint64]*merged{}, r, "a", v); e == nil {
			t.Fatalf("unsafe input accepted: %+v", r)
		}
	}
}
