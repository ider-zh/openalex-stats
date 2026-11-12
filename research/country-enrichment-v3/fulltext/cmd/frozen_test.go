package main

import "testing"

func original(name string) OriginalAuthors {
	a := OriginalAuthors{ID: 1, Verified: true, Loaded: true}
	a.Authors = append(a.Authors, struct {
		Name    string `json:"name"`
		RawName string `json:"raw_author_name"`
		ORCID   string `json:"orcid"`
	}{Name: name})
	return a
}
func TestFrozenIdentityNeverDOIOnlyAndRequiresAuthor(t *testing.T) {
	w := Work{ID: 1, DOI: "10.123/test", PMID: "1234", Title: "Long distinctive original title about molecular mechanisms", Year: 2020}
	p := Parsed{DOI: w.DOI, PMID: w.PMID, Title: "Unrelated long distinctive title about economic systems", Year: "2020", Authors: []string{"Jane Smith"}}
	if pass, _, _ := frozenIdentity(w, p, original("Jane Smith"), true); pass {
		t.Fatal("DOI bypassed title conflict")
	}
	p.Title = w.Title
	if pass, _, _ := frozenIdentity(w, p, OriginalAuthors{}, true); pass {
		t.Fatal("missing original author accepted")
	}
	p.PMID = "4321"
	if pass, rule, _ := frozenIdentity(w, p, original("Jane Smith"), true); pass || rule != "pmid_conflict" {
		t.Fatal("PMID conflict bypassed", rule)
	}
}
func TestFrozenNameOrderTranslationAndPublicationVersion(t *testing.T) {
	w := Work{ID: 1, Title: "Long distinctive original title about molecular mechanisms", Year: 2020}
	p := Parsed{Title: "原始非英文题名", AlternativeTitles: []string{w.Title}, Year: "2021", Authors: []string{"Smith Jane"}}
	if pass, _, m := frozenIdentity(w, p, original("Jane Smith"), true); !pass || len(m) != 1 {
		t.Fatal("published online/print or source translation lost")
	}
	if pass, rule, _ := frozenIdentity(w, p, original("Jane Smith"), false); pass || rule != "nonpublished_version_year_changed" {
		t.Fatal("later manuscript bypassed year", rule)
	}
	if fullName("J Smith") || fullName("Smith") || !fullName("Hui Li") {
		t.Fatal("full name rules changed")
	}
}
func TestFrozenJATSExplicitAssociationAndCanonicalName(t *testing.T) {
	b := []byte(`<article><front><article-meta><title-group><article-title>Long distinctive original title about molecular mechanisms</article-title></title-group><pub-date><year>2020</year></pub-date><contrib-group><contrib contrib-type="author"><name><surname>Smith</surname><given-names>Jane</given-names></name></contrib></contrib-group><aff id="a1">Publisher, <country>China</country></aff></article-meta></front></article>`)
	p, e := parseJATS(b, testRules())
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Countries) != 0 || len(p.Authors) != 1 || p.Authors[0] != "Jane Smith" {
		t.Fatal("unlinked sole affiliation or name order", p)
	}
}
