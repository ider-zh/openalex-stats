package main

import "testing"

func testRules() Rules {
	return Rules{valid: map[string]bool{"US": true, "GB": true, "KP": true, "CN": true}, alias: map[string]string{"usa": "US", "united states": "US", "united kingdom": "GB", "china": "CN", "north korea": "KP"}}
}
func TestJATSOnlyLinkedAuthorAffiliations(t *testing.T) {
	b := []byte(`<article><front><journal-meta><publisher><country>North Korea</country></publisher></journal-meta><article-meta><article-id pub-id-type="doi">10.123/a</article-id><title-group><article-title>A real article</article-title></title-group><pub-date><year>2020</year></pub-date><contrib-group><contrib contrib-type="author"><name><surname>Person</surname></name><xref ref-type="aff" rid="a1"/></contrib></contrib-group><aff id="a1">Dept, <country country="US">USA</country></aff><aff id="a2">Editor, <country>China</country></aff></article-meta></front><back><ref-list><ref><country>North Korea</country></ref></ref-list></back></article>`)
	p, e := parseJATS(b, testRules())
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Countries) != 1 || p.Countries[0] != "US" {
		t.Fatalf("body/publisher/unlinked leaked: %+v", p)
	}
}
func TestTEIHeaderNotReferencesAndConflictingCountry(t *testing.T) {
	b := []byte(`<TEI><teiHeader><fileDesc><titleStmt><title>Example title</title></titleStmt><sourceDesc><biblStruct><analytic><author><persName>Person</persName><affiliation><orgName>China Institute</orgName><address><country key="US">United Kingdom</country></address></affiliation></author><idno type="DOI">10.12/example</idno></analytic></biblStruct></sourceDesc></fileDesc></teiHeader><text><back><listBibl><biblStruct><author><affiliation><country>North Korea</country></affiliation></author></biblStruct></listBibl></back></text></TEI>`)
	p, e := parseTEI(b, testRules())
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Countries) != 0 || !p.Affiliations[0].Ambiguous {
		t.Fatalf("conflict or references accepted: %+v", p)
	}
}
func TestIdentifierConflictAndSecondStageWeights(t *testing.T) {
	w := Work{DOI: "10.1007/s11356-014-2541-4", Title: "原始护士研究", Year: 2011}
	p := Parsed{DOI: "10.1007/s11356-014-2541-4", Title: "Environmental treatment", Year: "2014"}
	if pass, _, _ := identity(w, p); pass {
		t.Fatal("exact DOI must not hide title/year conflict")
	}
	s := Sample{Works: []Work{{ID: 1, Stratum: "a", Locations: []Location{{IsOA: true, PDF: "https://example.org/a.pdf"}}}, {ID: 2, Stratum: "a", Locations: []Location{{IsOA: true, PDF: "https://example.org/b.pdf"}}}, {ID: 3, Stratum: "a", Locations: []Location{{IsOA: true, PDF: "https://example.org/c.pdf"}}}}}
	plan := preparePDF(s, []Outcome{{ID: 1, IdentityPass: true, CandidateCountries: []string{"US"}}}, 1)
	if len(plan.PDF) != 1 || plan.PDF[0].ID == 1 || plan.PDF[0].PI2 != .5 || plan.PDFStrata[0].Eligible != 2 {
		t.Fatalf("bad conditional frame/weights: %+v", plan)
	}
}

func TestSourceTranslationAndExplicitUKByline(t *testing.T) {
	b := []byte(`<article><front><article-meta><article-id pub-id-type="doi">10.1/source</article-id><title-group><article-title>中文原始题名</article-title><trans-title-group><trans-title>Vascular access in long term clinical treatment</trans-title></trans-title-group></title-group><pub-date><year>2013</year></pub-date><contrib-group><contrib contrib-type="author"><name><surname>Person</surname></name><xref ref-type="aff" rid="a1"/></contrib></contrib-group><aff id="a1">Department, London, UK</aff></article-meta></front></article>`)
	p, e := parseJATS(b, withXMLCountryAliases(testRules()))
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Countries) != 1 || p.Countries[0] != "GB" {
		t.Fatal("explicit UK tail lost", p)
	}
	w := Work{Title: "Vascular access in long term clinical treatment", DOI: "10.1/source", Year: 2013}
	ok, rule, score := identity(w, p)
	if !ok || rule != "doi_plus_source_trans_title_year" || score != 1 {
		t.Fatal("source translation not used", ok, rule, score)
	}
}

func TestV3MissingDOIRequiresFullOriginalAuthorAndYear(t *testing.T) {
	w := Work{Title: "Combined single-molecule force and fluorescence measurements for biology", DOI: "10.1186/example", Year: 2003}
	p := Parsed{Title: w.Title, Year: "2003", Authors: []string{"MarkIWallace"}}
	var a OriginalAuthors
	if rule, _ := v3Identity(w, p, a); rule != "title_year_only_review" {
		t.Fatal(rule)
	}
	a.Verified = true
	a.Loaded = true
	// Distinctive complete names match across adjacent TEI name tags, initials do not.
	a.Authors = append(a.Authors, struct {
		Name    string `json:"name"`
		RawName string `json:"raw_author_name"`
		ORCID   string `json:"orcid"`
	}{Name: "Mark I Wallace"})
	if rule, m := v3Identity(w, p, a); rule != "exact_long_title_year_original_full_author" || len(m) != 1 {
		t.Fatal(rule, m)
	}
	p.DOI = "10.1186/conflict"
	if rule, _ := v3Identity(w, p, a); rule != "doi_conflict" {
		t.Fatal(rule)
	}
	p.DOI = ""
	p.Year = "2018"
	if rule, _ := v3Identity(w, p, a); rule != "missing_or_conflicting_year" {
		t.Fatal(rule)
	}
	if distinctiveName("M I Wallace") || longDistinctiveTitle("Introduction") {
		t.Fatal("generic title or initials accepted")
	}
}
