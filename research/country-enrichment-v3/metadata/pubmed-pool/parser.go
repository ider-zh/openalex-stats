package main
import("encoding/xml";"regexp";"strings";"sort";"strconv";"unicode")
var aliases map[string]string
type Text struct {
	Inner string `xml:",innerxml"`
}
type ArticleID struct {
	Kind  string `xml:"IdType,attr"`
	Value string `xml:",chardata"`
}
type Affiliation struct {
	Text string `xml:"Affiliation"`
}
type Author struct {
	Given       string `xml:"ForeName"`
	Family      string `xml:"LastName"`
	Collective  string `xml:"CollectiveName"`
	Identifiers []struct {
		Source string `xml:"Source,attr"`
		Value  string `xml:",chardata"`
	} `xml:"Identifier"`
	Affiliations []Affiliation `xml:"AffiliationInfo"`
}
type Article struct {
	Citation struct {
		PMID    string `xml:"PMID"`
		Article struct {
			Title             Text     `xml:"ArticleTitle"`
			Vernacular        Text     `xml:"VernacularTitle"`
			Authors           []Author `xml:"AuthorList>Author"`
			LegacyAffiliation string   `xml:"Affiliation"`
			JournalYear       string   `xml:"Journal>JournalIssue>PubDate>Year"`
			MedlineDate       string   `xml:"Journal>JournalIssue>PubDate>MedlineDate"`
			Dates             []struct {
				Year string `xml:"Year"`
			} `xml:"ArticleDate"`
			ELocation []struct {
				Type  string `xml:"EIdType,attr"`
				Value string `xml:",chardata"`
			} `xml:"ELocationID"`
		} `xml:"Article"`
	} `xml:"MedlineCitation"`
	IDs []ArticleID `xml:"PubmedData>ArticleIdList>ArticleId"`
}
type Record struct {
	PMID, DOI, PMCID, Title, Vernacular string
	Years                               []int
	Authors                             []Author
	LegacyAffiliation                   string
	Countries                           []string
	CountryAmbiguous                    bool
}
func norm(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.Trim(s, " \t\r\n.()[]{}\"'")), " "))
}
func doi(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, p := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/", "doi:"} {
		s = strings.TrimPrefix(s, p)
	}
	return strings.TrimSpace(s)
}

var email = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
var emailTail = regexp.MustCompile(`(?i)(?:[.;,]\s*)?(?:electronic address|e-mail|email)\s*:.*$`)

func countries(s string) ([]string, bool) {
	s = emailTail.ReplaceAllString(s, "")
	s = email.ReplaceAllString(s, "")
	s = strings.TrimRight(s, " ;,\t\r\n")
	ps := strings.FieldsFunc(s, func(c rune) bool { return c == ',' || c == ';' || c == '\n' || c == '\r' || c == '，' || c == '；' })
	if len(ps) == 0 {
		return nil, false
	}
	last := norm(ps[len(ps)-1])
	if aliases[last] == "" {
		return nil, last == "korea" || last == "congo" || last == "georgia"
	}
	cs := map[string]bool{}
	for _, p := range ps[max(0, len(ps)-3):] {
		if c := aliases[norm(p)]; c != "" {
			cs[c] = true
		}
	}
	var out []string
	for c := range cs {
		out = append(out, c)
	}
	sort.Strings(out)
	return out, len(out) > 1
}
func plain(s string) string {
	d := xml.NewDecoder(strings.NewReader("<root>" + s + "</root>"))
	d.Strict = false
	var b strings.Builder
	for {
		t, e := d.Token()
		if e != nil {
			break
		}
		if c, ok := t.(xml.CharData); ok {
			b.Write(c)
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
func tokens(s string) map[string]bool {
	r := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(c rune) bool { return !unicode.IsLetter(c) && !unicode.IsDigit(c) }) {
		if len([]rune(w)) > 1 {
			r[w] = true
		}
	}
	return r
}
func similarity(a, b string) float64 {
	x, y := tokens(a), tokens(b)
	if len(x) == 0 || len(y) == 0 {
		return 0
	}
	n := 0
	for w := range x {
		if y[w] {
			n++
		}
	}
	return float64(n) / float64(len(x)+len(y)-n)
}
func convert(a Article) Record {
	r := Record{PMID: strings.TrimSpace(a.Citation.PMID), Title: plain(a.Citation.Article.Title.Inner), Vernacular: plain(a.Citation.Article.Vernacular.Inner), Authors: a.Citation.Article.Authors, LegacyAffiliation: a.Citation.Article.LegacyAffiliation}
	for _, id := range a.IDs {
		switch strings.ToLower(id.Kind) {
		case "doi":
			r.DOI = doi(id.Value)
		case "pmc":
			r.PMCID = strings.TrimSpace(id.Value)
		}
	}
	if r.DOI == "" {
		for _, v := range a.Citation.Article.ELocation {
			if v.Type == "doi" {
				r.DOI = doi(v.Value)
			}
		}
	}
	ys := []string{a.Citation.Article.JournalYear}
	if a.Citation.Article.MedlineDate != "" {
		ys = append(ys, regexp.MustCompile(`[12][0-9]{3}`).FindString(a.Citation.Article.MedlineDate))
	}
	for _, d := range a.Citation.Article.Dates {
		ys = append(ys, d.Year)
	}
	for _, y := range ys {
		n, e := strconv.Atoi(y)
		if e == nil && n >= 1000 && n <= 2027 {
			r.Years = append(r.Years, n)
		}
	}
	cs := map[string]bool{}
	apply := func(s string) {
		c, amb := countries(s)
		r.CountryAmbiguous = r.CountryAmbiguous || amb
		if !amb {
			for _, x := range c {
				cs[x] = true
			}
		}
	}
	apply(r.LegacyAffiliation)
	for i := range r.Authors {
		for j := range r.Authors[i].Affiliations {
			s := r.Authors[i].Affiliations[j].Text
			apply(s)
			// Keep source payload private and exact; review derivatives redact separately.
		}
	}
	// Keep private legacy source affiliation exact.
	for c := range cs {
		r.Countries = append(r.Countries, c)
	}
	sort.Strings(r.Countries)
	return r
}
