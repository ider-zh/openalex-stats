// Sparse original-snapshot metadata extraction for the frozen probability sample.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/parquet-go/parquet-go"
)

type record struct {
	ID          uint64  `json:"id"`
	WorkID      string  `json:"work_id"`
	Ordinal     uint64  `json:"ordinal"`
	Year        int32   `json:"year"`
	Type        string  `json:"type"`
	IsXPAC      bool    `json:"is_xpac"`
	Stratum     string  `json:"stratum"`
	Hash        string  `json:"sample_hash_u64"`
	N           uint64  `json:"stratum_population"`
	Nsample     int     `json:"stratum_sample"`
	Pi          float64 `json:"inclusion_probability"`
	Weight      float64 `json:"design_weight"`
	RawPath     string  `json:"raw_path"`
	CachePath   string  `json:"cache_path"`
	SourceIndex int     `json:"source_index"`
}
type loc struct {
	ID      *string `parquet:"id"`
	OA      *bool   `parquet:"is_oa"`
	PDF     *string `parquet:"pdf_url"`
	Landing *string `parquet:"landing_page_url"`
	License *string `parquet:"license"`
	Version *string `parquet:"version"`
}
type oa struct {
	OA     *bool   `parquet:"is_oa"`
	URL    *string `parquet:"oa_url"`
	Status *string `parquet:"oa_status"`
}
type rawMeta struct {
	ID         *string            `parquet:"id"`
	DOI        *string            `parquet:"doi"`
	Title      *string            `parquet:"display_name"`
	Year       *int32             `parquet:"publication_year"`
	Type       *string            `parquet:"type"`
	XPAC       *bool              `parquet:"is_xpac"`
	IDs        map[string]*string `parquet:"ids"`
	Locations  []*loc             `parquet:"locations,list"`
	Primary    *loc               `parquet:"primary_location"`
	Best       *loc               `parquet:"best_oa_location"`
	OpenAccess *oa                `parquet:"open_access"`
}
type location struct {
	ID      string `json:"id,omitempty"`
	IsOA    bool   `json:"is_oa"`
	PDF     string `json:"pdf_url,omitempty"`
	Landing string `json:"landing_page_url,omitempty"`
	License string `json:"license,omitempty"`
	Version string `json:"version,omitempty"`
	Role    string `json:"role"`
}
type output struct {
	record
	RawRow               uint64            `json:"raw_row_index"`
	Title                string            `json:"title"`
	DOI                  string            `json:"doi"`
	PMID                 string            `json:"pmid"`
	PMCID                string            `json:"pmcid"`
	IDs                  map[string]string `json:"snapshot_ids"`
	Locations            []location        `json:"locations"`
	IsOA                 bool              `json:"is_oa"`
	OAURL                string            `json:"oa_url"`
	OAStatus             string            `json:"oa_status"`
	BaselineCountries    []string          `json:"baseline_countries"`
	SourceIdentityPassed bool              `json:"source_identity_passed"`
}

func val(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func truth(p *bool) bool { return p != nil && *p }
func must(e error) {
	if e != nil {
		panic(e)
	}
}
func normalizeDOI(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	for _, p := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/", "doi:"} {
		s = strings.TrimPrefix(s, p)
	}
	return strings.TrimSpace(s)
}

var pmc = regexp.MustCompile(`(?i)PMC[0-9]+`)

func last(s string) string {
	s = strings.TrimRight(s, "/")
	p := strings.LastIndex(s, "/")
	if p >= 0 {
		return s[p+1:]
	}
	return s
}
func convert(t record, r rawMeta, row uint64) output {
	y := int32(0)
	if r.Year != nil {
		y = *r.Year
	}
	if val(r.ID) != t.WorkID || y != t.Year || val(r.Type) != t.Type || truth(r.XPAC) != t.IsXPAC {
		panic(fmt.Sprintf("source identity mismatch %d", t.ID))
	}
	o := output{record: t, RawRow: row, Title: val(r.Title), DOI: normalizeDOI(val(r.DOI)), IDs: map[string]string{}, BaselineCountries: []string{}, SourceIdentityPassed: true}
	for k, v := range r.IDs {
		o.IDs[k] = val(v)
	}
	if o.DOI == "" {
		o.DOI = normalizeDOI(o.IDs["doi"])
	}
	o.PMID = last(o.IDs["pmid"])
	o.PMCID = strings.ToUpper(pmc.FindString(o.IDs["pmcid"]))
	add := func(l *loc, role string) {
		if l == nil {
			return
		}
		o.Locations = append(o.Locations, location{val(l.ID), truth(l.OA), val(l.PDF), val(l.Landing), val(l.License), val(l.Version), role})
	}
	add(r.Primary, "primary")
	add(r.Best, "best_oa")
	for _, l := range r.Locations {
		add(l, "location")
	}
	if r.OpenAccess != nil {
		o.IsOA = truth(r.OpenAccess.OA)
		o.OAURL = val(r.OpenAccess.URL)
		o.OAStatus = val(r.OpenAccess.Status)
	}
	return o
}
func main() {
	if len(os.Args) != 4 {
		panic("extract frozen_sample_json dimension_parts output_dir")
	}
	started := time.Now()
	b, e := os.ReadFile(os.Args[1])
	must(e)
	digest := sha256.Sum256(b)
	var in struct {
		Works []record `json:"works"`
	}
	must(json.Unmarshal(b, &in))
	groups := map[string][]record{}
	for _, w := range in.Works {
		groups[w.RawPath] = append(groups[w.RawPath], w)
	}
	dir := os.Args[3]
	must(os.MkdirAll(filepath.Join(dir, "metadata-parts"), 0755))
	jobs := make(chan string)
	var mu sync.Mutex
	var all []output
	done := 0
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				targets := groups[path]
				idx := targets[0].SourceIndex
				targetFile := filepath.Join(dir, "metadata-parts", fmt.Sprintf("part-%05d.json", idx))
				var rows []output
				if saved, e := os.ReadFile(targetFile); e == nil {
					var old struct {
						SampleSHA string   `json:"sample_sha256"`
						Works     []output `json:"works"`
					}
					must(json.Unmarshal(saved, &old))
					if old.SampleSHA != hex.EncodeToString(digest[:]) {
						panic("checkpoint sample mismatch")
					}
					rows = old.Works
				} else {
					wanted := map[uint64]record{}
					for _, t := range targets {
						wanted[t.ID] = t
					}
					ids, e := os.ReadFile(filepath.Join(os.Args[2], fmt.Sprintf("part-%05d.ids", idx)))
					must(e)
					n := binary.LittleEndian.Uint64(ids[:8])
					if uint64(len(ids)) != 8+8*n {
						panic("raw ids shape")
					}
					type pos struct {
						t   record
						row uint64
					}
					var positions []pos
					for j := uint64(0); j < n; j++ {
						id := binary.LittleEndian.Uint64(ids[8+j*8 : 16+j*8])
						if t, ok := wanted[id]; ok {
							positions = append(positions, pos{t, j})
						}
					}
					if len(positions) != len(targets) {
						panic("physical rows missing")
					}
					ids = nil
					f, e := os.Open(path)
					must(e)
					st, e := f.Stat()
					must(e)
					pf, e := parquet.OpenFile(f, st.Size(), parquet.SkipBloomFilters(true))
					must(e)
					r := parquet.NewGenericReader[rawMeta](pf)
					for _, p := range positions {
						must(r.SeekToRow(int64(p.row)))
						buf := make([]rawMeta, 1)
						n, e := r.Read(buf)
						if n != 1 {
							must(e)
							panic("short sparse read")
						}
						rows = append(rows, convert(p.t, buf[0], p.row))
					}
					must(r.Close())
					must(f.Close())
					b, e := json.Marshal(map[string]any{"status": "passed", "sample_sha256": hex.EncodeToString(digest[:]), "raw_path": path, "works": rows})
					must(e)
					must(os.WriteFile(targetFile+".tmp", b, 0644))
					must(os.Rename(targetFile+".tmp", targetFile))
				}
				if len(rows) != len(targets) {
					panic("checkpoint row count")
				}
				mu.Lock()
				all = append(all, rows...)
				done++
				if done%50 == 0 {
					fmt.Printf("parts=%d/%d works=%d elapsed=%.1f\n", done, len(groups), len(all), time.Since(started).Seconds())
				}
				mu.Unlock()
			}
		}()
	}
	for p := range groups {
		jobs <- p
	}
	close(jobs)
	wg.Wait()
	if len(all) != len(in.Works) {
		panic("sample count mismatch")
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	var withDOI, withPMID, withPMCID, withOA int
	for _, w := range all {
		if w.DOI != "" {
			withDOI++
		}
		if w.PMID != "" {
			withPMID++
		}
		if w.PMCID != "" {
			withPMCID++
		}
		if w.IsOA {
			withOA++
		}
	}
	b, e = json.MarshalIndent(map[string]any{"status": "metadata_extraction_passed", "sample_sha256": hex.EncodeToString(digest[:]), "sample_size": len(all), "source_parts": len(groups), "all_source_id_year_type_xpac_match": true, "with_doi": withDOI, "with_pmid": withPMID, "with_pmcid": withPMCID, "with_oa_flag": withOA, "elapsed_seconds": time.Since(started).Seconds(), "works": all}, "", "  ")
	must(e)
	must(os.WriteFile(filepath.Join(dir, "sample-metadata.json"), b, 0644))
	fmt.Printf("DONE n=%d DOI=%d PMID=%d PMC=%d OA=%d seconds=%.2f\n", len(all), withDOI, withPMID, withPMCID, withOA, time.Since(started).Seconds())
}
