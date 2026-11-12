package main

import (
	"encoding/json"
	"sort"
	"strconv"
)

func (e *Env) evaluate(r RawWork, id uint64, year int32, original []string) (*Patch, Stats, []string) {
	s := Stats{Works: 1}
	if len(original) == 0 {
		s.Missing = 1
	} else {
		s.Known = 1
	}
	if len(r.Authors) > 0 {
		s.WithAuthors = 1
	}
	var instGood, textGood, proposed, matched, embeddedNew, lookupNew []string
	var evidence []Evidence
	hasRaw, hasInst, conflict, ambiguous := false, false, false, false
	for ai, a := range r.Authors {
		if a == nil {
			continue
		}
		var authCountries []string
		for _, c := range a.Countries {
			authCountries = addUnique(authCountries, str(c))
		}
		var instCandidates, authInstGood []string
		var raw []string
		for _, v := range a.Raw {
			if hasText(str(v)) {
				raw = addUnique(raw, str(v))
			}
		}
		type instEvidence struct {
			id, country, source string
			bad                 bool
			reason              string
		}
		var items []instEvidence
		var mentioned []string
		for _, in := range a.Institutions {
			if in == nil {
				continue
			}
			iid, country := str(in.ID), str(in.Country)
			if iid != "" {
				hasInst = true
				mentioned = addUnique(mentioned, iid)
			}
			looked := e.institutions[iid]
			bad := len(looked) > 1 || country != "" && len(looked) == 1 && looked[0] != country
			if e.rules.valid[country] {
				items = append(items, instEvidence{iid, country, "institution_embedded", bad, "embedded_lookup_disagreement"})
			}
			for _, c := range looked {
				items = append(items, instEvidence{iid, c, "institution_lookup", bad, "embedded_lookup_disagreement"})
			}
		}
		for _, aff := range a.Affiliations {
			if aff == nil {
				continue
			}
			if hasText(str(aff.Raw)) {
				raw = addUnique(raw, str(aff.Raw))
			}
			for _, ip := range aff.IDs {
				iid := str(ip)
				if iid == "" {
					continue
				}
				hasInst = true
				if contains(mentioned, iid) {
					continue
				}
				mentioned = append(mentioned, iid)
				cs := e.institutions[iid]
				for _, c := range cs {
					items = append(items, instEvidence{iid, c, "institution_lookup", len(cs) > 1, "institution_has_multiple_country_records"})
				}
			}
		}
		for _, it := range items {
			instCandidates = addUnique(instCandidates, it.country)
			proposed = addUnique(proposed, it.country)
			if !contains(original, it.country) {
				if it.source == "institution_embedded" {
					embeddedNew = addUnique(embeddedNew, it.country)
				} else {
					lookupNew = addUnique(lookupNew, it.country)
				}
			}
			bad, reason := it.bad, it.reason
			if len(authCountries) > 0 && !contains(authCountries, it.country) {
				bad = true
				reason = "novel_country_on_already_labelled_authorship"
			}
			decision := "conservative_candidate"
			if bad {
				conflict = true
				decision = "review_only"
			} else {
				authInstGood = addUnique(authInstGood, it.country)
				instGood = addUnique(instGood, it.country)
			}
			if bad || !contains(original, it.country) {
				evidence = append(evidence, Evidence{Author: ai, Source: it.source, Country: it.country, InstitutionID: it.id, Decision: decision, Reason: reasonIf(bad, reason)})
			}
		}
		_ = authInstGood
		for _, text := range raw {
			hasRaw = true
			hits, amb := e.rules.textHits(text)
			ambiguous = ambiguous || amb
			if amb {
				evidence = append(evidence, Evidence{Author: ai, Source: "raw_exact_tail_country", Raw: text, Decision: "rejected_or_review_only", Reason: "ambiguous_country_name_or_multiple_jurisdictions"})
			}
			for _, h := range hits {
				matched = addUnique(matched, h.Country)
				proposed = addUnique(proposed, h.Country)
				bad, reason := amb, "ambiguous_country_name_or_multiple_jurisdictions"
				if len(authCountries) > 0 && !contains(authCountries, h.Country) {
					bad = true
					reason = "novel_country_on_already_labelled_authorship"
				}
				if len(instCandidates) > 0 && !contains(instCandidates, h.Country) {
					bad = true
					reason = "text_institution_country_disagreement"
				}
				decision := "conservative_candidate"
				if bad {
					conflict = true
					decision = "review_only"
				} else {
					textGood = addUnique(textGood, h.Country)
				}
				if bad || !contains(original, h.Country) {
					evidence = append(evidence, Evidence{Author: ai, Source: h.Rule, Country: h.Country, Raw: text, Segment: h.Segment, Decision: decision, Reason: reasonIf(bad, reason)})
				}
			}
		}
	}
	if hasRaw {
		s.WithRaw = 1
		if len(original) == 0 {
			s.MissingWithRaw = 1
		}
	}
	if hasInst && len(original) == 0 {
		s.MissingWithInst = 1
	}
	if conflict {
		s.Conflicts = 1
	}
	if ambiguous {
		s.AmbiguousText = 1
	}
	instAdded, textAdded := difference(instGood, original), difference(textGood, original)
	added := uniqueSorted(append(append([]string{}, instAdded...), textAdded...))
	allNew := difference(proposed, original)
	reviewOnly := difference(allNew, added)
	if len(original) == 0 {
		if len(instAdded) > 0 {
			s.NewInstitution = 1
		}
		if len(textAdded) > 0 {
			s.NewText = 1
		}
		if s.NewInstitution == 1 && s.NewText == 1 {
			s.NewBoth = 1
		}
		if len(added) > 0 {
			s.NewCombined = 1
		} else if len(reviewOnly) > 0 {
			s.ReviewUnknown = 1
		}
	} else if len(added) > 0 {
		s.KnownExtended = 1
	}
	if len(embeddedNew) > 0 {
		s.EmbeddedNew = 1
	}
	if len(lookupNew) > 0 {
		s.LookupNew = 1
	}
	matched = uniqueSorted(matched)
	if len(matched) > 0 {
		s.TextMatched = 1
		if len(original) == 0 {
			s.MissingTextMatched = 1
		}
	}
	if len(original) > 0 && len(matched) > 0 {
		s.KnownTextMatched = 1
		if len(difference(matched, original)) == 0 {
			s.KnownTextSubset = 1
		} else {
			s.KnownTextNovel = 1
		}
		if len(intersection(matched, original)) == 0 {
			s.KnownTextDisjoint = 1
		}
	}
	if len(added) == 0 && len(reviewOnly) == 0 && !conflict && !ambiguous {
		return nil, s, matched
	}
	conservative := uniqueSorted(append(append([]string{}, original...), added...))
	candidate := uniqueSorted(append(append([]string{}, original...), proposed...))
	s.PatchRows = 1
	b, _ := json.Marshal(evidence)
	p := &Patch{ID: id, WorkID: "https://openalex.org/W" + strconv.FormatUint(id, 10), Year: year, Type: str(r.Type), XPAC: truth(r.XPAC), Original: original, Candidates: candidate, Conservative: conservative, Added: added, InstitutionAdded: instAdded, TextAdded: textAdded, ReviewOnly: reviewOnly, WasUnknown: len(original) == 0, NewlyKnown: s.NewCombined == 1, Conflict: conflict, RuleVersion: ruleVersion, Evidence: string(b)}
	return p, s, matched
}
func reasonIf(b bool, s string) string {
	if b {
		return s
	}
	return ""
}

func (e *Env) summarize(reports []FileReport, indices []int, startTime interface{ Unix() int64 }) error {
	return e.writeSummary(reports, indices, startTime.Unix())
}

func mergeGroups(reports []FileReport, indices []int) ([]Group, map[string]Validation) {
	groups := map[string]*Group{}
	validation := map[string]Validation{}
	for _, i := range indices {
		r := reports[i]
		for _, g := range r.Groups {
			k := g.Corpus + "|" + g.Dimension + "|" + g.Value
			if groups[k] == nil {
				copy := g
				groups[k] = &copy
			} else {
				groups[k].Stats.add(g.Stats)
			}
		}
		for k, v := range r.Validation {
			s := validation[k]
			s.Matched += v.Matched
			s.Subset += v.Subset
			s.Novel += v.Novel
			s.Disjoint += v.Disjoint
			validation[k] = s
		}
	}
	var out []Group
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		return a.Corpus+"/"+a.Dimension+"/"+a.Value < b.Corpus+"/"+b.Dimension+"/"+b.Value
	})
	return out, validation
}
