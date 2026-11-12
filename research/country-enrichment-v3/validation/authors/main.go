// Read original author identity and both raw-affiliation fields only on request.
package main

import (
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

type source struct {
	ID      uint64 `json:"id"`
	WorkID  string `json:"work_id"`
	RawPath string `json:"raw_path"`
	RawRow  uint64 `json:"raw_row_index"`
	Year    int32  `json:"year"`
	Type    string `json:"type"`
	XPAC    bool   `json:"is_xpac"`
}
type person struct {
	ID    *string `parquet:"id"`
	Name  *string `parquet:"display_name"`
	ORCID *string `parquet:"orcid"`
}
type aff struct {
	IDs []*string `parquet:"institution_ids,list"`
	Raw *string   `parquet:"raw_affiliation_string"`
}
type inst struct {
	ID      *string `parquet:"id"`
	Name    *string `parquet:"display_name"`
	Country *string `parquet:"country_code"`
	ROR     *string `parquet:"ror"`
}
type author struct {
	Author       *person   `parquet:"author"`
	RawName      *string   `parquet:"raw_author_name"`
	RawORCID     *string   `parquet:"raw_orcid"`
	Position     *string   `parquet:"author_position"`
	Countries    []*string `parquet:"countries,list"`
	Raw          []*string `parquet:"raw_affiliation_strings,list"`
	Affiliations []*aff    `parquet:"affiliations,list"`
	Institutions []*inst   `parquet:"institutions,list"`
}
type raw struct {
	ID      *string   `parquet:"id"`
	Year    *int32    `parquet:"publication_year"`
	Type    *string   `parquet:"type"`
	XPAC    *bool     `parquet:"is_xpac"`
	Authors []*author `parquet:"authorships,list"`
}
type authorOut struct {
	ID             string              `json:"id"`
	Name           string              `json:"name"`
	RawName        string              `json:"raw_author_name"`
	ORCID          string              `json:"orcid"`
	RawORCID       string              `json:"raw_orcid"`
	Position       string              `json:"position"`
	Countries      []string            `json:"countries"`
	Primary        []string            `json:"raw_affiliation_strings"`
	Alternate      []string            `json:"alternate_raw_affiliation_strings"`
	Union          []string            `json:"raw_affiliation_union"`
	InstitutionIDs []string            `json:"institution_ids"`
	Institutions   []map[string]string `json:"institutions"`
}
type result struct {
	source
	AuthorsLoaded  bool        `json:"authors_loaded"`
	Authors        []authorOut `json:"authors"`
	SampleSHA      string      `json:"sample_sha256"`
	SourceVerified bool        `json:"source_id_year_type_xpac_verified"`
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

var email = regexp.MustCompile(`(?i)[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}`)

func clean(s string) string { return email.ReplaceAllString(s, "[email removed]") }
func unique(a []string) []string {
	sort.Strings(a)
	out := []string{}
	for _, s := range a {
		if s != "" && (len(out) == 0 || out[len(out)-1] != s) {
			out = append(out, s)
		}
	}
	return out
}
func convert(s source, r raw, sha string) result {
	y := int32(0)
	if r.Year != nil {
		y = *r.Year
	}
	if val(r.ID) != s.WorkID || y != s.Year || val(r.Type) != s.Type || truth(r.XPAC) != s.XPAC {
		panic("original identity mismatch")
	}
	o := result{source: s, AuthorsLoaded: true, Authors: []authorOut{}, SampleSHA: sha, SourceVerified: true}
	for _, a := range r.Authors {
		if a == nil {
			continue
		}
		x := authorOut{RawName: clean(val(a.RawName)), RawORCID: val(a.RawORCID), Position: val(a.Position), Countries: []string{}, Primary: []string{}, Alternate: []string{}, Institutions: []map[string]string{}}
		if a.Author != nil {
			x.ID = val(a.Author.ID)
			x.Name = clean(val(a.Author.Name))
			x.ORCID = val(a.Author.ORCID)
		}
		for _, v := range a.Countries {
			x.Countries = append(x.Countries, val(v))
		}
		for _, v := range a.Raw {
			x.Primary = append(x.Primary, clean(val(v)))
		}
		for _, alt := range a.Affiliations {
			if alt == nil {
				continue
			}
			x.Alternate = append(x.Alternate, clean(val(alt.Raw)))
			for _, id := range alt.IDs {
				x.InstitutionIDs = append(x.InstitutionIDs, val(id))
			}
		}
		for _, in := range a.Institutions {
			if in == nil {
				continue
			}
			x.InstitutionIDs = append(x.InstitutionIDs, val(in.ID))
			x.Institutions = append(x.Institutions, map[string]string{"id": val(in.ID), "name": val(in.Name), "country_code": val(in.Country), "ror": val(in.ROR)})
		}
		x.Primary = unique(x.Primary)
		x.Alternate = unique(x.Alternate)
		x.Countries = unique(x.Countries)
		x.InstitutionIDs = unique(x.InstitutionIDs)
		x.Union = unique(append(append([]string{}, x.Primary...), x.Alternate...))
		o.Authors = append(o.Authors, x)
	}
	return o
}
func main() {
	if len(os.Args) != 4 {
		panic("authors metadata_parts_dir requests_json output_dir")
	}
	start := time.Now()
	b, e := os.ReadFile(os.Args[2])
	must(e)
	var req struct {
		IDs []uint64 `json:"ids"`
	}
	must(json.Unmarshal(b, &req))
	wanted := map[uint64]bool{}
	for _, id := range req.IDs {
		wanted[id] = true
	}
	parts, e := filepath.Glob(filepath.Join(os.Args[1], "part-*.json"))
	must(e)
	targets := map[string][]source{}
	found := map[uint64]bool{}
	sampleSHA := ""
	for _, file := range parts {
		b, e := os.ReadFile(file)
		must(e)
		var p struct {
			SHA   string   `json:"sample_sha256"`
			Works []source `json:"works"`
		}
		must(json.Unmarshal(b, &p))
		if sampleSHA == "" {
			sampleSHA = p.SHA
		}
		if sampleSHA != p.SHA {
			panic("sample mismatch")
		}
		for _, w := range p.Works {
			if wanted[w.ID] {
				if found[w.ID] {
					panic("duplicate work")
				}
				found[w.ID] = true
				targets[w.RawPath] = append(targets[w.RawPath], w)
			}
		}
	}
	if len(found) != len(wanted) {
		panic(fmt.Sprintf("Some requests not yet metadata-extracted: found %d requested %d", len(found), len(wanted)))
	}
	dir := os.Args[3]
	must(os.MkdirAll(filepath.Join(dir, "authors"), 0755))
	jobs := make(chan string)
	var mu sync.Mutex
	var all []result
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				ts := targets[path]
				sort.Slice(ts, func(i, j int) bool { return ts[i].RawRow < ts[j].RawRow })
				var pending []source
				for _, t := range ts {
					file := filepath.Join(dir, "authors", fmt.Sprintf("W%d.json", t.ID))
					if b, e := os.ReadFile(file); e == nil {
						var old result
						must(json.Unmarshal(b, &old))
						if old.SampleSHA != sampleSHA || !old.SourceVerified || old.source != t {
							panic("old source mismatch")
						}
						mu.Lock()
						all = append(all, old)
						mu.Unlock()
					} else {
						pending = append(pending, t)
					}
				}
				if len(pending) == 0 {
					continue
				}
				f, e := os.Open(path)
				must(e)
				st, e := f.Stat()
				must(e)
				pf, e := parquet.OpenFile(f, st.Size(), parquet.SkipBloomFilters(true))
				must(e)
				r := parquet.NewGenericReader[raw](pf)
				for _, t := range pending {
					must(r.SeekToRow(int64(t.RawRow)))
					buf := make([]raw, 1)
					n, e := r.Read(buf)
					if n != 1 {
						must(e)
						panic("short raw read")
					}
					o := convert(t, buf[0], sampleSHA)
					b, e := json.MarshalIndent(o, "", "  ")
					must(e)
					file := filepath.Join(dir, "authors", fmt.Sprintf("W%d.json", t.ID))
					must(os.WriteFile(file+".tmp", b, 0644))
					must(os.Rename(file+".tmp", file))
					mu.Lock()
					all = append(all, o)
					mu.Unlock()
				}
				must(r.Close())
				must(f.Close())
			}
		}()
	}
	for p := range targets {
		jobs <- p
	}
	close(jobs)
	wg.Wait()
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	b, e = json.MarshalIndent(map[string]any{"status": "passed", "sample_sha256": sampleSHA, "request_size": len(wanted), "works": all, "elapsed_seconds": time.Since(start).Seconds(), "email_substrings_removed": true}, "", "  ")
	must(e)
	base := strings.TrimSuffix(filepath.Base(os.Args[2]), filepath.Ext(os.Args[2]))
	must(os.WriteFile(filepath.Join(dir, base+"-authors.json"), b, 0644))
	fmt.Printf("authors=%d parts=%d seconds=%.2f\n", len(all), len(targets), time.Since(start).Seconds())
}
