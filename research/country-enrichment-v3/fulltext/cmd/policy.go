package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

type OriginalAuthors struct {
	ID       uint64 `json:"id"`
	Verified bool   `json:"source_id_year_type_xpac_verified"`
	Loaded   bool   `json:"authors_loaded"`
	Authors  []struct {
		Name    string `json:"name"`
		RawName string `json:"raw_author_name"`
		ORCID   string `json:"orcid"`
	} `json:"authors"`
}
type AuthorMatch struct {
	Original string `json:"original"`
	Source   string `json:"source"`
	Rule     string `json:"rule"`
}
type PolicyOutcome struct {
	Outcome
	V2Status                  string        `json:"v2_status"`
	PolicyConfidence          string        `json:"policy_confidence"`
	AuthorMatches             []AuthorMatch `json:"author_matches"`
	OriginalAuthorsLoaded     bool          `json:"original_authors_loaded"`
	OriginalAuthorCount       int           `json:"original_author_count"`
	SourcePublicationVersions []string      `json:"source_publication_versions"`
	PolicyVersion             string        `json:"policy_version"`
}

// Require a full original name, never initials or surnames alone. Removing spacing
// permits TEI's adjacent <forename>/<surname> elements without fuzzy name matching.
func distinctiveName(s string) bool {
	parts := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) })
	if len(parts) < 2 || len([]rune(normalizedTitle(s))) < 7 {
		return false
	}
	full := 0
	for _, p := range parts {
		if len([]rune(p)) >= 2 {
			full++
		}
	}
	return full >= 2
}
func matchAuthors(original OriginalAuthors, parsed []string) (matches []AuthorMatch) {
	if !original.Verified || !original.Loaded {
		return nil
	}
	for _, a := range original.Authors {
		for _, name := range []string{a.Name, a.RawName} {
			if !distinctiveName(name) {
				continue
			}
			for _, source := range parsed {
				if normalizedTitle(name) == normalizedTitle(source) {
					matches = append(matches, AuthorMatch{name, source, "exact_normalized_distinctive_full_name"})
					break
				}
			}
			if len(matches) > 0 && matches[len(matches)-1].Original == name {
				break
			}
		}
	}
	return
}
func longDistinctiveTitle(s string) bool {
	n := normalizedTitle(s)
	if len([]rune(n)) < 20 {
		return false
	}
	words := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	cjk := 0
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) {
			cjk++
		}
	}
	return len(words) >= 5 || cjk >= 12
}
func v3Identity(w Work, p Parsed, a OriginalAuthors) (string, []AuthorMatch) {
	if normalizeDOI(w.DOI) != "" && normalizeDOI(p.DOI) != "" && normalizeDOI(w.DOI) != normalizeDOI(p.DOI) {
		return "doi_conflict", nil
	}
	year, _ := strconv.Atoi(p.Year)
	if year == 0 || w.Year == 0 || abs(w.Year-year) > 1 {
		return "missing_or_conflicting_year", nil
	}
	if !longDistinctiveTitle(w.Title) || normalizedTitle(w.Title) != normalizedTitle(p.Title) {
		return "not_exact_distinctive_title", nil
	}
	m := matchAuthors(a, p.Authors)
	if len(m) > 0 {
		return "exact_long_title_year_original_full_author", m
	}
	return "title_year_only_review", nil
}
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Read cached v2 outputs only: no source GET, parser request or frame mutation.
func auditV3(sample Sample, plan Plan, input, authorsDir, output string) {
	works := map[uint64]Work{}
	for _, w := range sample.Works {
		works[w.ID] = w
	}
	counts := map[string]int{}
	for _, pick := range plan.PDF {
		b, e := os.ReadFile(filepath.Join(input, fmt.Sprintf("W%d.json", pick.ID)))
		must(e)
		var old Outcome
		must(json.Unmarshal(b, &old))
		o := PolicyOutcome{Outcome: old, V2Status: old.Status, PolicyConfidence: "unchanged_v2", PolicyVersion: "pdf-identity-v3-exact-title-year-original-full-author"}
		w := works[pick.ID]
		for _, l := range w.Locations {
			if l.PDF == pick.URL && l.Version != "" {
				o.SourcePublicationVersions = addUnique(o.SourcePublicationVersions, l.Version)
			}
		}
		var a OriginalAuthors
		if b, e = os.ReadFile(filepath.Join(authorsDir, fmt.Sprintf("W%d.json", pick.ID))); e == nil {
			must(json.Unmarshal(b, &a))
			mustID := a.ID == pick.ID && a.Verified && a.Loaded
			if !mustID {
				panic("unverified authors")
			}
			o.OriginalAuthorsLoaded = true
			o.OriginalAuthorCount = len(a.Authors)
		}
		if old.Parsed != nil && old.Status == "identity_review" {
			rule, m := v3Identity(w, *old.Parsed, a)
			o.AuthorMatches = m
			o.PolicyConfidence = rule
			if rule == "exact_long_title_year_original_full_author" {
				o.IdentityPass = true
				o.IdentityRule = "v3_" + rule
				o.CandidateCountries = old.Parsed.Countries
				if len(o.CandidateCountries) > 0 {
					o.Status = "country_candidate_review_pending"
				} else {
					o.Status = "parsed_no_linked_country"
				}
			} else if rule == "title_year_only_review" && len(old.Parsed.Countries) > 0 {
				o.Status = "title_year_only_country_review"
			}
		}
		counts[o.PolicyConfidence+":"+o.Status]++
		saveJSON(filepath.Join(output, fmt.Sprintf("W%d.json", pick.ID)), o)
	}
	fmt.Println("v3 policy classifications", counts)
}
