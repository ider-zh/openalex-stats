package main

import (
	"bytes"
	"encoding/xml"
	"io"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

type Node struct {
	Name string
	Attr map[string]string
	Body strings.Builder
	Kids []*Node
}

func parseTree(b []byte) (*Node, error) {
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = false
	root := &Node{Name: "document"}
	stack := []*Node{root}
	for {
		t, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		switch x := t.(type) {
		case xml.StartElement:
			n := &Node{Name: x.Name.Local, Attr: map[string]string{}}
			for _, a := range x.Attr {
				n.Attr[a.Name.Local] = a.Value
			}
			p := stack[len(stack)-1]
			p.Kids = append(p.Kids, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			for _, n := range stack {
				n.Body.Write(x)
			}
		}
	}
	return root, nil
}
func (n *Node) child(name string) *Node {
	if n == nil {
		return nil
	}
	for _, c := range n.Kids {
		if c.Name == name {
			return c
		}
	}
	return nil
}
func (n *Node) all(name string) (out []*Node) {
	if n == nil {
		return nil
	}
	if n.Name == name {
		out = append(out, n)
	}
	for _, c := range n.Kids {
		out = append(out, c.all(name)...)
	}
	return
}
func first(ns []*Node) *Node {
	if len(ns) == 0 {
		return nil
	}
	return ns[0]
}
func text(n *Node) string {
	if n == nil {
		return ""
	}
	return strings.Join(strings.Fields(n.Body.String()), " ")
}

var emailPattern = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)

func clean(s string) string {
	return emailPattern.ReplaceAllString(strings.Join(strings.Fields(s), " "), "[email removed]")
}

type Affiliation struct {
	ID         string            `json:"id"`
	AuthorRefs []string          `json:"author_refs"`
	Text       string            `json:"text"`
	Countries  []string          `json:"countries"`
	Evidence   []CountryEvidence `json:"evidence"`
	Linked     bool              `json:"linked"`
	Ambiguous  bool              `json:"ambiguous"`
}
type CountryEvidence struct {
	Country string `json:"country"`
	Text    string `json:"text"`
	Key     string `json:"structured_key,omitempty"`
	Rule    string `json:"rule"`
}
type Parsed struct {
	Format            string        `json:"format"`
	Title             string        `json:"title"`
	AlternativeTitles []string      `json:"alternative_titles,omitempty"`
	DOI               string        `json:"doi"`
	PMID              string        `json:"pmid,omitempty"`
	PMCID             string        `json:"pmcid,omitempty"`
	Year              string        `json:"year"`
	Authors           []string      `json:"authors"`
	Affiliations      []Affiliation `json:"affiliations"`
	Countries         []string      `json:"countries"`
	License           []string      `json:"license,omitempty"`
}

func (r Rules) aff(n *Node, id string, authors []string, linked bool) Affiliation {
	a := Affiliation{ID: id, AuthorRefs: authors, Text: clean(text(n)), Linked: linked}
	for _, c := range n.all("country") {
		key := strings.ToUpper(strings.TrimSpace(c.Attr["country"]))
		if key == "" {
			key = strings.ToUpper(strings.TrimSpace(c.Attr["key"]))
		}
		named := r.alias[normalizeSegment(text(c))]
		code := named
		if r.valid[key] {
			code = key
		}
		if code == "" {
			a.Ambiguous = true
			continue
		}
		if r.valid[key] && named != "" && key != named {
			a.Ambiguous = true
			continue
		}
		a.Countries = addUnique(a.Countries, code)
		a.Evidence = append(a.Evidence, CountryEvidence{code, text(c), key, "source_affiliation_country_element"})
	}
	if len(n.all("country")) == 0 {
		hits, amb := r.textHits(a.Text)
		a.Ambiguous = amb
		for _, h := range hits {
			a.Countries = addUnique(a.Countries, h.Country)
			a.Evidence = append(a.Evidence, CountryEvidence{Country: h.Country, Text: h.Segment, Rule: h.Rule})
		}
	}
	sort.Strings(a.Countries)
	return a
}
func parseJATS(b []byte, r Rules) (Parsed, error) {
	root, e := parseTree(b)
	if e != nil {
		return Parsed{}, e
	}
	article := first(root.all("article"))
	meta := article.child("front").child("article-meta")
	p := Parsed{Format: "JATS", Title: text(first(meta.all("article-title")))}
	for _, title := range meta.all("trans-title") {
		if s := text(title); s != "" {
			p.AlternativeTitles = append(p.AlternativeTitles, s)
		}
	}
	if meta == nil {
		return p, nil
	}
	for _, id := range meta.all("article-id") {
		switch strings.ToLower(id.Attr["pub-id-type"]) {
		case "doi":
			p.DOI = text(id)
		case "pmid":
			p.PMID = text(id)
		case "pmc", "pmcid":
			p.PMCID = text(id)
		}
	}
	for _, date := range meta.all("pub-date") {
		y := text(date.child("year"))
		if y != "" {
			p.Year = y
			break
		}
	}
	for _, lic := range meta.all("license") {
		p.License = append(p.License, clean(text(lic)))
	}
	refs := map[string][]string{}
	inline := []Affiliation{}
	authorIndex := 0
	for _, c := range meta.all("contrib") {
		typ := c.Attr["contrib-type"]
		if typ != "" && typ != "author" {
			continue
		}
		name := text(c.child("name"))
		if n := c.child("name"); n != nil {
			if given, family := text(n.child("given-names")), text(n.child("surname")); given != "" && family != "" {
				name = given + " " + family
			}
		}
		if name == "" {
			name = text(c.child("collab"))
		}
		p.Authors = append(p.Authors, name)
		who := name
		if who == "" {
			who = "author#" + string(rune('A'+authorIndex))
		}
		authorIndex++
		for _, x := range c.all("xref") {
			if x.Attr["ref-type"] == "aff" {
				for _, rid := range strings.Fields(x.Attr["rid"]) {
					refs[rid] = addUnique(refs[rid], who)
				}
			}
		}
		for _, a := range c.all("aff") {
			inline = append(inline, r.aff(a, a.Attr["id"], []string{who}, true))
		}
	}
	for _, a := range meta.all("aff") {
		id := a.Attr["id"]
		names := refs[id]
		linked := len(names) > 0
		// Frozen v3 requires explicit byline xref or nested affiliation linkage.
		af := r.aff(a, id, names, linked)
		for _, v := range inline {
			if v.Text == af.Text {
				af = v
				break
			}
		}
		p.Affiliations = append(p.Affiliations, af)
	}
	for _, a := range p.Affiliations {
		if a.Linked && !a.Ambiguous {
			for _, c := range a.Countries {
				p.Countries = addUnique(p.Countries, c)
			}
		}
	}
	sort.Strings(p.Countries)
	return p, nil
}

func withXMLCountryAliases(r Rules) Rules { r.alias["uk"] = "GB"; r.alias["u.k"] = "GB"; return r }
func parseTEI(b []byte, r Rules) (Parsed, error) {
	root, e := parseTree(b)
	if e != nil {
		return Parsed{}, e
	}
	header := first(root.all("teiHeader"))
	file := header.child("fileDesc")
	bib := first(file.child("sourceDesc").all("biblStruct"))
	analytic := bib.child("analytic")
	p := Parsed{Format: "TEI", Title: text(first(file.child("titleStmt").all("title")))}
	if p.Title == "" {
		p.Title = text(first(analytic.all("title")))
	}
	for _, id := range bib.all("idno") {
		if strings.EqualFold(id.Attr["type"], "DOI") {
			p.DOI = text(id)
			break
		}
	}
	for _, date := range bib.all("date") {
		when := date.Attr["when"]
		if len(when) >= 4 {
			p.Year = when[:4]
			break
		}
	}
	for i, author := range analytic.all("author") {
		name := text(author.child("persName"))
		p.Authors = append(p.Authors, name)
		for _, a := range author.all("affiliation") {
			p.Affiliations = append(p.Affiliations, r.aff(a, "tei-author-"+string(rune('A'+i)), []string{name}, true))
		}
	}
	for _, a := range p.Affiliations {
		if a.Linked && !a.Ambiguous {
			for _, c := range a.Countries {
				p.Countries = addUnique(p.Countries, c)
			}
		}
	}
	sort.Strings(p.Countries)
	return p, nil
}
func normalizedTitle(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}
func normalizeDOI(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "doi:"} {
		s = strings.TrimPrefix(s, prefix)
	}
	return strings.TrimSpace(s)
}
