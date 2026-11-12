package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"
)

const ruleVersion = "country-enrichment-review-v1"

type Alias struct{ Name, Code, Source string }
type Rules struct {
	Version         string            `json:"version"`
	Aliases         []Alias           `json:"aliases"`
	RejectedAliases []string          `json:"rejected_aliases"`
	Descriptions    map[string]string `json:"descriptions"`
	alias           map[string]string
	valid           map[string]bool
}

func loadRules(dictionary string) (Rules, error) {
	b, e := os.ReadFile(dictionary)
	if e != nil {
		return Rules{}, e
	}
	// OpenAlex dictionary JSON uses snake_case.
	var raw struct {
		Countries []struct {
			ID   string `json:"id"`
			Name string `json:"display_name"`
		} `json:"countries"`
	}
	if e = json.Unmarshal(b, &raw); e != nil {
		return Rules{}, e
	}
	r := Rules{Version: ruleVersion, alias: map[string]string{}, valid: map[string]bool{}, RejectedAliases: []string{"all two-letter codes including US/UK/IN/CA", "Georgia", "Jersey", "Congo", "Korea", "Virgin Islands"}, Descriptions: map[string]string{
		"institution_embedded":   "Country of the institution explicitly attached to this paper's authorship; conflicting embedded/lookup countries are review-only.",
		"institution_lookup":     "Same-snapshot institution ID lookup; includes authorship.institutions and authorship.affiliations.institution_ids; never author last-known affiliations.",
		"raw_exact_tail_country": "Case-folded complete country-name segment in the final three comma/semicolon/newline-delimited address segments. A segment must consist entirely of the alias; no substrings, free-word matching, city/state guessing or two-letter abbreviations. Multiple distinct country codes in one text are review-only.",
		"candidate_policy":       "Evidence-backed candidate only, not production or manually validated truth. Preserve originals. Unknown-to-known and extension of already-known sets reported separately.",
		"disagreement":           "Authorship baseline countries, institution candidates and text candidates are cross-checked. Novel countries on already-labelled authorships, embedded/lookup disagreement, or text/institution disagreement are retained for review and withheld from conservative candidates.",
		"secondary_review_v1b":   "Additional sidecars retain unresolved raw affiliations, exact snapshot institution-name matches and institution.geo.country_code when top-level country is absent. These do not enter v1 conservative candidates. Single-country authorship raw-string anchors are lowercased and whitespace-normalized, deduplicated per Work/country/text SHA256; all associated institution IDs are unioned for institution-grouped holdout evaluation.",
	}}
	blocked := map[string]bool{"georgia": true, "jersey": true, "congo": true, "korea": true, "virgin islands": true}
	add := func(name, code, source string) error {
		key := normalizeSegment(name)
		if len([]rune(key)) < 3 || blocked[key] {
			return nil
		}
		if prev, ok := r.alias[key]; ok && prev != code {
			return fmt.Errorf("ambiguous alias %s", name)
		}
		if _, ok := r.alias[key]; !ok {
			r.Aliases = append(r.Aliases, Alias{name, code, source})
			r.alias[key] = code
		}
		return nil
	}
	for _, c := range raw.Countries {
		r.valid[c.ID] = true
		if e = add(c.Name, c.ID, "snapshot_country_dictionary"); e != nil {
			return r, e
		}
	}
	manual := map[string][]string{
		"US": {"United States", "United States of America", "USA", "U.S.A."},
		"GB": {"United Kingdom", "United Kingdom of Great Britain and Northern Ireland", "Great Britain", "England", "Scotland", "Wales", "Northern Ireland"},
		"KR": {"South Korea", "Republic of Korea", "Korea, Republic of"}, "KP": {"North Korea", "Democratic People's Republic of Korea"},
		"CN": {"People's Republic of China", "P. R. China", "P.R. China", "PR China", "中国", "中华人民共和国"},
		"RU": {"Russia", "Russian Federation", "Российская Федерация", "Россия"}, "IR": {"Iran", "Islamic Republic of Iran"},
		"TW": {"Taiwan", "Taiwan R.O.C.", "Republic of China (Taiwan)"}, "HK": {"Hong Kong", "Hong Kong SAR"}, "MO": {"Macao", "Macau"},
		"VN": {"Vietnam", "Viet Nam"}, "CZ": {"Czech Republic", "Czechia"}, "TR": {"Turkey", "Türkiye"}, "SY": {"Syria"},
		"BO": {"Bolivia"}, "VE": {"Venezuela"}, "TZ": {"Tanzania"}, "LA": {"Laos", "Lao PDR"}, "MD": {"Moldova"},
		"CD": {"Democratic Republic of the Congo", "DR Congo"}, "CG": {"Republic of the Congo"},
		"DE": {"Deutschland", "Allemagne"}, "ES": {"España"}, "BR": {"Brasil"}, "IT": {"Italia"}, "JP": {"日本"}, "PL": {"Polska"}, "PT": {"Portugal"}, "MX": {"México"}, "GR": {"Greece", "Hellas"},
	}
	for code, names := range manual {
		if !r.valid[code] {
			return r, fmt.Errorf("alias country absent %s", code)
		}
		for _, name := range names {
			if e = add(name, code, "explicit_reviewed_name_alias"); e != nil {
				return r, e
			}
		}
	}
	sort.Slice(r.Aliases, func(i, j int) bool {
		if r.Aliases[i].Code != r.Aliases[j].Code {
			return r.Aliases[i].Code < r.Aliases[j].Code
		}
		return r.Aliases[i].Name < r.Aliases[j].Name
	})
	return r, nil
}

func normalizeSegment(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, " \t\r\n.()[]{}\"'")
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

type TextHit struct {
	Country string `json:"country"`
	Segment string `json:"segment"`
	Rule    string `json:"rule"`
}

func (r Rules) textHits(raw string) (hits []TextHit, ambiguous bool) {
	// Exact segment matching prevents Sudan inside South Sudan and Guinea
	// inside Papua New Guinea, as well as country names inside institution brands.
	segments := strings.FieldsFunc(raw, func(c rune) bool { return c == ',' || c == ';' || c == '\n' || c == '\r' || c == '，' || c == '；' })
	if len(segments) == 0 {
		return nil, false
	}
	last := normalizeSegment(segments[len(segments)-1])
	_, lastHasCountry := r.alias[last]
	start := max(0, len(segments)-3)
	for offset, part := range segments[start:] {
		key := normalizeSegment(part)
		if code, ok := r.alias[key]; ok {
			found := false
			for _, h := range hits {
				found = found || h.Country == code
			}
			if !found {
				hits = append(hits, TextHit{code, strings.TrimSpace(part), "raw_exact_tail_country"})
			}
		}
		if start+offset == len(segments)-1 && (key == "georgia" || key == "jersey" || key == "congo" || key == "korea" || key == "virgin islands") {
			ambiguous = true
		}
	}
	if len(hits) > 1 {
		ambiguous = true
	}
	// A country-like segment in the middle of an address is not sufficient.
	// Earlier tail segments only detect conflicting jurisdiction mentions.
	if !lastHasCountry {
		return nil, ambiguous
	}
	return
}

func uniqueSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	sort.Strings(in)
	out := in[:0]
	for _, v := range in {
		if v != "" && (len(out) == 0 || out[len(out)-1] != v) {
			out = append(out, v)
		}
	}
	return out
}
func addUnique(dst []string, v string) []string {
	if v == "" {
		return dst
	}
	for _, x := range dst {
		if x == v {
			return dst
		}
	}
	return append(dst, v)
}
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
func difference(a, b []string) (out []string) {
	for _, v := range a {
		if !contains(b, v) {
			out = append(out, v)
		}
	}
	return uniqueSorted(out)
}
func hasText(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return !unicode.IsSpace(r) }) >= 0
}
