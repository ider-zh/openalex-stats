// Run isolated country-volume/network-support diagnostics; never rewrite models.
package main

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	prod "openalexstats/internal/eci"
	diag "openalexstats/research/small-country-complexity-v1/diagnostic"
)

type input struct {
	Corpus string      `json:"corpus"`
	Year   int         `json:"year"`
	Scheme string      `json:"scheme"`
	Level  string      `json:"level"`
	Metric string      `json:"metric"`
	Matrix prod.Matrix `json:"matrix"`
	WholeP []float64   `json:"whole_publications"`
	WholeC []float64   `json:"whole_citations"`
	FracP  []float64   `json:"fractional_publications"`
	FracC  []float64   `json:"fractional_citations"`
	Base   prod.Result `json:"baseline_spectral"`
	Java   prod.Result `json:"baseline_java"`
}
type config struct {
	Kind  string  `json:"kind"`
	Value float64 `json:"parameter"`
}
type job struct {
	Input  int
	Config config
}
type output struct {
	Report map[string]any
	Rows   [][]string
}

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func save(p string, v any) {
	b, e := json.MarshalIndent(v, "", "  ")
	must(e)
	must(os.WriteFile(p, append(b, '\n'), 0600))
}
func scores(r prod.Result) map[string]float64 {
	m := map[string]float64{}
	if r.Status != "ok" {
		return m
	}
	for x, i := range r.ActiveCountries {
		m[r.Countries[i]] = r.ECI[x]
	}
	return m
}
func ranks(s map[string]float64) map[string]int {
	r := map[string]int{}
	for id, v := range s {
		n := 1
		for _, w := range s {
			if w > v {
				n++
			}
		}
		r[id] = n
	}
	return r
}
func averageRanks(s map[string]float64) map[string]float64 {
	ids := make([]string, 0, len(s))
	for id := range s {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return s[ids[i]] > s[ids[j]] })
	out := map[string]float64{}
	for i := 0; i < len(ids); {
		j := i + 1
		for j < len(ids) && s[ids[j]] == s[ids[i]] {
			j++
		}
		v := float64(i+j+1) / 2
		for k := i; k < j; k++ {
			out[ids[k]] = v
		}
		i = j
	}
	return out
}
func correlation(a, b map[string]float64) (float64, bool) {
	ids := []string{}
	for id := range a {
		if _, ok := b[id]; ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) < 3 {
		return 0, false
	}
	x, y := 0., 0.
	for _, id := range ids {
		x += a[id]
		y += b[id]
	}
	x /= float64(len(ids))
	y /= float64(len(ids))
	xx, yy, xy := 0., 0., 0.
	for _, id := range ids {
		dx, dy := a[id]-x, b[id]-y
		xx += dx * dx
		yy += dy * dy
		xy += dx * dy
	}
	if xx == 0 || yy == 0 {
		return 0, false
	}
	return xy / math.Sqrt(xx*yy), true
}
func numeric(s map[string]float64, id string) string {
	if v, ok := s[id]; ok {
		return strconv.FormatFloat(v, 'g', 17, 64)
	}
	return ""
}
func order(r map[string]int, id string) string {
	if v, ok := r[id]; ok {
		return strconv.Itoa(v)
	}
	return ""
}
func identity(in input) map[string]any {
	return map[string]any{"corpus": in.Corpus, "year": in.Year, "scheme": in.Scheme, "level": in.Level, "metric": in.Metric}
}
func scale(in input) ([]float64, []float64) {
	if in.Metric == "citations" {
		return in.WholeC, in.FracC
	}
	return in.WholeP, in.FracP
}
func verify(in input, r prod.Result, j prod.Result) {
	if r.Status != in.Base.Status || j.Status != in.Java.Status {
		panic("baseline status mismatch")
	}
	for i, row := range r.Advantage {
		for k, v := range row {
			if v != in.Base.Advantage[i][k] {
				panic("baseline binary network mismatch")
			}
		}
	}
	for _, pair := range [][2]prod.Result{{in.Base, r}, {in.Java, j}} {
		a, b := scores(pair[0]), scores(pair[1])
		if len(a) != len(b) {
			panic("baseline valid set mismatch")
		}
		factor := 1.
		if c, ok := correlation(a, b); ok && c < 0 {
			factor = -1
		}
		for id, v := range a {
			w, ok := b[id]
			if !ok || math.Abs(v-factor*w) > 1e-6 {
				panic(fmt.Sprintf("baseline score mismatch %s %g %g", id, v, w))
			}
		}
	}
}
func run(in input, cfg config) []output {
	whole, frac := scale(in)
	m := in.Matrix
	m.Values = append([][]float64(nil), in.Matrix.Values...)
	included := make([]bool, len(m.Countries))
	for i := range included {
		included[i] = true
	}
	c := diag.Config{RCAThreshold: 1, MinimumDiversity: 1}
	switch cfg.Kind {
	case "country_scale_recompute_global_margins":
		for i, n := range whole {
			if n < cfg.Value {
				included[i] = false
				m.Values[i] = make([]float64, len(m.Disciplines))
			}
		}
	case "cell_support_fixed_global_rca":
		c.MinimumCellSupport = cfg.Value
	case "rca_threshold":
		c.RCAThreshold = cfg.Value
	case "minimum_diversity_fixed_global_rca":
		c.MinimumDiversity = int(cfg.Value)
	}
	r := in.Base
	j := in.Java
	if cfg.Kind != "display_only" {
		var e error
		r, e = diag.Compute(m, c)
		must(e)
		j, _, e = prod.ReflectionsFromPrepared(r, prod.DefaultReflectionConfig())
		must(e)
	}
	if cfg.Kind == "baseline" {
		verify(in, r, j)
	}
	outputs := []output{}
	for _, method := range []string{"spectral", "java_reflections"} {
		ref, res := in.Base, r
		if method == "java_reflections" {
			ref, res = in.Java, j
		}
		bs, ns := scores(ref), scores(res)
		if cfg.Kind == "display_only" {
			for i, id := range m.Countries {
				if whole[i] < cfg.Value {
					included[i] = false
					delete(ns, id)
				}
			}
		}
		raw := map[string]float64{}
		for id, v := range ns {
			raw[id] = v
		}
		factor := 1.
		rc, ok := correlation(bs, ns)
		if ok && rc < 0 {
			factor = -1
		}
		for id, v := range ns {
			ns[id] = factor * v
		}
		br, nr := ranks(bs), ranks(ns)
		rawr := ranks(raw)
		totalMass, includedMass, activeMass := 0., 0., 0.
		removed := 0
		byCode := map[string]int{}
		for i, id := range m.Countries {
			byCode[id] = i
			totalMass += frac[i]
			if included[i] {
				includedMass += frac[i]
			}
			if _, ok := ns[id]; ok {
				activeMass += frac[i]
			}
			if _, ok := bs[id]; ok {
				if _, kept := ns[id]; !kept {
					removed++
				}
			}
		}
		ids := []string{}
		for id := range ns {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			if nr[ids[i]] != nr[ids[j]] {
				return nr[ids[i]] < nr[ids[j]]
			}
			return ids[i] < ids[j]
		})
		top := []map[string]any{}
		sizes := []float64{}
		small := map[string]int{"below10": 0, "below100": 0, "below1000": 0, "below10000": 0}
		for _, id := range ids {
			if nr[id] > 10 {
				continue
			}
			i := byCode[id]
			v := whole[i]
			sizes = append(sizes, v)
			for _, t := range []float64{10, 100, 1000, 10000} {
				if v < t {
					small["below"+strconv.Itoa(int(t))]++
				}
			}
			top = append(top, map[string]any{"country": id, "rank": nr[id], "before_rank": br[id], "raw_after_rank": rawr[id], "score": ns[id], "whole_publications": in.WholeP[i], "whole_citation_events": in.WholeC[i], "fractional_publications": in.FracP[i], "fractional_citation_events": in.FracC[i]})
		}
		sort.Float64s(sizes)
		median := 0.
		if len(sizes) > 0 {
			median = sizes[len(sizes)/2]
			if len(sizes)%2 == 0 {
				median = (median + sizes[len(sizes)/2-1]) / 2
			}
		}
		commonB, commonN := map[string]float64{}, map[string]float64{}
		for id, b := range br {
			if n, ok := nr[id]; ok {
				commonB[id] = float64(b)
				commonN[id] = float64(n)
			}
		}
		rankCorr, rankOK := correlation(commonB, commonN)
		commonScoresB, commonScoresN := map[string]float64{}, map[string]float64{}
		for id := range commonB {
			commonScoresB[id] = bs[id]
			commonScoresN[id] = ns[id]
		}
		spearman, spearmanOK := correlation(averageRanks(commonScoresB), averageRanks(commonScoresN))
		summary := identity(in)
		summary["kind"] = cfg.Kind
		summary["parameter"] = cfg.Value
		summary["method"] = method
		summary["status"] = res.Status
		summary["baseline_active_countries"] = len(bs)
		summary["active_countries"] = len(ns)
		summary["removed_previously_ranked_countries"] = removed
		summary["component_count"] = res.ComponentCount
		summary["eigen_gap"] = res.EigenGap
		summary["unstable"] = res.Unstable
		summary["comparison_only_multiplier"] = factor
		summary["common_country_count"] = len(commonB)
		summary["raw_score_correlation"] = nil
		summary["matched_rank_correlation"] = nil
		summary["matched_rank_correlation_definition"] = "Pearson of global competition ranks on common countries; differs from Spearman after changes in ranking universe"
		summary["common_country_spearman"] = nil
		if ok {
			summary["raw_score_correlation"] = rc
		}
		if rankOK {
			summary["matched_rank_correlation"] = rankCorr
		}
		if spearmanOK {
			summary["common_country_spearman"] = spearman
		}
		summary["weak_cross_version_correspondence"] = !ok || math.Abs(rc) < .8
		summary["total_known_country_fractional_attribution_mass"] = totalMass
		summary["included_row_fractional_attribution_mass"] = includedMass
		summary["ranked_country_fractional_attribution_mass"] = activeMass
		summary["included_fraction"] = 0.
		summary["ranked_fraction"] = 0.
		if totalMass > 0 {
			summary["included_fraction"] = includedMass / totalMass
			summary["ranked_fraction"] = activeMass / totalMass
		}
		summary["top10_including_ties"] = top
		summary["top10_small_scale_counts"] = small
		summary["top10_conventional_median_whole_metric_scale"] = median
		edges, changed := 0, 0
		supportMass, matrixMass := 0., 0.
		for i, row := range res.Advantage {
			for k, v := range row {
				matrixMass += in.Matrix.Values[i][k]
				if v {
					edges++
					supportMass += in.Matrix.Values[i][k]
				}
				if v != in.Base.Advantage[i][k] {
					changed++
				}
			}
		}
		summary["binary_edges"] = edges
		summary["changed_binary_edges"] = changed
		summary["M_edge_fractional_input_mass"] = supportMass
		summary["all_classified_fractional_input_mass"] = matrixMass
		major := map[string]any{}
		for _, id := range []string{"CN", "US", "GB", "DE", "FR", "JP", "IN", "CA", "KR", "SG", "KP", "AG", "EH", "PR", "SX", "MQ"} {
			v := map[string]any{"before_rank": nil, "after_rank": nil, "after_valid": false}
			if k, ok := br[id]; ok {
				v["before_rank"] = k
			}
			if _, ok := ns[id]; ok {
				v["after_valid"] = true
				v["after_rank"] = nr[id]
			}
			major[id] = v
		}
		summary["selected_country_rank_examples"] = major
		pcis := []map[string]any{}
		if res.Status == "ok" {
			for x, k := range res.ActiveDisciplines {
				pcis = append(pcis, map[string]any{"discipline": res.Disciplines[k], "matched_score": factor * res.PCI[x]})
			}
			sort.Slice(pcis, func(i, j int) bool { return pcis[i]["matched_score"].(float64) > pcis[j]["matched_score"].(float64) })
		}
		summary["coupled_discipline_scores"] = pcis
		rows := [][]string{}
		for i, id := range m.Countries {
			rows = append(rows, []string{in.Corpus, strconv.Itoa(in.Year), in.Scheme, in.Level, in.Metric, cfg.Kind, strconv.FormatFloat(cfg.Value, 'g', -1, 64), method, id, numeric(bs, id), numeric(raw, id), numeric(ns, id), order(br, id), order(rawr, id), order(nr, id), strconv.FormatFloat(in.WholeP[i], 'g', 17, 64), strconv.FormatFloat(in.WholeC[i], 'g', 17, 64), strconv.FormatFloat(in.FracP[i], 'g', 17, 64), strconv.FormatFloat(in.FracC[i], 'g', 17, 64), strconv.FormatFloat(factor, 'g', -1, 64), res.Status})
		}
		outputs = append(outputs, output{summary, rows})
	}
	return outputs
}
func main() {
	path := flag.String("input", "", "frozen32-matrix input")
	out := flag.String("out", "", "new output dir")
	workers := flag.Int("workers", 32, "parallel independent model jobs")
	flag.Parse()
	if *path == "" || *out == "" || *workers < 1 {
		panic("arguments required")
	}
	if _, e := os.Stat(*out); !os.IsNotExist(e) {
		panic("output exists")
	}
	b, e := os.ReadFile(*path)
	must(e)
	var inputs []input
	must(json.Unmarshal(b, &inputs))
	if len(inputs) != 32 {
		panic("expected32 frozen model inputs")
	}
	// Baseline reproduction is required before any sensitivity experiment.
	for _, in := range inputs {
		run(in, config{"baseline", 0})
	}
	cases := []config{{"baseline", 0}}
	for _, t := range []float64{10, 100, 1000, 10000} {
		cases = append(cases, config{"display_only", t}, config{"country_scale_recompute_global_margins", t})
	}
	for _, t := range []float64{5, 10, 50} {
		cases = append(cases, config{"cell_support_fixed_global_rca", t})
	}
	for _, t := range []float64{.5, 1.5, 2} {
		cases = append(cases, config{"rca_threshold", t})
	}
	for _, t := range []float64{2, 3, 5} {
		cases = append(cases, config{"minimum_diversity_fixed_global_rca", t})
	}
	jobs := make(chan job)
	reports := make(chan []output, len(inputs)*len(cases))
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				reports <- run(inputs[j.Input], j.Config)
			}
		}()
	}
	go func() {
		for i := range inputs {
			for _, cfg := range cases {
				jobs <- job{i, cfg}
			}
		}
		close(jobs)
		wg.Wait()
		close(reports)
	}()
	var all []output
	for group := range reports {
		all = append(all, group...)
	}
	sort.Slice(all, func(i, j int) bool {
		a, _ := json.Marshal(all[i].Report)
		b, _ := json.Marshal(all[j].Report)
		return string(a) < string(b)
	})
	must(os.MkdirAll(*out, 0700))
	f, e := os.Create(filepath.Join(*out, "all-country-diagnostic-ranks.csv"))
	must(e)
	w := csv.NewWriter(f)
	must(w.Write([]string{"corpus", "year", "scheme", "level", "metric", "kind", "parameter", "method", "country", "before_score", "raw_after_score", "matched_after_score", "before_rank", "raw_after_rank", "matched_after_rank", "whole_publications", "whole_citation_events", "fractional_publications", "fractional_citation_events", "comparison_only_multiplier", "status"}))
	var summaries []map[string]any
	for _, r := range all {
		summaries = append(summaries, r.Report)
		for _, row := range r.Rows {
			must(w.Write(row))
		}
	}
	w.Flush()
	must(w.Error())
	must(f.Close())
	save(filepath.Join(*out, "experiments.json"), summaries)
	save(filepath.Join(*out, "manifest.json"), map[string]any{"status": "completed_isolated_diagnostics", "input_path": *path, "input_sha256": hash(b), "input_matrices": len(inputs), "baseline_edge_and_up_to_sign_score_checks": "all32 passed; both methods tolerance1e-6", "cases": cases, "case_method_results": len(all), "workers": *workers, "elapsed_seconds": time.Since(start).Seconds(), "production_models_changed": false, "orientation": "Each same-year result has raw scores plus one country-correlation multiplier applied jointly to its ECI/PCI for diagnostic comparison. Not a new temporal production model; weak correlation explicitly flagged.", "coverage": "Known-country fractional attribution mass, including UNKNOWN subject via primary-field totals; not whole-country sum claimed as unique global papers. Citation-event support is not distinct cited-target paper count.", "display_only": "Scores and M unchanged, only display/ranking universe filtered.", "country_scale": "Zero low-scale original fractional rows, recompute all global RCA margins; retained coauthor country fractions not renormalized.", "cell_support": "Retain original X/RCA global margins, filter M edges by X>=support before T recompute.", "rankings": "Competition ranks, exact ties retained; no ranks when model disconnected/degenerate/empty."})
	fmt.Printf("completed matrices=%d results=%d elapsed=%.2fs out=%s\n", len(inputs), len(all), time.Since(start).Seconds(), *out)
}
