package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const frozenPolicyVersion = "fulltext-holdout-v3-20261112-01"

type FrozenOutcome struct {
	Outcome
	OriginalExtractionStatus               string        `json:"original_extraction_status"`
	PolicyVersion                          string        `json:"policy_version"`
	PolicySHA256                           string        `json:"policy_sha256"`
	AuthorMatches                          []AuthorMatch `json:"author_matches"`
	OriginalAuthorsLoaded                  bool          `json:"original_authors_loaded"`
	OriginalAuthorCount                    int           `json:"original_author_count"`
	SourcePublicationVersion               string        `json:"source_publication_version"`
	SourceVisibleCountryCompleteness       string        `json:"source_visible_country_completeness"`
	CompletePaperCountrySet                *bool         `json:"complete_paper_country_set"`
	UnresolvedAffiliations                 int           `json:"unresolved_affiliations"`
	SourceAuthorsWithoutLinkedAffiliations int           `json:"source_authors_without_linked_affiliations"`
	ProductionApplied                      bool          `json:"production_applied"`
}

func fullName(s string) bool {
	parts := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) })
	if len(parts) >= 2 {
		n := 0
		letters := 0
		for _, p := range parts {
			l := len([]rune(p))
			letters += l
			if l >= 2 {
				n++
			}
		}
		return n >= 2 && letters >= 5
	}
	cjk := 0
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) {
			cjk++
		}
	}
	return cjk >= 3
}
func tokens(s string) string {
	p := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) })
	sort.Strings(p)
	return strings.Join(p, " ")
}
func frozenAuthorMatches(a OriginalAuthors, parsed []string) []AuthorMatch {
	if !a.Verified || !a.Loaded {
		return nil
	}
	var matches []AuthorMatch
	for _, author := range a.Authors {
		for _, name := range []string{author.Name, author.RawName} {
			if !fullName(name) {
				continue
			}
			for _, source := range parsed {
				rule := ""
				if normalizedTitle(name) == normalizedTitle(source) {
					rule = "exact_normalized_full_personal_name"
				} else if fullName(source) && tokens(name) == tokens(source) {
					rule = "exact_full_name_tokens_order_insensitive"
				}
				if rule != "" {
					matches = append(matches, AuthorMatch{name, source, rule})
					break
				}
			}
			if len(matches) > 0 && matches[len(matches)-1].Original == name {
				break
			}
		}
	}
	return matches
}
func frozenIdentity(w Work, p Parsed, a OriginalAuthors, published bool) (bool, string, []AuthorMatch) {
	if normalizeDOI(w.DOI) != "" && normalizeDOI(p.DOI) != "" && normalizeDOI(w.DOI) != normalizeDOI(p.DOI) {
		return false, "doi_conflict", nil
	}
	if w.PMID != "" && p.PMID != "" && strings.Trim(w.PMID, "/") != strings.Trim(p.PMID, "/") {
		return false, "pmid_conflict", nil
	}
	year, _ := strconv.Atoi(p.Year)
	if year == 0 || w.Year == 0 || abs(w.Year-year) > 1 {
		return false, "missing_or_conflicting_year", nil
	}
	if !published && year != w.Year {
		return false, "nonpublished_version_year_changed", nil
	}
	exact := normalizedTitle(w.Title) == normalizedTitle(p.Title)
	for _, alt := range p.AlternativeTitles {
		exact = exact || normalizedTitle(w.Title) == normalizedTitle(alt)
	}
	if !longDistinctiveTitle(w.Title) || !exact {
		return false, "not_exact_distinctive_title_or_source_translation", nil
	}
	m := frozenAuthorMatches(a, p.Authors)
	if len(m) == 0 {
		return false, "missing_original_full_author_concordance", nil
	}
	return true, "exact_title_year_original_full_author_consistent_identifiers", m
}
func sourceVersion(w Work, o Outcome, pickURL string) (string, bool) {
	if o.Route == "pmc_jats" {
		if o.Metadata == nil {
			return "unknown_pmc_version", false
		}
		b, e := os.ReadFile(o.Metadata.Path)
		if e != nil {
			return "unreadable_pmc_version", false
		}
		var m struct {
			Manuscript *bool `json:"is_manuscript"`
		}
		if json.Unmarshal(b, &m) != nil || m.Manuscript == nil {
			return "unknown_pmc_version", false
		}
		if *m.Manuscript {
			return "author_manuscript", false
		}
		return "published_pmc", true
	}
	for _, l := range w.Locations {
		if l.PDF == pickURL && l.Version != "" {
			return l.Version, l.Version == "publishedVersion"
		}
	}
	return "unknown_pdf_version", false
}
func applyFrozenPolicy(sample Sample, input, authorsDir, planPath string) {
	works := map[uint64]Work{}
	for _, w := range sample.Works {
		works[w.ID] = w
	}
	picks := map[uint64]string{}
	if planPath != "" {
		var p Plan
		readJSON(planPath, &p)
		for _, x := range p.PDF {
			picks[x.ID] = x.URL
		}
	}
	// Digest the explicit manifest path supplied alongside the executable output.
	policyBytes, e := os.ReadFile(filepath.Join(outdir, "frozen-policy.json"))
	must(e)
	policySHA := digest(policyBytes)
	files, e := filepath.Glob(filepath.Join(input, "W*.json"))
	must(e)
	counts := map[string]int{}
	for _, f := range files {
		var old Outcome
		readJSON(f, &old)
		w, ok := works[old.ID]
		if !ok {
			panic("outcome outside frozen metadata frame")
		}
		o := FrozenOutcome{Outcome: old, OriginalExtractionStatus: old.Status, PolicyVersion: frozenPolicyVersion, PolicySHA256: policySHA, SourceVisibleCountryCompleteness: "unknown", ProductionApplied: false}
		o.IdentityPass = false
		o.CandidateCountries = nil
		var a OriginalAuthors
		if b, e := os.ReadFile(filepath.Join(authorsDir, fmt.Sprintf("W%d.json", w.ID))); e == nil {
			must(json.Unmarshal(b, &a))
			if a.ID != w.ID || !a.Verified || !a.Loaded {
				panic("unverified original authors cache")
			}
		}
		o.OriginalAuthorsLoaded = a.Loaded
		o.OriginalAuthorCount = len(a.Authors)
		if old.Parsed == nil {
			o.Status = "source_unavailable_or_unparsed"
		} else {
			version, published := sourceVersion(w, old, picks[w.ID])
			o.SourcePublicationVersion = version
			o.IdentityPass, o.IdentityRule, o.AuthorMatches = frozenIdentity(w, *old.Parsed, a, published)
			if old.Route == "pmc_jats" && version != "published_pmc" {
				o.IdentityPass = false
				o.IdentityRule = "not_confirmed_published_pmc_version"
			}
			if !o.IdentityPass {
				o.Status = "identity_rejected_or_review"
			} else {
				covered := map[string]bool{}
				for _, aff := range old.Parsed.Affiliations {
					if !aff.Linked || aff.Ambiguous || len(aff.Countries) == 0 {
						o.UnresolvedAffiliations++
						continue
					}
					for _, name := range aff.AuthorRefs {
						covered[name] = true
					}
					for _, country := range aff.Countries {
						o.CandidateCountries = addUnique(o.CandidateCountries, country)
					}
				}
				for _, name := range old.Parsed.Authors {
					if !covered[name] {
						o.SourceAuthorsWithoutLinkedAffiliations++
					}
				}
				sort.Strings(o.CandidateCountries)
				if len(o.CandidateCountries) == 0 {
					o.Status = "no_supported_author_country"
				} else {
					o.Status = "automatic_country_candidate"
				}
				o.SourceVisibleCountryCompleteness = "partial_or_unknown"
				if o.UnresolvedAffiliations == 0 && o.SourceAuthorsWithoutLinkedAffiliations == 0 && len(old.Parsed.Authors) > 0 {
					o.SourceVisibleCountryCompleteness = "all_visible_authors_and_affiliations_resolved"
				}
			}
		}
		counts[o.Status]++
		saveJSON(filepath.Join(outdir, "policy-results", fmt.Sprintf("W%d.json", w.ID)), o)
	}
	saveJSON(filepath.Join(outdir, "policy-summary.json"), map[string]any{"policy_version": frozenPolicyVersion, "policy_sha256": policySHA, "counts": counts, "production_applied": false, "complete_paper_country_set": nil})
	fmt.Println("Frozen fulltext policy", counts)
}
