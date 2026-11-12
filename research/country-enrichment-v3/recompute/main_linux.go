//go:build linux

// Reaggregate the complete immutable Q with a gated UNKNOWN-only country overlay.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/parquet-go/parquet-go"
	"io"
	"math"
	"openalexstats/internal/aggregate"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/methods"
	"openalexstats/internal/model"
	"openalexstats/internal/pipeline"
	"openalexstats/internal/production"
	"openalexstats/internal/results"
	"openalexstats/internal/store"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"
)

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func readJSON(p string, v any) { b, e := os.ReadFile(p); must(e); must(json.Unmarshal(b, v)) }
func writeJSON(p string, v any) {
	b, e := json.MarshalIndent(v, "", "  ")
	must(e)
	must(os.WriteFile(p, append(b, '\n'), 0600))
}

type gate struct {
	Route string `json:"route"`
	Path  string `json:"path"`
	SHA   string `json:"sha256"`
}
type overlayManifest struct {
	Status   string `json:"status"`
	PatchSHA string `json:"patch_sha256"`
	Rows     uint64 `json:"rows"`
	Gates    []gate `json:"gates"`
}
type counts struct {
	d     [][]byte
	years int
	rows  int
}

func openCounts(dir string, rows, years int) *counts {
	paths, e := filepath.Glob(filepath.Join(dir, "target-*.counts"))
	must(e)
	sort.Strings(paths)
	if len(paths) != 64 {
		panic("expected64 original counter partitions")
	}
	c := &counts{make([][]byte, 64), years, rows}
	for p, path := range paths {
		f, e := os.Open(path)
		must(e)
		st, e := f.Stat()
		must(e)
		n := int64(0)
		if p < rows {
			n = int64((rows-1-p)/64+1) * int64(years) * 8
		}
		if st.Size() != n {
			panic("counter partition shape")
		}
		if n > 0 {
			d, e := syscall.Mmap(int(f.Fd()), 0, int(n), syscall.PROT_READ, syscall.MAP_SHARED)
			must(e)
			c.d[p] = d
		}
		f.Close()
	}
	return c
}
func (c *counts) at(i int, b []uint64) {
	d := c.d[i%64]
	off := i / 64 * c.years * 8
	for j := range b {
		b[j] = binary.LittleEndian.Uint64(d[off+j*8 : off+j*8+8])
	}
}
func (c *counts) close() {
	for _, d := range c.d {
		if len(d) > 0 {
			syscall.Munmap(d)
		}
	}
}

type changed struct {
	Works, Publications, Citations uint64
	Windows                        map[string]uint64
}

func reduce(ctx context.Context, columns *store.Columns, c *counts, dict results.Dictionary, sc []results.Scenario, ps map[uint64]patch, workers int) ([]results.Input, changed) {
	valid := map[string]bool{}
	for _, r := range dict.Countries {
		valid[r.ID] = true
	}
	parts := make([]*results.Dense, workers)
	changes := make([]changed, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for p := 0; p < workers; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			d, e := results.NewDense(dict, sc, 2000, 2026)
			if e != nil {
				errs[p] = e
				return
			}
			parts[p] = d
			ch := changed{Windows: map[string]uint64{}}
			var w model.Work
			cs := make([]uint64, 27)
			for i := columns.Len() * p / workers; i < columns.Len()*(p+1)/workers; i++ {
				if i%65536 == 0 && ctx.Err() != nil {
					errs[p] = ctx.Err()
					return
				}
				columns.View(i, &w)
				c.at(i, cs)
				if q, ok := ps[w.ID]; ok {
					if e = apply(&w, q, valid); e != nil {
						errs[p] = e
						return
					}
					ch.Works++
					if w.Year >= 2000 && w.Year <= 2026 {
						ch.Publications++
						ch.Windows["2000_2026"]++
					}
					if w.Year >= 2000 && w.Year <= 2025 {
						ch.Windows["2000_2025"]++
					}
					for _, n := range cs {
						ch.Citations += n
					}
				}
				if e = d.AddSeries(w, cs); e != nil {
					errs[p] = e
					return
				}
			}
			changes[p] = ch
		}(p)
	}
	wg.Wait()
	for _, e := range errs {
		must(e)
	}
	ch := changed{Windows: map[string]uint64{}}
	for p, d := range parts {
		if p > 0 {
			must(parts[0].Merge(d))
			parts[p] = nil
		}
		ch.Works += changes[p].Works
		ch.Publications += changes[p].Publications
		ch.Citations += changes[p].Citations
		for k, n := range changes[p].Windows {
			ch.Windows[k] += n
		}
	}
	return parts[0].Inputs(), ch
}
func main() {
	base := flag.String("base", "", "immutable completed baseline result")
	dim := flag.String("dimensions", "", "same Q dimensions")
	counter := flag.String("counts", "", "original target counters")
	pp := flag.String("patch", "", "validated merged patch parquet")
	pm := flag.String("manifest", "", "validated merged patch manifest")
	out := flag.String("out", "", "new result directory")
	methodOut := flag.String("methods-out", "", "new reflections directory")
	workers := flag.Int("workers", 32, "local concurrency")
	flag.Parse()
	if *base == "" || *dim == "" || *counter == "" || *pp == "" || *pm == "" || *out == "" || *methodOut == "" || *workers < 1 || *workers > 48 {
		panic("missing/invalid args")
	}
	if _, e := os.Stat(*out); !os.IsNotExist(e) {
		panic("result directory must be new")
	}
	if _, e := os.Stat(*methodOut); !os.IsNotExist(e) {
		panic("method result directory must be new")
	}
	t := time.Now()
	var om overlayManifest
	readJSON(*pm, &om)
	if om.Status != "passed_validated_overlay" || len(om.Gates) == 0 {
		panic("overlay has no passed gate")
	}
	patchArt, e := checkpoint.Hash(*pp)
	must(e)
	if patchArt.SHA256 != om.PatchSHA {
		panic("patch SHA mismatch")
	}
	for _, g := range om.Gates {
		a, e := checkpoint.Hash(g.Path)
		must(e)
		if a.SHA256 != g.SHA {
			panic("gate SHA mismatch")
		}
		var v struct {
			Status string `json:"status"`
		}
		readJSON(g.Path, &v)
		if v.Status != "passed" {
			panic("route has not passed independent validation")
		}
	}
	var src production.Report
	readJSON(filepath.Join(*base, "run.json"), &src)
	if src.Status != "full_snapshot_complete" {
		panic("base incomplete")
	}
	var dc results.Dictionary
	var sc []results.Scenario
	readJSON(filepath.Join(*base, "dictionary.json"), &dc)
	readJSON(filepath.Join(*base, "scenarios.json"), &sc)
	_, e = results.ResolveScenarios(sc)
	must(e)
	var completion struct {
		RunID     string                `json:"run_id"`
		Run       checkpoint.Artifact   `json:"run"`
		Artifacts []checkpoint.Artifact `json:"artifacts"`
	}
	readJSON(filepath.Join(*base, "completion.json"), &completion)
	if completion.RunID != src.RunID {
		panic("base completion identity")
	}
	for _, a := range append(completion.Artifacts, completion.Run) {
		name := filepath.Base(a.Path)
		if name == "run.json" || name == "dictionary.json" || name == "scenarios.json" {
			b, e := checkpoint.Hash(filepath.Join(*base, name))
			must(e)
			if a.SHA256 != b.SHA256 {
				panic("baseline input changed")
			}
		}
	}
	// Read exactly one merged row per Work; fail on duplicate/empty records.
	f, e := os.Open(*pp)
	must(e)
	r := parquet.NewGenericReader[patch](f)
	ps := map[uint64]patch{}
	buf := make([]patch, 4096)
	var expected uint64
	for {
		n, e := r.Read(buf)
		for _, p := range buf[:n] {
			if p.ID == 0 || len(p.Countries) == 0 {
				panic("invalid patch")
			}
			if _, ok := ps[p.ID]; ok {
				panic("duplicate Work in patch")
			}
			p.Countries = append([]string(nil), p.Countries...)
			ps[p.ID] = p
			if src.Config.Scope.Corpus == "all" || !p.XPAC {
				expected++
			}
		}
		if e == io.EOF {
			break
		}
		must(e)
	}
	r.Close()
	f.Close()
	if uint64(len(ps)) != om.Rows {
		panic("overlay row count mismatch")
	}
	fmt.Printf("validated patch=%d expected_in_corpus=%d\n", len(ps), expected)
	df, e := os.Open(*dim)
	must(e)
	columns, e := store.ReadFrom(df, 300<<30)
	must(e)
	df.Close()
	if columns.Len() != src.SelectedWorks {
		panic("dimensions row count mismatch")
	}
	c := openCounts(*counter, columns.Len(), 27)
	defer c.close()
	fmt.Printf("dimensions loaded rows=%d elapsed=%.1fs\n", columns.Len(), time.Since(t).Seconds())
	inputs, ch := reduce(context.Background(), columns, c, dc, sc, ps, *workers)
	if ch.Works != expected {
		panic(fmt.Sprintf("patch not fully applied %d != %d", ch.Works, expected))
	}
	for _, in := range inputs {
		q := in.Quality
		if q.PublicationWorks != src.Quality.PublicationWorks || q.CitationEvents != src.Graph.RetainedEdges {
			panic("raw corpus or reference edge totals changed")
		}
		if q.MissingCountryPublications+ch.Publications != src.Quality.MissingCountryPublications || q.MissingCountryCitations+ch.Citations != src.Quality.MissingCountryCitations {
			panic("UNKNOWN recovery conservation mismatch")
		}
		if in.Scenario.CountryMode == "whole" {
			for _, v := range in.Rows {
				if math.Trunc(v.Publications) != v.Publications || math.Trunc(v.Citations) != v.Citations || v.Publications < 0 || v.Citations < 0 {
					panic("whole-count integrality violation")
				}
			}
		}
	}
	fmt.Printf("full aggregation done recovered=%d elapsed=%.1fs\n", ch.Works, time.Since(t).Seconds())
	var main []aggregate.Row
	for _, in := range inputs {
		if in.Scenario.ForECI() {
			main = in.Rows
		}
	}
	models, e := pipeline.ComputeModels(context.Background(), main, *workers, 2000, 2026, &dc)
	must(e)
	rows, coverage, e := results.Derive(inputs)
	must(e)
	must(results.Export(*out, rows, coverage, sc, models, dc))
	cube, e := os.Create(filepath.Join(*out, "cube.parquet"))
	must(e)
	cw := parquet.NewGenericWriter[aggregate.Row](cube)
	_, e = cw.Write(main)
	must(e)
	must(cw.Close())
	cube.Close()
	exe, e := os.Executable()
	must(e)
	code, e := checkpoint.Hash(exe)
	must(e)
	mh, e := checkpoint.Hash(*pm)
	must(e)
	rid := sha256.Sum256([]byte(src.RunID + "\n" + patchArt.SHA256 + "\n" + code.SHA256))
	report := src
	report.RunID = hex.EncodeToString(rid[:])
	report.BuildID = "country-enrichment-v3-" + code.SHA256[:16]
	report.Config.OutputDir = *out
	report.StartedAt = t
	report.CompletedAt = time.Now()
	report.Status = "full_snapshot_complete"
	report.StageSeconds = map[string]float64{"gated_country_overlay_total": time.Since(t).Seconds()}
	report.Quality = inputs[0].Quality
	for _, in := range inputs {
		if in.Scenario.ForECI() {
			report.Quality = in.Quality
		}
	}
	report.StatisticsRows = len(rows)
	report.CubeRows = len(main)
	report.Models = len(models)
	report.Notes = append(report.Notes, "Country-enriched derivative of immutable baseline; Q and original integer citation counters reused. SourceStage resources are historical; this run does not repeat source scanning or citation graph construction.")
	writeJSON(filepath.Join(*out, "run.json"), struct {
		production.Report
		Enrichment any `json:"country_enrichment"`
	}{report, map[string]any{"base_run_id": src.RunID, "base_directory": *base, "patch": patchArt, "manifest": mh, "recovered": ch, "original_version_preserved": true, "production_applied": false, "elapsed_seconds": time.Since(t).Seconds()}})
	writeJSON(filepath.Join(*out, "enrichment.json"), map[string]any{"status": "completed_gated_full_corpus_reaggregation", "base_run_id": src.RunID, "run_id": report.RunID, "gates": om.Gates, "patch": patchArt, "manifest": mh, "recovered": ch, "source_dimensions": *dim, "source_counts": *counter, "baseline_selected_works": src.SelectedWorks, "baseline_reference_events": src.Graph.RetainedEdges, "unchanged_q_and_edges": true, "production_applied": false})
	files, e := os.ReadDir(*out)
	must(e)
	var arts []checkpoint.Artifact
	var ra checkpoint.Artifact
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		a, e := checkpoint.Hash(filepath.Join(*out, f.Name()))
		must(e)
		if f.Name() == "run.json" {
			ra = a
		} else {
			arts = append(arts, a)
		}
	}
	writeJSON(filepath.Join(*out, "completion.json"), map[string]any{"version": 1, "run_id": report.RunID, "run": ra, "artifacts": arts})
	_, e = methods.Derive(context.Background(), methods.Config{Data: *out, Output: *methodOut, Workers: min(*workers, 16), BuildID: report.BuildID})
	must(e)
	fmt.Printf("complete corpus=%s run=%s seconds=%.2f\n", src.Config.Scope.Corpus, report.RunID, time.Since(t).Seconds())
}
