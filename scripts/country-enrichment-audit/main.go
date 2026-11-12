// Independent release gates over the evaluator's sealed sidecars and SQL oracle.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"openalexstats/internal/checkpoint"
)

type coverage struct {
	Corpus string            `json:"corpus"`
	Window string            `json:"window"`
	Stats  map[string]uint64 `json:"stats"`
}
type sourcePart struct {
	Index                int `json:"index"`
	Source, Cache, Patch checkpoint.Artifact
	Anchors              checkpoint.Artifact `json:"known_affiliation_anchors"`
	Unresolved           checkpoint.Artifact `json:"unresolved_affiliations"`
	Baseline             string              `json:"baseline_sha256"`
	CacheBaseline        string              `json:"cache_baseline_sha256"`
	Build                string              `json:"build_sha256"`
}
type group struct {
	Corpus, Dimension, Value string
	Stats                    map[string]uint64
}
type sqlRow map[string]json.RawMessage

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	work := flag.String("work", "", "evaluator directory")
	audit := flag.String("audit", "", "SQL output directory")
	secondary := flag.String("secondary", "", "secondary preparation directory")
	analysis := flag.String("analysis", "", "secondary SQL output directory")
	out := flag.String("output", "", "acceptance JSON")
	pilot := flag.Bool("pilot", false, "pilot gates only")
	flag.Parse()
	if *work == "" || *audit == "" || *out == "" {
		return fmt.Errorf("missing audit arguments")
	}
	var s struct {
		Status         string          `json:"status"`
		Files          int             `json:"source_files"`
		Rows           uint64          `json:"raw_rows"`
		Patches        uint64          `json:"patch_rows"`
		SourceManifest string          `json:"source_manifest_sha256"`
		Checks         map[string]bool `json:"baseline_checks"`
		Coverage       []coverage      `json:"coverage"`
	}
	if e := checkpoint.ReadJSON(filepath.Join(*work, "summary.json"), &s); e != nil {
		return e
	}
	checks := map[string]bool{}
	check := func(name string, ok bool) error {
		checks[name] = ok
		if !ok {
			return fmt.Errorf("audit failed: %s", name)
		}
		return nil
	}
	if !*pilot {
		if e := check("complete_current_snapshot", s.Status == "full_snapshot_evaluation_complete" && s.Files == 2446 && s.Rows == 510372821 && s.SourceManifest == "10c1785b4a6f9757823fc6dd005a052716e2c510ea751659cab4bcfba4c35e2a"); e != nil {
			return e
		}
	}
	for k, v := range s.Checks {
		if e := check("producer_baseline/"+k, v); e != nil {
			return e
		}
	}
	get := func(c, w string) map[string]uint64 {
		for _, x := range s.Coverage {
			if x.Corpus == c && x.Window == w {
				return x.Stats
			}
		}
		return map[string]uint64{}
	}
	var invariants []sqlRow
	if e := checkpoint.ReadJSON(filepath.Join(*audit, "patch-invariants.json"), &invariants); e != nil {
		return e
	}
	if len(invariants) != 1 {
		return fmt.Errorf("invariant output shape")
	}
	for k, v := range invariants[0] {
		n, e := u(v)
		if e != nil {
			return e
		}
		want := uint64(0)
		if k == "patch_rows" || k == "unique_work_ids" {
			want = s.Patches
		}
		if e = check("patch/"+k, n == want); e != nil {
			return e
		}
	}
	var cached []sqlRow
	if e := checkpoint.ReadJSON(filepath.Join(*audit, "cache-patch-validation.json"), &cached); e != nil {
		return e
	}
	if len(cached) != 1 {
		return fmt.Errorf("cache validation shape")
	}
	for k, v := range cached[0] {
		n, e := u(v)
		if e != nil {
			return e
		}
		want := s.Patches
		if k == "invalid_patch_baseline" {
			want = 0
		}
		if e = check("independent_cache_join/"+k, n == want); e != nil {
			return e
		}
	}
	var outputs []sqlRow
	if e := checkpoint.ReadJSON(filepath.Join(*audit, "patch-output-counts.json"), &outputs); e != nil {
		return e
	}
	mapping := map[string]string{"patch_rows": "patch_rows", "newly_known": "newly_known_combined", "newly_known_institution": "newly_known_institution", "newly_known_text": "newly_known_text", "newly_known_both": "newly_known_both", "already_known_extended": "already_known_extended", "unknown_review_only": "unknown_with_review_only", "conflict_works": "works_with_conflict"}
	totals := map[string]uint64{}
	for _, r := range outputs {
		corpus := "core"
		var xp bool
		if e := json.Unmarshal(r["is_xpac"], &xp); e != nil {
			return e
		}
		if xp {
			corpus = "xpac"
		}
		expected := get(corpus, "2000_2026")
		for k, key := range mapping {
			n, e := u(r[k])
			if e != nil {
				return e
			}
			if e = check("sql_output/"+corpus+"/"+key, n == expected[key]); e != nil {
				return e
			}
			totals[key] += n
		}
	}
	for k, n := range totals {
		if e := check("sql_output/all/"+k, n == get("all", "2000_2026")[k]); e != nil {
			return e
		}
	}
	var groups []group
	if e := checkpoint.ReadJSON(filepath.Join(*work, "groups.json"), &groups); e != nil {
		return e
	}
	byYear := map[string]map[string]uint64{}
	for _, g := range groups {
		if g.Dimension == "year" && g.Corpus != "all" {
			byYear[g.Corpus+"/"+g.Value] = g.Stats
		}
	}
	var years []sqlRow
	if e := checkpoint.ReadJSON(filepath.Join(*audit, "patch-year-counts.json"), &years); e != nil {
		return e
	}
	yearMap := map[string]map[string]uint64{}
	ymap := map[string]string{"newly_known": "newly_known_combined", "institution": "newly_known_institution", "text": "newly_known_text", "both": "newly_known_both", "known_extensions": "already_known_extended"}
	for _, r := range years {
		var xp bool
		var year int
		if e := json.Unmarshal(r["is_xpac"], &xp); e != nil {
			return e
		}
		if e := json.Unmarshal(r["year"], &year); e != nil {
			return e
		}
		c := "core"
		if xp {
			c = "xpac"
		}
		key := c + "/" + strconv.Itoa(year)
		v := map[string]uint64{}
		for sqlKey, goKey := range ymap {
			n, e := u(r[sqlKey])
			if e != nil {
				return e
			}
			v[goKey] = n
		}
		yearMap[key] = v
	}
	for key, expected := range byYear {
		for _, field := range ymap {
			if e := check("sql_year/"+key+"/"+field, yearMap[key][field] == expected[field]); e != nil {
				return e
			}
		}
	}
	var parts []sourcePart
	if e := checkpoint.ReadJSON(filepath.Join(*work, "source-patch-manifest.json"), &parts); e != nil {
		return e
	}
	if e := check("source_part_count", len(parts) == s.Files); e != nil {
		return e
	}
	seen := map[int]bool{}
	var artifacts []checkpoint.Artifact
	var rawRows uint64
	builds := map[string]int{}
	for _, p := range parts {
		if seen[p.Index] || len(p.Source.SHA256) != 64 || len(p.Baseline) != 64 || p.Baseline != p.CacheBaseline {
			return fmt.Errorf("source identity invalid %d", p.Index)
		}
		seen[p.Index] = true
		var meta struct {
			Rows  uint64 `json:"raw_rows"`
			ModNS int64  `json:"source_mod_ns"`
		}
		if e := checkpoint.ReadJSON(filepath.Join(*work, "parts", fmt.Sprintf("part-%06d.json", p.Index)), &meta); e != nil {
			return e
		}
		st, e := os.Stat(p.Source.Path)
		if e != nil {
			return e
		}
		if e = check(fmt.Sprintf("source_stat/%d", p.Index), st.Size() == p.Source.Bytes && st.ModTime().UnixNano() == meta.ModNS); e != nil {
			return e
		}
		rawRows += meta.Rows
		builds[p.Build]++
		artifacts = append(artifacts, p.Patch, p.Anchors, p.Unresolved)
	}
	if e := check("source_row_sum", rawRows == s.Rows); e != nil {
		return e
	}
	jobs := make(chan int)
	errs := make([]error, len(artifacts))
	var wg sync.WaitGroup
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				errs[i] = artifacts[i].Verify()
			}
		}()
	}
	for i := range artifacts {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			return fmt.Errorf("artifact %s: %w", artifacts[i].Path, e)
		}
	}
	if e := check("all_patch_anchor_unresolved_sha256", true); e != nil {
		return e
	}
	var refs []checkpoint.Artifact
	for _, path := range []string{filepath.Join(*work, "summary.json"), filepath.Join(*work, "completion.json"), filepath.Join(*work, "source-patch-manifest.json"), filepath.Join(*audit, "patch-invariants.json"), filepath.Join(*audit, "cache-patch-validation.json"), filepath.Join(*audit, "patch-output-counts.json"), filepath.Join(*audit, "patch-year-counts.json")} {
		a, e := checkpoint.Hash(path)
		if e != nil {
			return e
		}
		refs = append(refs, a)
	}
	if !*pilot {
		var citations struct {
			Status  string `json:"status"`
			Reports []struct {
				Corpus string            `json:"corpus"`
				Total  map[string]uint64 `json:"totals"`
			} `json:"reports"`
		}
		path := filepath.Join(*work, "citation-summary.json")
		if e := checkpoint.ReadJSON(path, &citations); e != nil {
			return e
		}
		if e := check("citation_coverage_passed", citations.Status == "passed" && len(citations.Reports) == 2); e != nil {
			return e
		}
		for _, r := range citations.Reports {
			if e := check("citation_conservation/"+r.Corpus, r.Total["newly_known_combined_events"] == r.Total["newly_known_institution_events"]+r.Total["newly_known_text_events"]-r.Total["newly_known_both_events"] && r.Total["newly_known_combined_events"] <= r.Total["baseline_missing_country_events"]); e != nil {
				return e
			}
		}
		a, e := checkpoint.Hash(path)
		if e != nil {
			return e
		}
		refs = append(refs, a)
	}
	if *analysis != "" {
		var inv []sqlRow
		if e := checkpoint.ReadJSON(filepath.Join(*analysis, "secondary-invariants.json"), &inv); e != nil {
			return e
		}
		if len(inv) != 1 {
			return fmt.Errorf("secondary invariant shape")
		}
		rows, e := u(inv[0]["secondary_rows"])
		if e != nil {
			return e
		}
		unique, e := u(inv[0]["unique_ids"])
		if e != nil {
			return e
		}
		overlap, e := u(inv[0]["overlap_with_v1_newly_known"])
		if e != nil {
			return e
		}
		invalid, e := u(inv[0]["invalid_secondary_rows"])
		if e != nil {
			return e
		}
		if e = check("secondary_unique_unapplied_disjoint", rows == unique && overlap == 0 && invalid == 0); e != nil {
			return e
		}
		if !*pilot {
			var conditional struct {
				Status string `json:"status"`
			}
			path := filepath.Join(*secondary, "conditional-citation-summary.json")
			if e = checkpoint.ReadJSON(path, &conditional); e != nil {
				return e
			}
			if e = check("secondary_conditional_citations_passed", conditional.Status == "passed"); e != nil {
				return e
			}
			a, e := checkpoint.Hash(path)
			if e != nil {
				return e
			}
			refs = append(refs, a)
		}
	}
	status := "passed"
	if *pilot {
		status = "pilot_passed"
	}
	return checkpoint.JSON(*out, map[string]any{"status": status, "checks": checks, "checks_passed": len(checks), "source_parts": s.Files, "raw_rows": s.Rows, "patch_rows": s.Patches, "sidecar_artifacts_sha256_verified": len(artifacts), "part_build_counts": builds, "evidence": refs, "human_review_completed": false, "production_mutated": false, "raw_sha_interpretation": "Complete raw SHA256 was computed during each evaluator source read (and rechecked for imported legacy parts). Acceptance rechecks current raw size/mtime and every sealed sidecar SHA; it does not claim a second complete raw rescan.", "completed_at": time.Now().UTC()})
}
func u(b json.RawMessage) (uint64, error) { var n uint64; e := json.Unmarshal(b, &n); return n, e }
