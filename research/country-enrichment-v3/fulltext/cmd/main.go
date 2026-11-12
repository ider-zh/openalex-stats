// country-fulltext-pilot is an isolated, review-only stratified experiment.
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Location struct {
	IsOA    bool   `json:"is_oa"`
	PDF     string `json:"pdf_url"`
	Landing string `json:"landing_page_url"`
	License string `json:"license"`
	Version string `json:"version"`
}
type Work struct {
	ID        uint64     `json:"id"`
	WorkID    string     `json:"work_id"`
	Stratum   string     `json:"stratum"`
	Weight    float64    `json:"design_weight"`
	PI        float64    `json:"inclusion_probability"`
	Title     string     `json:"title"`
	DOI       string     `json:"doi"`
	PMID      string     `json:"pmid"`
	PMCID     string     `json:"pmcid"`
	Year      int        `json:"year"`
	XPAC      bool       `json:"is_xpac"`
	Type      string     `json:"type"`
	OAURL     string     `json:"oa_url"`
	IsOA      bool       `json:"is_oa"`
	Locations []Location `json:"locations"`
}
type Sample struct {
	Works     []Work `json:"works"`
	SampleSHA string `json:"sample_sha256"`
}
type PMCMatch struct {
	ID        uint64   `json:"id"`
	PMCID     string   `json:"pmcid"`
	DOIMatch  bool     `json:"doi_match"`
	PMIDMatch bool     `json:"pmid_match"`
	Keys      []string `json:"inventory_metadata_keys"`
}
type Plan struct {
	Status        string       `json:"status"`
	Mode          string       `json:"mode"`
	Seed          string       `json:"seed"`
	SampleSHA     string       `json:"sample_sha256"`
	SampleSize    int          `json:"sample_size"`
	ParentPlanSHA string       `json:"parent_plan_sha256,omitempty"`
	PMC           []PMCMatch   `json:"pmc"`
	PDF           []PDFPick    `json:"pdf"`
	PDFEligible   []PDFPick    `json:"pdf_eligible_frame,omitempty"`
	PDFStrata     []PDFStratum `json:"pdf_strata"`
}
type PDFPick struct {
	ID      uint64  `json:"id"`
	URL     string  `json:"url"`
	License string  `json:"license"`
	Stratum string  `json:"stratum"`
	Hash    string  `json:"hash"`
	PI2     float64 `json:"pi2"`
}
type PDFStratum struct {
	Stratum  string  `json:"stratum"`
	Eligible int     `json:"eligible"`
	Selected int     `json:"selected"`
	PI2      float64 `json:"pi2"`
}
type Fetch struct {
	URL         string  `json:"url"`
	Status      int     `json:"http_status"`
	Bytes       int     `json:"bytes"`
	SHA         string  `json:"sha256"`
	Path        string  `json:"path"`
	Seconds     float64 `json:"seconds"`
	Error       string  `json:"error,omitempty"`
	ContentType string  `json:"content_type"`
	Cached      bool    `json:"cached"`
}
type Outcome struct {
	ID                 uint64   `json:"id"`
	WorkID             string   `json:"work_id"`
	Stratum            string   `json:"stratum"`
	Weight             float64  `json:"design_weight"`
	PI1                float64  `json:"pi1"`
	PI2                float64  `json:"pi2"`
	Route              string   `json:"route"`
	Status             string   `json:"status"`
	Source             Fetch    `json:"source"`
	Metadata           *Fetch   `json:"metadata,omitempty"`
	Parsed             *Parsed  `json:"parsed,omitempty"`
	IdentityPass       bool     `json:"identity_pass"`
	IdentityRule       string   `json:"identity_rule"`
	TitleSimilarity    float64  `json:"title_similarity"`
	CandidateCountries []string `json:"candidate_countries"`
	License            string   `json:"source_license"`
	ParseSeconds       float64  `json:"parse_seconds"`
	Error              string   `json:"error,omitempty"`
	TEIPath            string   `json:"tei_path,omitempty"`
	TEISHA             string   `json:"tei_sha256,omitempty"`
	SourceVersion      string   `json:"source_version,omitempty"`
	PlanSHA            string   `json:"source_plan_sha256,omitempty"`
	ExtractorVersion   string   `json:"extractor_version"`
}

const extractorVersion = "country-fulltext-v2-2026-pmc-bool-s3"

var client = &http.Client{Timeout: 50 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
	if len(via) > 6 {
		return fmt.Errorf("redirect limit")
	}
	return validatePublic(req.URL)
}}
var outdir string
var requestLedger sync.Mutex

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func readJSON(p string, v any) { b, e := os.ReadFile(p); must(e); must(json.Unmarshal(b, v)) }
func saveJSON(p string, v any) {
	must(os.MkdirAll(filepath.Dir(p), 0755))
	b, e := json.MarshalIndent(v, "", "  ")
	must(e)
	must(os.WriteFile(p+".tmp", b, 0644))
	must(os.Rename(p+".tmp", p))
}
func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func contentURL(raw string) string {
	if strings.HasPrefix(raw, "s3://pmc-oa-opendata/") {
		return "https://pmc-oa-opendata.s3.amazonaws.com/" + strings.TrimPrefix(raw, "s3://pmc-oa-opendata/")
	}
	return raw
}
func validatePublic(u *url.URL) error {
	h := strings.ToLower(u.Hostname())
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("non-HTTP source")
	}
	if h == "localhost" || h == "127.0.0.1" || strings.HasPrefix(h, "192.168.") || strings.HasPrefix(h, "10.") || h == "169.254.169.254" {
		return fmt.Errorf("non-public source host")
	}
	return nil
}
func fetch(raw, path string, max int64) (result Fetch, contents []byte) {
	f := Fetch{URL: raw, Path: path}
	t := time.Now()
	defer func() {
		result.Seconds = time.Since(t).Seconds()
		requestLedger.Lock()
		defer requestLedger.Unlock()
		file, e := os.OpenFile(filepath.Join(outdir, "fetch-events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if e == nil {
			_ = json.NewEncoder(file).Encode(result)
			_ = file.Close()
		}
	}()
	if b, e := os.ReadFile(path); e == nil {
		f.Status = 200
		f.Bytes = len(b)
		f.SHA = digest(b)
		f.Cached = true
		return f, b
	}
	u, e := url.Parse(raw)
	if e == nil {
		e = validatePublic(u)
	}
	if e != nil {
		f.Error = e.Error()
		return f, nil
	}
	req, e := http.NewRequest("GET", raw, nil)
	if e != nil {
		f.Error = e.Error()
		return f, nil
	}
	req.Header.Set("User-Agent", "OpenAlex-country-research-pilot/1.0 (bounded academic metadata study)")
	resp, e := client.Do(req)
	if e != nil {
		f.Error = e.Error()
		f.Seconds = time.Since(t).Seconds()
		return f, nil
	}
	defer resp.Body.Close()
	f.Status = resp.StatusCode
	f.URL = resp.Request.URL.String()
	f.ContentType = resp.Header.Get("Content-Type")
	if resp.StatusCode != 200 {
		f.Error = resp.Status
		f.Seconds = time.Since(t).Seconds()
		return f, nil
	}
	if resp.ContentLength > max {
		f.Error = "content_length_exceeds_budget"
		return f, nil
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, max+1))
	f.Bytes = len(b)
	if e != nil {
		f.Error = e.Error()
		return f, nil
	}
	f.Bytes = len(b)
	f.SHA = digest(b)
	if int64(len(b)) > max {
		f.Error = "body_exceeds_budget"
		return f, nil
	}
	must(os.MkdirAll(filepath.Dir(path), 0755))
	must(os.WriteFile(path+".tmp", b, 0644))
	must(os.Rename(path+".tmp", path))
	f.Seconds = time.Since(t).Seconds()
	return f, b
}
func doiMap(sample Sample, idmap string) map[uint64]*PMCMatch {
	dois := map[string][]uint64{}
	pmids := map[string][]uint64{}
	out := map[uint64]*PMCMatch{}
	conflicts := map[uint64]bool{}
	re := regexp.MustCompile(`(?i)PMC[0-9]+`)
	for _, w := range sample.Works {
		if d := normalizeDOI(w.DOI); d != "" {
			dois[d] = append(dois[d], w.ID)
		}
		if w.PMID != "" {
			p := strings.Trim(strings.TrimPrefix(w.PMID, "https://pubmed.ncbi.nlm.nih.gov/"), "/")
			pmids[p] = append(pmids[p], w.ID)
		}
		pmc := re.FindString(w.PMCID)
		for _, l := range w.Locations {
			if pmc == "" {
				pmc = re.FindString(l.Landing + " " + l.PDF)
			}
		}
		if pmc != "" {
			out[w.ID] = &PMCMatch{ID: w.ID, PMCID: strings.ToUpper(pmc)}
		}
	}
	f, e := os.Open(idmap)
	must(e)
	defer f.Close()
	g, e := gzip.NewReader(f)
	must(e)
	defer g.Close()
	r := csv.NewReader(g)
	r.FieldsPerRecord = -1
	header, e := r.Read()
	must(e)
	ix := map[string]int{}
	for i, h := range header {
		ix[h] = i
	}
	var rows int
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		must(e)
		rows++
		if len(row) < len(header) {
			continue
		}
		d := normalizeDOI(row[ix["DOI"]])
		p := row[ix["PMID"]]
		ids := append(append([]uint64{}, dois[d]...), pmids[p]...)
		for _, id := range ids {
			pmc := strings.ToUpper(row[ix["PMCID"]])
			if pmc == "" {
				continue
			}
			m := out[id]
			if m == nil {
				m = &PMCMatch{ID: id, PMCID: pmc}
				out[id] = m
			}
			if m.PMCID != pmc {
				conflicts[id] = true
				continue
			}
			m.DOIMatch = m.DOIMatch || containsValue(dois[d], id)
			m.PMIDMatch = m.PMIDMatch || containsValue(pmids[p], id)
		}
	}
	for id := range conflicts {
		delete(out, id)
	}
	fmt.Printf("PMC ID-map rows %d, unambiguous candidates %d, deferred mapping conflicts %d\n", rows, len(out), len(conflicts))
	return out
}
func containsValue[T comparable](xs []T, v T) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
func prepare(sample Sample, idmap, inventoryPlan string) Plan {
	matches := doiMap(sample, idmap)
	saveJSON(filepath.Join(outdir, "pmc-idmap-matches.json"), matches)
	byPMC := map[string][]*PMCMatch{}
	for _, m := range matches {
		byPMC[m.PMCID] = append(byPMC[m.PMCID], m)
	}
	var inv struct {
		Files []struct {
			Key  string `json:"key"`
			Size int64  `json:"size"`
			MD5  string `json:"MD5checksum"`
			URL  string `json:"url"`
		} `json:"files"`
	}
	readJSON(inventoryPlan, &inv)
	for _, item := range inv.Files {
		path := filepath.Join(outdir, "inventory", filepath.Base(item.Key))
		f, b := fetch(item.URL, path, item.Size+1024)
		if f.Error != "" {
			panic(f.Error)
		}
		m := md5.Sum(b)
		if hex.EncodeToString(m[:]) != item.MD5 {
			panic("inventory MD5 mismatch")
		}
		g, e := gzip.NewReader(bytes.NewReader(b))
		must(e)
		r := csv.NewReader(g)
		r.FieldsPerRecord = -1
		for {
			row, e := r.Read()
			if e == io.EOF {
				break
			}
			must(e)
			if len(row) < 2 {
				continue
			}
			key := row[1]
			name := strings.TrimSuffix(filepath.Base(key), ".json")
			pmc := strings.Split(name, ".")[0]
			for _, hit := range byPMC[pmc] {
				hit.Keys = append(hit.Keys, key)
			}
		}
		g.Close()
		saveJSON(path+".server-verification.json", f)
		fmt.Println("inventory checked", filepath.Base(path))
	}
	p := Plan{Status: "frozen", Mode: "xml_all_pmc_matches", Seed: "openalex-country-pilot-fulltext-v1", SampleSHA: sample.SampleSHA, SampleSize: len(sample.Works)}
	for _, m := range matches {
		sort.Strings(m.Keys)
		p.PMC = append(p.PMC, *m)
	}
	sort.Slice(p.PMC, func(i, j int) bool { return p.PMC[i].ID < p.PMC[j].ID })
	return p
}
func identity(w Work, p Parsed) (bool, string, float64) {
	a, b := normalizedTitle(w.Title), normalizedTitle(p.Title)
	sim := titleSimilarity(a, b)
	titleRule := "doi_plus_title_year"
	for _, alt := range p.AlternativeTitles {
		if candidate := titleSimilarity(a, normalizedTitle(alt)); candidate > sim {
			sim = candidate
			titleRule = "doi_plus_source_trans_title_year"
		}
	}
	sameDOI := normalizeDOI(w.DOI) != "" && normalizeDOI(w.DOI) == normalizeDOI(p.DOI)
	year, _ := strconv.Atoi(p.Year)
	dy := w.Year - year
	if dy < 0 {
		dy = -dy
	}
	yearOK := w.Year == 0 || (year > 0 && dy <= 1)
	if normalizeDOI(w.DOI) != "" && normalizeDOI(p.DOI) != "" && !sameDOI {
		return false, "doi_conflict", sim
	}
	if sameDOI && sim >= .85 && yearOK {
		return true, titleRule, sim
	}
	if w.DOI == "" && len([]rune(a)) >= 20 && a == b && yearOK {
		return true, "exact_long_title_plus_direct_source_year_reviewable", sim
	}
	return false, "identity_requires_review", sim
}
func titleSimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	runes := func(s string) map[string]bool {
		r := []rune(s)
		m := map[string]bool{}
		for i := 1; i < len(r); i++ {
			m[string(r[i-1:i+1])] = true
		}
		return m
	}
	x, y := runes(a), runes(b)
	n := 0
	for z := range x {
		if y[z] {
			n++
		}
	}
	return 2 * float64(n) / float64(len(x)+len(y))
}
func baseOutcome(w Work, route string) Outcome {
	return Outcome{ID: w.ID, WorkID: w.WorkID, Stratum: w.Stratum, Weight: w.Weight, PI1: w.PI, PI2: 1, Route: route, Status: "not_attempted", ExtractorVersion: extractorVersion}
}
func finishParsed(o Outcome, w Work, p Parsed) Outcome {
	o.Parsed = &p
	o.IdentityPass, o.IdentityRule, o.TitleSimilarity = identity(w, p)
	if !o.IdentityPass {
		o.Status = "identity_review"
		return o
	}
	o.CandidateCountries = p.Countries
	if len(p.Countries) > 0 {
		o.Status = "country_candidate_review_pending"
	} else {
		o.Status = "parsed_no_linked_country"
	}
	return o
}
func xmlOne(w Work, m PMCMatch, r Rules) Outcome {
	o := baseOutcome(w, "pmc_jats")
	if len(m.Keys) == 0 {
		o.Status = "pmc_not_in_cloud_inventory"
		return o
	}
	type version struct {
		Key, XML, License string
		Manuscript        bool
		Fetch             Fetch
	}
	versions := []version{}
	for _, key := range m.Keys {
		f, b := fetch("https://pmc-oa-opendata.s3.amazonaws.com/"+key, filepath.Join(outdir, "pmc-metadata", filepath.Base(key)), 2<<20)
		if f.Error != "" {
			continue
		}
		var md struct {
			XML        string `json:"xml_url"`
			License    string `json:"license_code"`
			Manuscript bool   `json:"is_manuscript"`
		}
		if json.Unmarshal(b, &md) != nil {
			continue
		}
		versions = append(versions, version{key, md.XML, md.License, md.Manuscript, f})
	}
	sort.SliceStable(versions, func(i, j int) bool { return !versions[i].Manuscript && versions[j].Manuscript })
	for _, v := range versions {
		if v.XML == "" {
			continue
		}
		f, b := fetch(contentURL(v.XML), filepath.Join(outdir, "xml", strings.TrimSuffix(filepath.Base(v.Key), ".json")+".xml"), 32<<20)
		o.Source = f
		o.Metadata = &v.Fetch
		o.License = v.License
		o.SourceVersion = v.Key
		if f.Error != "" {
			o.Status = "xml_download_failed"
			o.Error = f.Error
			continue
		}
		t := time.Now()
		p, e := parseJATS(b, r)
		o.ParseSeconds = time.Since(t).Seconds()
		if e != nil {
			o.Status = "xml_parse_failed"
			o.Error = e.Error()
			continue
		}
		candidate := finishParsed(o, w, p)
		if candidate.IdentityPass {
			return candidate
		}
		o = candidate
	}
	if len(versions) == 0 {
		o.Status = "pmc_metadata_failed"
	}
	return o
}
func pdfURLs(w Work) (string, string) {
	for _, l := range w.Locations {
		if l.IsOA && l.PDF != "" {
			u, e := url.Parse(l.PDF)
			if e == nil && validatePublic(u) == nil && !strings.Contains(u.Hostname(), "ncbi.nlm.nih.gov") {
				return l.PDF, l.License
			}
		}
	}
	if w.IsOA && strings.Contains(strings.ToLower(w.OAURL), ".pdf") {
		return w.OAURL, ""
	}
	return "", ""
}
func preparePDF(sample Sample, xmlResults []Outcome, cap int) Plan {
	recovered := map[uint64]bool{}
	for _, r := range xmlResults {
		recovered[r.ID] = r.IdentityPass && len(r.CandidateCountries) > 0
	}
	groups := map[string][]PDFPick{}
	for _, w := range sample.Works {
		if recovered[w.ID] {
			continue
		}
		u, l := pdfURLs(w)
		if u == "" {
			continue
		}
		h := digest([]byte("openalex-fulltext-pdf-v1\n" + strconv.FormatUint(w.ID, 10)))
		groups[w.Stratum] = append(groups[w.Stratum], PDFPick{ID: w.ID, URL: u, License: l, Stratum: w.Stratum, Hash: h})
	}
	p := Plan{Status: "frozen", Mode: "pdf_conditional_on_no_xml_country", Seed: "openalex-fulltext-pdf-v1", SampleSHA: sample.SampleSHA, SampleSize: len(sample.Works)}
	for s, g := range groups {
		sort.Slice(g, func(i, j int) bool { return g[i].Hash < g[j].Hash })
		n := min(cap, len(g))
		pi := float64(n) / float64(len(g))
		p.PDFStrata = append(p.PDFStrata, PDFStratum{s, len(g), n, pi})
		for _, x := range g {
			x.PI2 = pi
			p.PDFEligible = append(p.PDFEligible, x)
		}
		for _, x := range g[:n] {
			x.PI2 = pi
			p.PDF = append(p.PDF, x)
		}
	}
	sort.Slice(p.PDFStrata, func(i, j int) bool { return p.PDFStrata[i].Stratum < p.PDFStrata[j].Stratum })
	return p
}
func pdfOne(w Work, pick PDFPick, r Rules, service string) Outcome {
	o := baseOutcome(w, "oa_pdf_grobid_header")
	o.PI2 = pick.PI2
	o.License = pick.License
	f, b := fetch(pick.URL, filepath.Join(outdir, "pdf", fmt.Sprintf("W%d.pdf", w.ID)), 20<<20)
	o.Source = f
	if f.Error != "" {
		o.Status = "pdf_download_failed"
		o.Error = f.Error
		return o
	}
	if !bytes.Contains(b[:min(1024, len(b))], []byte("%PDF-")) {
		o.Status = "not_pdf"
		return o
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, e := mw.CreateFormFile("input", fmt.Sprintf("W%d.pdf", w.ID))
	if e != nil {
		panic(e)
	}
	_, e = part.Write(b)
	must(e)
	for k, v := range map[string]string{"consolidateHeader": "0", "consolidateCitations": "0", "consolidateFunders": "0", "includeRawAffiliations": "1"} {
		must(mw.WriteField(k, v))
	}
	must(mw.Close())
	req, e := http.NewRequest("POST", service+"/api/processHeaderDocument", &body)
	must(e)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "application/xml")
	t := time.Now()
	resp, e := (&http.Client{Timeout: 150 * time.Second}).Do(req)
	if e != nil {
		o.Status = "grobid_failed"
		o.Error = e.Error()
		return o
	}
	defer resp.Body.Close()
	tei, e := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	o.ParseSeconds = time.Since(t).Seconds()
	if e != nil || resp.StatusCode != 200 {
		o.Status = "grobid_failed"
		o.Error = fmt.Sprint(resp.StatusCode, " ", e)
		return o
	}
	o.TEIPath = filepath.Join(outdir, "tei", fmt.Sprintf("W%d.tei.xml", w.ID))
	must(os.MkdirAll(filepath.Dir(o.TEIPath), 0755))
	must(os.WriteFile(o.TEIPath, tei, 0644))
	o.TEISHA = digest(tei)
	p, e := parseTEI(tei, r)
	if e != nil {
		o.Status = "tei_parse_failed"
		o.Error = e.Error()
		return o
	}
	return finishParsed(o, w, p)
}
func main() {
	mode := flag.String("mode", "prepare", "prepare|xml|prepare-pdf|pdf")
	planFlag := flag.String("plan", "", "explicit content plan, including provisional first-version discovery")
	samplePath := flag.String("sample", "", "frozen sample metadata")
	out := flag.String("out", "", "isolated output")
	idmap := flag.String("idmap", "", "PMC bulk ID map")
	inv := flag.String("inventory-plan", "", "official inventory plan")
	dict := flag.String("dictionary", "", "original country dictionary")
	workers := flag.Int("workers", 3, "bounded workers")
	pdfCap := flag.Int("pdf-per-stratum", 8, "second-stage per-stratum cap")
	resultSubdir := flag.String("result-subdir", "", "versioned XML result directory; PDF defaults unchanged")
	policyInput := flag.String("policy-input", "", "cached PDF v2 outcomes, for audit-v3 only")
	authorsDir := flag.String("original-authors", "", "verified original authors cache, for audit-v3 only")
	retryParser := flag.Bool("retry-grobid-bootstrap-failures", false, "Retry only cached PDFs whose parser failed during environment setup, preserving old outcomes")
	service := flag.String("grobid", "http://127.0.0.1:18070", "local parser")
	flag.Parse()
	outdir = *out
	must(os.MkdirAll(outdir, 0755))
	if *mode == "download-inventory" {
		var plan struct {
			Files []struct {
				Key  string `json:"key"`
				URL  string `json:"url"`
				Size int64  `json:"size"`
				MD5  string `json:"MD5checksum"`
			} `json:"files"`
		}
		readJSON(*inv, &plan)
		jobs := make(chan int)
		var wg sync.WaitGroup
		for i := 0; i < *workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					x := plan.Files[i]
					f, b := fetch(x.URL, filepath.Join(outdir, "inventory", filepath.Base(x.Key)), x.Size+1024)
					if f.Error != "" {
						fmt.Println("inventory failure", filepath.Base(x.Key), f.Error)
						continue
					}
					m := md5.Sum(b)
					if hex.EncodeToString(m[:]) != x.MD5 {
						panic("inventory checksum mismatch")
					}
					saveJSON(f.Path+".fetch.json", f)
					fmt.Println("inventory downloaded", filepath.Base(x.Key), f.Bytes)
				}
			}()
		}
		for i := range plan.Files {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		return
	}
	var sample Sample
	readJSON(*samplePath, &sample)
	byID := map[uint64]Work{}
	for _, w := range sample.Works {
		byID[w.ID] = w
	}
	switch *mode {
	case "apply-frozen-policy":
		applyFrozenPolicy(sample, *policyInput, *authorsDir, *planFlag)
	case "audit-v3":
		var p Plan
		readJSON(*planFlag, &p)
		auditV3(sample, p, *policyInput, *authorsDir, outdir)
	case "prepare-idmap":
		matches := doiMap(sample, *idmap)
		p := Plan{Status: "provisional_first_version_only_not_final", Mode: "xml_first_version_discovery", Seed: "openalex-country-pilot-fulltext-v1", SampleSHA: sample.SampleSHA, SampleSize: len(sample.Works)}
		for _, m := range matches {
			m.Keys = []string{"metadata/" + m.PMCID + ".1.json"}
			p.PMC = append(p.PMC, *m)
		}
		sort.Slice(p.PMC, func(i, j int) bool { return p.PMC[i].ID < p.PMC[j].ID })
		saveJSON(filepath.Join(outdir, "xml-plan-provisional.json"), p)
	case "prepare":
		p := prepare(sample, *idmap, *inv)
		saveJSON(filepath.Join(outdir, "xml-plan.json"), p)
	case "xml":
		var p Plan
		planPath := *planFlag
		if planPath == "" {
			planPath = filepath.Join(outdir, "xml-plan.json")
		}
		readJSON(planPath, &p)
		planBytes, e := os.ReadFile(planPath)
		must(e)
		planSHA := digest(planBytes)
		r, e := loadRules(*dict)
		must(e)
		r = withXMLCountryAliases(r)
		jobs := make(chan PMCMatch)
		var wg sync.WaitGroup
		for i := 0; i < *workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for m := range jobs {
					dir := *resultSubdir
					if dir == "" {
						dir = "xml-results-v2"
					}
					path := filepath.Join(outdir, dir, fmt.Sprintf("W%d.json", m.ID))
					if old, e := os.ReadFile(path); e == nil {
						var previous Outcome
						if json.Unmarshal(old, &previous) == nil && previous.PlanSHA == planSHA && previous.ExtractorVersion == "country-fulltext-xml-v3-trans-title-uk" {
							continue
						}
					}
					v := xmlOne(byID[m.ID], m, r)
					v.PlanSHA = planSHA
					v.ExtractorVersion = "country-fulltext-xml-v3-trans-title-uk"
					saveJSON(path, v)
					fmt.Println("XML", m.ID, v.Status)
				}
			}()
		}
		for _, m := range p.PMC {
			jobs <- m
		}
		close(jobs)
		wg.Wait()
	case "prepare-pdf":
		files, e := filepath.Glob(filepath.Join(outdir, "xml-results/*.json"))
		must(e)
		rs := []Outcome{}
		for _, f := range files {
			var o Outcome
			readJSON(f, &o)
			rs = append(rs, o)
		}
		p := preparePDF(sample, rs, *pdfCap)
		dest := *planFlag
		if dest == "" {
			dest = filepath.Join(outdir, "pdf-plan.json")
		}
		saveJSON(dest, p)
	case "pdf":
		var p Plan
		planPath := *planFlag
		if planPath == "" {
			planPath = filepath.Join(outdir, "pdf-plan.json")
		}
		readJSON(planPath, &p)
		planBytes, e := os.ReadFile(planPath)
		must(e)
		planSHA := digest(planBytes)
		if p.ParentPlanSHA != "" {
			planSHA = p.ParentPlanSHA
		}
		r, e := loadRules(*dict)
		must(e)
		jobs := make(chan PDFPick)
		var wg sync.WaitGroup
		for i := 0; i < *workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for pick := range jobs {
					path := filepath.Join(outdir, "pdf-results", fmt.Sprintf("W%d.json", pick.ID))
					if old, e := os.ReadFile(path); e == nil {
						var previous Outcome
						must(json.Unmarshal(old, &previous))
						if !*retryParser || previous.Status != "grobid_failed" {
							continue
						}
						source, e := os.ReadFile(filepath.Join(outdir, "pdf", fmt.Sprintf("W%d.pdf", pick.ID)))
						if e != nil || !bytes.Contains(source[:min(1024, len(source))], []byte("%PDF-")) {
							panic("Parser retry requires cached original PDF")
						}
						backup := filepath.Join(outdir, "bootstrap-parser-errors", fmt.Sprintf("W%d.json", pick.ID))
						if _, e = os.Stat(backup); os.IsNotExist(e) {
							saveJSON(backup, previous)
						}
					}
					o := pdfOne(byID[pick.ID], pick, r, *service)
					o.PlanSHA = planSHA
					saveJSON(path, o)
					fmt.Println("PDF", pick.ID, o.Status)
				}
			}()
		}
		for _, x := range p.PDF {
			jobs <- x
		}
		close(jobs)
		wg.Wait()
	default:
		panic("unknown mode")
	}
}
