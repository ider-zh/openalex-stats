// Design-weighted experimental pipeline opportunity. This is not a rollout gate.
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

type Work struct {
	ID      uint64  `json:"id"`
	Year    int     `json:"year"`
	XPAC    bool    `json:"is_xpac"`
	Stratum string  `json:"stratum"`
	Pi      float64 `json:"inclusion_probability"`
	Weight  float64 `json:"design_weight"`
}
type Counts struct {
	ID           uint64   `json:"id"`
	CoreEligible bool     `json:"core_eligible"`
	Core         []uint64 `json:"core_incoming_reference_events_2000_2026"`
	All          []uint64 `json:"all_incoming_reference_events_2000_2026"`
}
type Candidate struct {
	ID      uint64  `json:"id"`
	Route   string  `json:"source_route"`
	Year    int     `json:"year"`
	XPAC    bool    `json:"is_xpac"`
	Stratum string  `json:"stratum"`
	Pi1     float64 `json:"pi1"`
	Pi2     float64 `json:"pi2"`
	Status  string  `json:"status"`
}
type Total struct {
	ObservedCandidateWorks                 int       `json:"observed_candidate_works"`
	HTRecoveredUnknownWorks                float64   `json:"ht_recovered_unknown_works"`
	ObservedPublicationWorks2000to2025     int       `json:"observed_publication_works_2000_2025"`
	HTPublicationWorks2000to2025           float64   `json:"ht_publication_works_2000_2025"`
	ObservedPublicationWorks2000to2026     int       `json:"observed_publication_works_2000_2026"`
	HTPublicationWorks2000to2026           float64   `json:"ht_publication_works_2000_2026"`
	HTCountryKnownCitationEventsByYear     []float64 `json:"ht_country_known_incoming_events_year2000_2026"`
	HTCountryKnownCitationEvents2000to2025 float64   `json:"ht_country_known_events_2000_2025"`
}

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func load(path string, v any) string {
	b, e := os.ReadFile(path)
	must(e)
	must(json.Unmarshal(b, v))
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
func near(a, b float64) bool {
	return math.Abs(a-b) <= 1e-12*math.Max(math.Abs(a), math.Abs(b))+1e-18
}
func main() {
	if len(os.Args) != 2 {
		panic("usage: probability_effect analysis-base")
	}
	b := os.Args[1]
	v := filepath.Join(b, "validation")
	hashes := map[string]string{}
	var sample struct {
		Frame any    `json:"frame"`
		Size  int    `json:"sample_size"`
		N     uint64 `json:"baseline_unknown"`
		Works []Work `json:"works"`
	}
	hashes["sample-ids.json"] = load(filepath.Join(v, "sample-ids.json"), &sample)
	works := map[uint64]Work{}
	for _, w := range sample.Works {
		works[w.ID] = w
	}
	var counts struct {
		Works []Counts `json:"works"`
	}
	hashes["sample-citation-counts.json"] = load(filepath.Join(v, "sample-citation-counts.json"), &counts)
	ct := map[uint64]Counts{}
	for _, r := range counts.Works {
		ct[r.ID] = r
	}
	var prior struct {
		IDs []uint64 `json:"ids"`
	}
	hashes["probability-fulltext-prior-text-recoveries.json"] = load(filepath.Join(v, "probability-fulltext-prior-text-recoveries.json"), &prior)
	known := map[uint64]bool{}
	for _, id := range prior.IDs {
		known[id] = true
	}
	var pdf struct {
		Rows []struct {
			ID      uint64  `json:"id"`
			Pi      float64 `json:"pi2"`
			Stratum string  `json:"stratum"`
		} `json:"pdf"`
		Strata []struct {
			Name     string  `json:"stratum"`
			N        int     `json:"eligible"`
			Selected int     `json:"selected"`
			Pi       float64 `json:"pi2"`
		} `json:"pdf_strata"`
	}
	hashes["pdf-plan.json"] = load(filepath.Join(b, "fulltext/pdf-frame-prep/pdf-plan.json"), &pdf)
	pdfPi := map[uint64]float64{}
	for _, r := range pdf.Rows {
		pdfPi[r.ID] = r.Pi
	}
	for _, h := range pdf.Strata {
		if !near(h.Pi, float64(h.Selected)/float64(h.N)) {
			panic("phase2 stratum probability mismatch")
		}
	}
	totals := map[string]*Total{}
	get := func(key string) *Total {
		if t, ok := totals[key]; ok {
			return t
		}
		t := &Total{HTCountryKnownCitationEventsByYear: make([]float64, 27)}
		totals[key] = t
		return t
	}
	observed := []map[string]any{}
	seen := map[uint64]bool{}
	for _, rel := range []string{"fulltext/probability/export/automatic-candidates.jsonl", "fulltext/pdf-probability/export/automatic-candidates.jsonl"} {
		path := filepath.Join(b, rel)
		raw, e := os.ReadFile(path)
		must(e)
		hashes[rel] = fmt.Sprintf("%x", sha256.Sum256(raw))
		f, e := os.Open(path)
		must(e)
		s := bufio.NewScanner(f)
		s.Buffer(make([]byte, 1<<20), 64<<20)
		for s.Scan() {
			var c Candidate
			must(json.Unmarshal(s.Bytes(), &c))
			w, ok := works[c.ID]
			if !ok || seen[c.ID] || c.Status != "automatic_country_candidate" || w.Year != c.Year || w.XPAC != c.XPAC || w.Stratum != c.Stratum || !near(w.Pi, c.Pi1) || !near(w.Weight, 1/w.Pi) {
				panic("candidate outside or inconsistent with authoritative original probability sample")
			}
			seen[c.ID] = true
			pi2 := 1.
			if c.Route == "oa_pdf_grobid_header" {
				pi2 = pdfPi[c.ID]
				if pi2 <= 0 {
					panic("PDF candidate outside frozen phase2 sample")
				}
			} else if c.Route != "pmc_jats" {
				panic("unexpected route")
			}
			if !near(pi2, c.Pi2) {
				panic("candidate phase2 probability mismatch")
			}
			weight := 1 / (w.Pi * pi2)
			r, ok := ct[c.ID]
			if !ok || len(r.All) != 27 || (r.CoreEligible && len(r.Core) != 27) {
				panic("invalid original graph incoming-event record")
			}
			observed = append(observed, map[string]any{"id": c.ID, "route": c.Route, "year": w.Year, "is_xpac": w.XPAC, "pi1": w.Pi, "pi2": pi2, "combined_weight": weight, "already_recovered_by_passed_text": known[c.ID]})
			for _, domain := range []string{"all_pipeline_candidates", "net_after_passed_text"} {
				if domain == "net_after_passed_text" && known[c.ID] {
					continue
				}
				for _, corpus := range []string{"Core", "All"} {
					if corpus == "Core" && !r.CoreEligible {
						continue
					}
					events := r.All
					if corpus == "Core" {
						events = r.Core
					}
					for _, route := range []string{c.Route, "combined_pmc_then_pdf"} {
						t := get(domain + "/" + corpus + "/" + route)
						t.ObservedCandidateWorks++
						t.HTRecoveredUnknownWorks += weight
						if w.Year >= 2000 && w.Year <= 2025 {
							t.ObservedPublicationWorks2000to2025++
							t.HTPublicationWorks2000to2025 += weight
						}
						if w.Year >= 2000 && w.Year <= 2026 {
							t.ObservedPublicationWorks2000to2026++
							t.HTPublicationWorks2000to2026 += weight
						}
						for i, n := range events {
							t.HTCountryKnownCitationEventsByYear[i] += weight * float64(n)
							if i < 26 {
								t.HTCountryKnownCitationEvents2000to2025 += weight * float64(n)
							}
						}
					}
				}
			}
		}
		must(s.Err())
		must(f.Close())
	}
	out := map[string]any{"status": "independent_probability_pipeline_ht_point_estimates", "remaining_non_development_unknown_frame_N": sample.N, "sample_n": sample.Size, "input_sha256": hashes, "totals": totals, "observed_candidates": observed, "rollout_gate_passed": false, "production_applied": false, "confidence_intervals": nil, "notes": []string{"Experimental operational pipeline opportunity, not validated full-patch recovery and not claims that unavailable sources lack countries.", "First-stage stratum probability is authoritative n/N after predeclared development Work exclusion; second-stage PDF probability is frozen selected/eligible within first-stage stratum. HT weight=1/(pi1*pi2), not pi1-only design_weight.", "Source extraction, country-label precision, and country-set completeness are distinct. PMC7 supported7, visiblecomplete7; PDF5 supported5, visiblecomplete4; full-paper completeness unknown for all12; routes insufficient for rollout.", "Net excludes only actual Root passed text patch IDs. No unpassed metadata routes are subtracted; refresh later if accepted route precedence changes.", "Works before2000/unknown-year can contribute incoming citation events in2000-2026 but are excluded from publication counts beginning2000.", "Incoming events use original fixed Core/All graph/corpus, never external cumulative citation counts; recovered-country events count each citing-reference event once rather than multiplying by countries.", "Rare positives and two-stage selection make point estimates uncertain; no crude binomial or normal interval is labelled a survey-design CI. These estimates are not absolute country completeness or ECI/PCI estimates.", "Target frame is residual277411702 original UNKNOWN Works, excluding6766 prior-development UNKNOWN Works; do not silently substitute the full277418468 denominator."}}
	raw, e := json.MarshalIndent(out, "", "  ")
	must(e)
	must(os.WriteFile(filepath.Join(v, "probability-fulltext-ht-effects.json"), append(raw, '\n'), 0600))
	for k, t := range totals {
		if k == "net_after_passed_text/Core/combined_pmc_then_pdf" || k == "all_pipeline_candidates/Core/combined_pmc_then_pdf" {
			fmt.Printf("%s observed=%d HTworks=%.3f HTpublications2000-2025=%.3f HTcitationevents2000-2025=%.3f\n", k, t.ObservedCandidateWorks, t.HTRecoveredUnknownWorks, t.HTPublicationWorks2000to2025, t.HTCountryKnownCitationEvents2000to2025)
		}
	}
}
