// Secondary research only: precise text hashes and typed institution geo-country
// candidates. It does not apply any candidate to production statistics.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
	"openalexstats/internal/checkpoint"
)

const version = "country-enrichment-secondary-review-v1b"

type Unresolved struct {
	ID   uint64   `parquet:"id"`
	Year int32    `parquet:"year"`
	Type string   `parquet:"type"`
	XPAC bool     `parquet:"is_xpac"`
	Raw  []string `parquet:"raw_affiliations,list"`
	IDs  []string `parquet:"institution_ids,list"`
}
type TextRow struct {
	ID   uint64 `parquet:"id"`
	Year int32  `parquet:"year"`
	Type string `parquet:"type"`
	XPAC bool   `parquet:"is_xpac"`
	Hash string `parquet:"raw_normalized_sha256"`
	Raw  string `parquet:"raw_affiliation"`
}
type Institution struct {
	ID   *string `parquet:"id"`
	Name *string `parquet:"display_name"`
	Code *string `parquet:"country_code"`
	Geo  *struct {
		Name      *string  `parquet:"country"`
		City      *string  `parquet:"city"`
		Latitude  *float64 `parquet:"latitude"`
		Longitude *float64 `parquet:"longitude"`
	} `parquet:"geo"`
}
type GeoCandidate struct {
	ID            string   `parquet:"institution_id"`
	Name          string   `parquet:"institution_name"`
	Label         string   `parquet:"geo_country_name"`
	City          string   `parquet:"city"`
	Countries     []string `parquet:"country_candidates,list"`
	MappingSource string   `parquet:"mapping_source"`
	KnownSupports uint64   `parquet:"known_code_crosswalk_institutions"`
	Latitude      *float64 `parquet:"latitude"`
	Longitude     *float64 `parquet:"longitude"`
	ReviewOnly    bool     `parquet:"review_only"`
}
type Part struct {
	Index       int                 `json:"index"`
	Input       checkpoint.Artifact `json:"unresolved_affiliations"`
	InputRows   int64               `json:"unresolved_rows"`
	Source      checkpoint.Artifact `json:"source"`
	SourceBuild string              `json:"build_sha256"`
}
type Report struct {
	Version    string              `json:"version"`
	Build      string              `json:"build_sha256"`
	Input      checkpoint.Artifact `json:"input"`
	Output     checkpoint.Artifact `json:"output"`
	InputRows  int64               `json:"input_rows"`
	OutputRows int64               `json:"output_rows"`
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	work := flag.String("work", "", "completed v1 evaluator")
	out := flag.String("output", "", "separate v1b sidecar")
	manifest := flag.String("manifest", "/mnt/hg02/openalex-snapshot/data/parquet/manifest.json", "source snapshot manifest")
	dictionary := flag.String("dictionary", "/mnt/nvme0/public/openalex-stats/results/core-concept-hits-v1/dictionary.json", "snapshot country dictionary")
	workers := flag.Int("workers", 12, "NVMe sidecar workers")
	pilot := flag.Bool("pilot", false, "allow partial evaluator for development only")
	mode := flag.String("mode", "prepare", "prepare or conditional-citations")
	analysis := flag.String("analysis-output", "", "secondary SQL output, defaults output/analysis")
	flag.Parse()
	if *work == "" || *out == "" || *workers < 1 || *workers > 24 {
		return fmt.Errorf("invalid sidecar arguments")
	}
	runtime.GOMAXPROCS(*workers)
	debug.SetMemoryLimit(32 << 30)
	ctx := context.Background()
	if e := os.MkdirAll(*out, 0755); e != nil {
		return e
	}
	unlock, e := checkpoint.LockDirectory(*out)
	if e != nil {
		return e
	}
	defer unlock()
	if *mode == "conditional-citations" {
		if *analysis == "" {
			*analysis = filepath.Join(*out, "analysis")
		}
		return conditionalCitations(*work, *out, *analysis, *workers)
	}
	var comp struct {
		Status    string                `json:"status"`
		Artifacts []checkpoint.Artifact `json:"artifacts"`
	}
	e = checkpoint.ReadJSON(filepath.Join(*work, "completion.json"), &comp)
	if !*pilot && (e != nil || comp.Status != "full_snapshot_evaluation_complete") {
		return fmt.Errorf("requires completed full v1 evaluator")
	}
	if !*pilot {
		for _, a := range comp.Artifacts {
			if e = a.Verify(); e != nil {
				return e
			}
		}
	}
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	binary, e := checkpoint.Hash(exe)
	if e != nil {
		return e
	}
	start := time.Now()
	if e = buildGeo(*manifest, *dictionary, *out); e != nil {
		return e
	}
	paths, e := filepath.Glob(filepath.Join(*work, "parts", "part-*.json"))
	if e != nil {
		return e
	}
	if !*pilot && len(paths) != 2446 {
		return fmt.Errorf("expected full 2446 source parts")
	}
	sort.Strings(paths)
	reports := make([]Report, len(paths))
	errs := make([]error, len(paths))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < min(*workers, len(paths)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if e := ctx.Err(); e != nil {
					errs[index] = e
					continue
				}
				var p Part
				if e := checkpoint.ReadJSON(paths[index], &p); e != nil {
					errs[index] = e
					continue
				}
				reports[index], errs[index] = transform(p, *out, binary.SHA256)
				if errs[index] == nil {
					_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"stage": "secondary_part", "index": p.Index, "input_rows": reports[index].InputRows, "text_rows": reports[index].OutputRows})
				}
			}
		}()
	}
	for i := range paths {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	var inputs, outputs int64
	for i, e := range errs {
		if e != nil {
			return fmt.Errorf("part %d: %w", i, e)
		}
		inputs += reports[i].InputRows
		outputs += reports[i].OutputRows
	}
	status := "full_sidecar_complete"
	if *pilot {
		status = "pilot_complete"
	}
	identity := map[string]any{"status": status, "version": version, "build": binary, "v1_work": *work, "source_parts": len(paths), "unresolved_work_rows": inputs, "unresolved_text_rows": outputs, "normalization": "Go strings.ToLower(strings.Join(strings.Fields(raw), space)); full SHA256 of normalized UTF-8; deduplicated within Work by hash", "seconds": time.Since(start).Seconds(), "production_mutated": false, "automatic_application": false, "completed_at": time.Now().UTC()}
	if e = checkpoint.JSON(filepath.Join(*out, "parts-manifest.json"), reports); e != nil {
		return e
	}
	return checkpoint.JSON(filepath.Join(*out, "preparation.json"), identity)
}
func each[T any](path string, fn func(T) error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("projection: %v", p)
		}
	}()
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil {
		return e
	}
	pf, e := parquet.OpenFile(f, s.Size(), parquet.SkipPageIndex(true), parquet.SkipBloomFilters(true))
	if e != nil {
		return e
	}
	r := parquet.NewGenericReader[T](pf)
	defer r.Close()
	buf := make([]T, 256)
	for {
		n, e := r.Read(buf)
		for _, v := range buf[:n] {
			if x := fn(v); x != nil {
				return x
			}
		}
		clear(buf[:n])
		if errors.Is(e, io.EOF) {
			return nil
		}
		if e != nil {
			return e
		}
	}
}
func norm(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func add(s []string, v string) []string {
	if v == "" {
		return s
	}
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}
func transform(p Part, out, build string) (r Report, err error) {
	path := filepath.Join(out, "texts", fmt.Sprintf("part-%06d.parquet", p.Index))
	metaPath := filepath.Join(out, "parts", fmt.Sprintf("part-%06d.json", p.Index))
	if checkpoint.ReadJSON(metaPath, &r) == nil && r.Version == version && r.Build == build && r.Input == p.Input && r.InputRows == p.InputRows && r.Output.Verify() == nil {
		return r, nil
	}
	r = Report{Version: version, Build: build, Input: p.Input}
	if e := p.Input.Verify(); e != nil {
		return r, e
	}
	r.Output, err = checkpoint.Write(path, func(w io.Writer) error {
		pw := parquet.NewGenericWriter[TextRow](w, parquet.Compression(&zstd.Codec{}), parquet.MaxRowsPerRowGroup(32768))
		pending := make([]TextRow, 0, 256)
		flush := func() error {
			if len(pending) == 0 {
				return nil
			}
			_, e := pw.Write(pending)
			clear(pending)
			pending = pending[:0]
			return e
		}
		e := each[Unresolved](p.Input.Path, func(v Unresolved) error {
			r.InputRows++
			seen := map[string]bool{}
			for _, raw := range v.Raw {
				normalized := norm(raw)
				if normalized == "" {
					continue
				}
				sum := sha256.Sum256([]byte(normalized))
				hash := hex.EncodeToString(sum[:])
				if seen[hash] {
					continue
				}
				seen[hash] = true
				r.OutputRows++
				pending = append(pending, TextRow{v.ID, v.Year, v.Type, v.XPAC, hash, raw})
				if len(pending) == cap(pending) {
					if e := flush(); e != nil {
						return e
					}
				}
			}
			return nil
		})
		if e != nil {
			_ = pw.Close()
			return e
		}
		if e = flush(); e != nil {
			return e
		}
		return pw.Close()
	})
	if err != nil {
		return r, err
	}
	if r.InputRows != p.InputRows {
		return r, fmt.Errorf("unresolved source row count changed")
	}
	err = checkpoint.JSON(metaPath, r)
	return
}

func buildGeo(manifest, dictionary, out string) error {
	var d struct {
		Countries []struct {
			ID   string `json:"id"`
			Name string `json:"display_name"`
		} `json:"countries"`
	}
	if e := checkpoint.ReadJSON(dictionary, &d); e != nil {
		return e
	}
	valid := map[string]bool{}
	names := map[string][]string{}
	for _, v := range d.Countries {
		valid[v.ID] = true
		names[norm(v.Name)] = add(names[norm(v.Name)], v.ID)
	}
	var m struct {
		Entities []struct {
			Entity string `json:"entity"`
			Files  []struct {
				URL string `json:"url"`
			} `json:"files"`
		} `json:"entities"`
	}
	if e := checkpoint.ReadJSON(manifest, &m); e != nil {
		return e
	}
	var files []string
	for _, entity := range m.Entities {
		if entity.Entity == "institutions" {
			for _, f := range entity.Files {
				p := strings.SplitN(f.URL, "/parquet/", 2)
				if len(p) != 2 {
					return fmt.Errorf("invalid institutions path")
				}
				files = append(files, filepath.Join(filepath.Dir(manifest), p[1]))
			}
		}
	}
	type support struct {
		Countries    []string `json:"countries"`
		Institutions uint64   `json:"known_code_institutions"`
		Examples     []string `json:"example_institution_ids"`
	}
	cross := map[string]support{}
	var rows []Institution
	var artifacts []checkpoint.Artifact
	for _, path := range files {
		a, e := checkpoint.Hash(path)
		if e != nil {
			return e
		}
		artifacts = append(artifacts, a)
		if e = each[Institution](path, func(v Institution) error {
			rows = append(rows, v)
			if v.Geo != nil && valid[str(v.Code)] && str(v.Geo.Name) != "" {
				key := norm(str(v.Geo.Name))
				s := cross[key]
				s.Countries = add(s.Countries, str(v.Code))
				s.Institutions++
				if len(s.Examples) < 3 {
					s.Examples = append(s.Examples, str(v.ID))
				}
				cross[key] = s
			}
			return nil
		}); e != nil {
			return e
		}
	}
	var candidates []GeoCandidate
	var unresolved []string
	for _, v := range rows {
		if valid[str(v.Code)] || v.Geo == nil || str(v.ID) == "" || str(v.Geo.Name) == "" {
			continue
		}
		key := norm(str(v.Geo.Name))
		cs := append([]string{}, names[key]...)
		support := cross[key]
		for _, c := range support.Countries {
			cs = add(cs, c)
		}
		sort.Strings(cs)
		source := "snapshot_canonical_country_name"
		if support.Institutions > 0 {
			source = "known_institution_typed_geo_country_crosswalk"
		}
		if len(cs) != 1 {
			unresolved = append(unresolved, key)
			continue
		}
		candidates = append(candidates, GeoCandidate{ID: str(v.ID), Name: str(v.Name), Label: str(v.Geo.Name), City: str(v.Geo.City), Countries: cs, MappingSource: source, KnownSupports: support.Institutions, Latitude: v.Geo.Latitude, Longitude: v.Geo.Longitude, ReviewOnly: true})
	}
	_, e := checkpoint.Write(filepath.Join(out, "geo-institution-candidates.parquet"), func(w io.Writer) error {
		pw := parquet.NewGenericWriter[GeoCandidate](w, parquet.Compression(&zstd.Codec{}))
		if _, e := pw.Write(candidates); e != nil {
			return e
		}
		return pw.Close()
	})
	if e != nil {
		return e
	}
	if e = checkpoint.JSON(filepath.Join(out, "geo-country-crosswalk.json"), cross); e != nil {
		return e
	}
	return checkpoint.JSON(filepath.Join(out, "geo-study.json"), map[string]any{"institutions_scanned": len(rows), "typed_geo_candidate_institution_ids": len(candidates), "unmapped_or_ambiguous_country_names": unresolved, "country_dictionary_sha256": mustHash(dictionary), "institution_files": artifacts, "external_geocoding_called": false, "inferred_from_brand": false, "review_only": true})
}
func mustHash(p string) checkpoint.Artifact {
	a, e := checkpoint.Hash(p)
	if e != nil {
		panic(e)
	}
	return a
}
