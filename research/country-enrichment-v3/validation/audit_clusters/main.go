// Protocol-r3 cluster audit gate. Original-source gold is supplied independently.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
)

type Review struct {
	ID               uint64 `json:"id"`
	Route            string `json:"route"`
	Selected         bool   `json:"selected_for_audit"`
	Identity         string `json:"identity_status"`
	CountrySupported *bool  `json:"all_predicted_countries_source_supported"`
	Source           string `json:"original_source_path"`
	SHA              string `json:"original_source_sha256"`
	Evidence         string `json:"independent_evidence"`
	Visible          *bool  `json:"source_visible_country_set_complete"`
	Full             *bool  `json:"full_paper_country_set_complete"`
	Systemic         bool   `json:"unisolated_systemic_error"`
}
type Cluster struct {
	ID      uint64 `json:"cluster_id"`
	Route   string `json:"route"`
	Hash    string `json:"audit_hash_sha256"`
	MemberN int    `json:"member_n"`
	Members []struct {
		ID uint64 `json:"id"`
	} `json:"members"`
}
type Sample struct {
	Status    string    `json:"status"`
	Route     string    `json:"route"`
	N         int       `json:"sample_cluster_n"`
	Members   int       `json:"sample_member_work_n"`
	Records   []Cluster `json:"records"`
	Frame     int       `json:"eligible_independent_cluster_N"`
	WorkFrame int       `json:"eligible_work_frame_N"`
}

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func success(r Review) bool {
	_, e := hex.DecodeString(r.SHA)
	return r.Selected && r.Identity == "verified" && r.CountrySupported != nil && *r.CountrySupported && r.Source != "" && len(r.SHA) == 64 && e == nil && r.Evidence != ""
}
func wilson(s, n int) (float64, float64) {
	z := 1.959963984540054
	nn := float64(n)
	p := float64(s) / nn
	den := 1 + z*z/nn
	c := (p + z*z/(2*nn)) / den
	h := z * math.Sqrt(p*(1-p)/nn+z*z/(4*nn*nn)) / den
	return c - h, c + h
}
func evaluate(sample Sample, reviews []Review) map[string]any {
	if sample.Status != "frozen_independent_paper_cluster_audit_sample" || sample.N != len(sample.Records) || sample.Frame < sample.N || sample.WorkFrame < sample.Members {
		panic("wrong or inconsistent frozen cluster sample")
	}
	byID := map[uint64]Review{}
	for _, r := range reviews {
		if !r.Selected || r.Route != sample.Route {
			continue
		}
		if r.ID == 0 {
			panic("zero reviewed Work")
		}
		if _, ok := byID[r.ID]; ok {
			panic("duplicate reviewed Work")
		}
		byID[r.ID] = r
	}
	expected := map[uint64]bool{}
	clustersSeen := map[uint64]bool{}
	works, workSuccess, clusterSuccess, visibleObserved, visibleComplete, fullObserved, fullComplete, missing := 0, 0, 0, 0, 0, 0, 0, 0
	systemic := false
	failures := []map[string]any{}
	for _, cl := range sample.Records {
		if cl.Route != sample.Route || cl.ID == 0 || cl.MemberN != len(cl.Members) || cl.MemberN == 0 || clustersSeen[cl.ID] {
			panic("invalid/duplicate frozen cluster")
		}
		clustersSeen[cl.ID] = true
		h := sha256.Sum256([]byte("openalex-country-enrichment-v3-20261112-route-candidate-audit-01\n" + sample.Route + "\n" + strconv.FormatUint(cl.ID, 10)))
		if cl.Hash != hex.EncodeToString(h[:]) {
			panic("cluster audit hash mismatch")
		}
		ok := true
		minID := uint64(math.MaxUint64)
		failed := []uint64{}
		for _, m := range cl.Members {
			if m.ID == 0 || expected[m.ID] {
				panic("duplicate/zero sample member")
			}
			expected[m.ID] = true
			works++
			if m.ID < minID {
				minID = m.ID
			}
			r, exists := byID[m.ID]
			good := exists && success(r)
			if good {
				workSuccess++
			} else {
				ok = false
				failed = append(failed, m.ID)
				if !exists {
					missing++
				}
			}
			systemic = systemic || r.Systemic
			if r.Visible != nil {
				visibleObserved++
				if *r.Visible {
					visibleComplete++
				}
			}
			if r.Full != nil {
				fullObserved++
				if *r.Full {
					fullComplete++
				}
			}
		}
		if minID != cl.ID {
			panic("cluster ID is not minimum member WorkID")
		}
		if ok {
			clusterSuccess++
		} else {
			failures = append(failures, map[string]any{"cluster_id": cl.ID, "incorrect_or_unverified_member_ids": failed})
		}
	}
	if works != sample.Members {
		panic("sample member count mismatch")
	}
	for id := range byID {
		if !expected[id] {
			panic("selected review outside frozen cluster sample")
		}
	}
	var cp, wp, lo, hi *float64
	if sample.N > 0 {
		p := float64(clusterSuccess) / float64(sample.N)
		l, h := wilson(clusterSuccess, sample.N)
		cp, lo, hi = &p, &l, &h
	}
	if works > 0 {
		p := float64(workSuccess) / float64(works)
		wp = &p
	}
	status, reason := "insufficient_n", "At least128 independent paper clusters required; unreviewed or unverified members stay in denominator"
	if sample.N >= 128 {
		if systemic {
			status = "blocked_systemic_error"
			reason = "Unisolated systematic identity/role/reference error forbids rollout"
		} else if *cp < .99 || *lo < .97 || *wp < .99 {
			status = "failed_precision"
			reason = "Requires cluster point>=.99, two-sided95 Wilson lower>=.97, and selected-member Work point>=.99"
		} else {
			status = "passed_source_supported_country_precision"
			reason = "Supports supplied source-visible author-country labels in declared frame; full-paper completeness separately qualified"
		}
	}
	sort.Slice(failures, func(i, j int) bool { return failures[i]["cluster_id"].(uint64) < failures[j]["cluster_id"].(uint64) })
	return map[string]any{"route": sample.Route, "status": status, "reason": reason, "selected_independent_clusters": sample.N, "successful_clusters": clusterSuccess, "incorrect_or_unverified_clusters": sample.N - clusterSuccess, "cluster_precision": cp, "cluster_wilson95_lower": lo, "cluster_wilson95_upper": hi, "selected_member_works": works, "successful_member_works": workSuccess, "member_work_point_precision": wp, "member_work_IID_wilson_not_claimed": true, "missing_member_reviews": missing, "unisolated_systemic_error": systemic, "source_visible_completeness_observed": visibleObserved, "source_visible_complete": visibleComplete, "full_paper_completeness_observed": fullObserved, "full_paper_complete": fullComplete, "failed_clusters": failures, "eligible_cluster_frame_N": sample.Frame, "eligible_work_frame_N": sample.WorkFrame, "no_failure_replacement": true, "production_applied": false, "user_review_complete": false}
}
func main() {
	if len(os.Args) != 4 {
		panic("usage: audit_clusters frozen-cluster-sample.json independent-member-reviews.json output.json")
	}
	sb, e := os.ReadFile(os.Args[1])
	must(e)
	rb, e := os.ReadFile(os.Args[2])
	must(e)
	var s Sample
	must(json.Unmarshal(sb, &s))
	var r struct {
		Reviews []Review `json:"reviews"`
	}
	must(json.Unmarshal(rb, &r))
	out := evaluate(s, r.Reviews)
	out["sample_sha256"] = fmt.Sprintf("%x", sha256.Sum256(sb))
	out["reviews_sha256"] = fmt.Sprintf("%x", sha256.Sum256(rb))
	b, e := json.MarshalIndent(out, "", "  ")
	must(e)
	must(os.WriteFile(os.Args[3], append(b, '\n'), 0600))
	fmt.Println(string(b))
}
