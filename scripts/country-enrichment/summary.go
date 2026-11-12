package main

import (
	"fmt"
	"path/filepath"
	"time"

	"openalexstats/internal/checkpoint"
)

func (e *Env) writeSummary(reports []FileReport, indices []int, started int64) error {
	groups, validation := mergeGroups(reports, indices)
	full := len(indices) == len(e.paths)
	var rows int64
	var patches uint64
	type partIdentity struct {
		Index                int `json:"index"`
		Source, Cache, Patch checkpoint.Artifact
		BaselineSHA          string              `json:"baseline_sha256"`
		CacheBaselineSHA     string              `json:"cache_baseline_sha256"`
		Anchors              checkpoint.Artifact `json:"known_affiliation_anchors"`
		Unresolved           checkpoint.Artifact `json:"unresolved_affiliations"`
		AnchorRows           int64               `json:"anchor_rows"`
		UnresolvedRows       int64               `json:"unresolved_rows"`
		BuildSHA             string              `json:"build_sha256"`
		SchemaSHA            string              `json:"output_schema_sha256,omitempty"`
		CompatibleOrigin     checkpoint.Artifact `json:"compatible_origin_checkpoint,omitempty"`
	}
	parts := make([]partIdentity, 0, len(indices))
	for _, i := range indices {
		r := reports[i]
		rows += r.RawRows
		patches += r.PatchRows()
		parts = append(parts, partIdentity{i, r.Source, r.Cache, r.Patch, r.BaselineSHA, r.CacheBaselineSHA, r.Anchors, r.Unresolved, r.AnchorRows, r.UnresolvedRows, r.BuildSHA, r.SchemaSHA, r.CompatibleOrigin})
	}
	get := func(c, w string) Stats {
		for _, g := range groups {
			if g.Corpus == c && g.Dimension == "window" && g.Value == w {
				return g.Stats
			}
		}
		return Stats{}
	}
	checks := map[string]bool{"source_rows_match_selected_files": true, "all_country_set_and_work_identity_hashes_match_production_cache": true, "source_and_patch_sha256_complete": true}
	if full {
		core, all := get("core", "2000_2026"), get("all", "2000_2026")
		checks["full_source_rows_510372821"] = rows == 510372821
		checks["core_q_269194661"] = get("core", "all_years").Works == 269194661
		checks["all_q_404079112"] = get("all", "all_years").Works == 404079112
		checks["core_publications_201267475"] = core.Works == 201267475
		checks["core_missing_country_103275459"] = core.Missing == 103275459
		checks["all_publications_285365821"] = all.Works == 285365821
		checks["all_missing_country_180207949"] = all.Missing == 180207949
	}
	for _, g := range groups {
		if g.Stats.Known+g.Stats.Missing != g.Stats.Works || g.Stats.NewCombined != g.Stats.NewInstitution+g.Stats.NewText-g.Stats.NewBoth || g.Stats.NewCombined > g.Stats.Missing {
			return fmt.Errorf("coverage accounting invariant failed %s/%s/%s", g.Corpus, g.Dimension, g.Value)
		}
	}
	for k, v := range checks {
		if !v {
			return fmt.Errorf("baseline validation failed %s", k)
		}
	}
	type coverage struct {
		Corpus          string  `json:"corpus"`
		Window          string  `json:"window"`
		Stats           Stats   `json:"stats"`
		Baseline        float64 `json:"baseline_coverage_percent"`
		Enhanced        float64 `json:"conservative_candidate_coverage_percent"`
		Gain            float64 `json:"gain_percentage_points"`
		InstitutionOnly uint64  `json:"institution_only_new_works"`
		TextOnly        uint64  `json:"text_only_new_works"`
	}
	var covers []coverage
	for _, c := range []string{"core", "xpac", "all"} {
		for _, w := range []string{"all_years", "2000_2026"} {
			s := get(c, w)
			x := coverage{Corpus: c, Window: w, Stats: s, InstitutionOnly: s.NewInstitution - s.NewBoth, TextOnly: s.NewText - s.NewBoth}
			if s.Works > 0 {
				x.Baseline = 100 * float64(s.Known) / float64(s.Works)
				x.Enhanced = 100 * float64(s.Known+s.NewCombined) / float64(s.Works)
				x.Gain = 100 * float64(s.NewCombined) / float64(s.Works)
			}
			covers = append(covers, x)
		}
	}
	status := "pilot_complete"
	if full {
		status = "full_snapshot_evaluation_complete"
	}
	summary := map[string]any{"status": status, "rule_version": ruleVersion, "rules_sha256": e.ruleSHA, "build_sha256": e.buildSHA, "source_manifest_sha256": e.manifestSHA, "source_files": len(indices), "expected_full_source_files": len(e.paths), "raw_rows": rows, "patch_rows": patches, "started_at": time.Unix(started, 0).UTC(), "completed_at": time.Now().UTC(), "baseline_checks": checks, "coverage": covers, "validation_interpretation": "Agreement with existing country tags is a consistency test, not measured human-reviewed precision. Existing tags can be incomplete. Candidate data remains review-only.", "production_mutated": false, "automatic_application": false}
	buildCounts := map[string]int{}
	for _, i := range indices {
		buildCounts[reports[i].BuildSHA]++
	}
	summary["part_build_sha256_counts"] = buildCounts
	summary["output_schema_sha256"] = e.schemaSHA
	summary["compatibility_evidence"] = e.compatibilityArtifacts
	outputs := []struct {
		name  string
		value any
	}{{"summary.json", summary}, {"groups.json", groups}, {"text-validation-by-country.json", validation}, {"source-patch-manifest.json", parts}}
	var artifacts []checkpoint.Artifact
	for _, out := range outputs {
		p := filepath.Join(e.cfg.Work, out.name)
		if er := checkpoint.JSON(p, out.value); er != nil {
			return er
		}
		a, er := checkpoint.Hash(p)
		if er != nil {
			return er
		}
		artifacts = append(artifacts, a)
	}
	for _, name := range []string{"rules.json", "identity.json"} {
		a, er := checkpoint.Hash(filepath.Join(e.cfg.Work, name))
		if er != nil {
			return er
		}
		artifacts = append(artifacts, a)
	}
	if er := checkpoint.JSON(filepath.Join(e.cfg.Work, "completion.json"), map[string]any{"status": status, "rule_version": ruleVersion, "rules_sha256": e.ruleSHA, "build_sha256": e.buildSHA, "source_files": len(indices), "raw_rows": rows, "artifacts": artifacts, "completed_at": time.Now().UTC()}); er != nil {
		return er
	}
	log(map[string]any{"stage": "complete", "status": status, "source_files": len(indices), "raw_rows": rows, "patch_rows": patches, "coverage": covers})
	return nil
}
