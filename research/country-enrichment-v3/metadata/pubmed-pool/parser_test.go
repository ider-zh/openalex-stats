package main

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestIdentifiersAndAffiliationsUseOnlyArticlePaths(t *testing.T) {
	aliases = map[string]string{"china": "CN", "united states": "US", "republic of korea": "KR"}
	raw := `<PubmedArticle><MedlineCitation><PMID Version="1">123</PMID><Article><ArticleTitle>Correct <i>paper</i> title</ArticleTitle><Journal><JournalIssue><PubDate><Year>2020</Year></PubDate></JournalIssue></Journal><AuthorList><Author><LastName>Li</LastName><AffiliationInfo><Affiliation>Department, University, China. Electronic address: private@example.org.</Affiliation></AffiliationInfo></Author></AuthorList></Article><MedlineJournalInfo><Country>United States</Country></MedlineJournalInfo></MedlineCitation><PubmedData><ArticleIdList><ArticleId IdType="doi">10.1000/correct</ArticleId></ArticleIdList><ReferenceList><Reference><ArticleIdList><ArticleId IdType="doi">10.1000/wrong-reference</ArticleId></ArticleIdList></Reference></ReferenceList></PubmedData></PubmedArticle>`
	var a Article
	if e := xml.Unmarshal([]byte(raw), &a); e != nil {
		t.Fatal(e)
	}
	r := convert(a)
	if r.DOI != "10.1000/correct" || r.PMID != "123" || len(r.Countries) != 1 || r.Countries[0] != "CN" {
		t.Fatalf("wrong structural fields: %+v", r)
	}
	if r.Title != "Correct paper title" {
		t.Fatal(r.Title)
	}
	if !strings.Contains(r.Authors[0].Affiliations[0].Text, "private@example.org") {
		t.Fatal("private source payload altered before policy evaluation")
	}
}
