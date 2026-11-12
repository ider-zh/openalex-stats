// country-enrichment is an isolated research evaluator. It writes a sidecar
// review dataset and never mutates raw data, normalized caches or statistics.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
	"openalexstats/internal/cache"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/model"
	"openalexstats/internal/policy"
	"openalexstats/internal/scan"
)

type Affiliation struct {
	IDs []*string `parquet:"institution_ids,list"`
	Raw *string   `parquet:"raw_affiliation_string"`
}
type Institution struct {
	ID      *string `parquet:"id"`
	Country *string `parquet:"country_code"`
}
type Authorship struct {
	Countries    []*string      `parquet:"countries,list"`
	Institutions []*Institution `parquet:"institutions,list"`
	Affiliations []*Affiliation `parquet:"affiliations,list"`
	Raw          []*string      `parquet:"raw_affiliation_strings,list"`
}
type RawWork struct {
	ID       *string       `parquet:"id"`
	Year     *int32        `parquet:"publication_year"`
	Type     *string       `parquet:"type"`
	XPAC     *bool         `parquet:"is_xpac"`
	Paratext *bool         `parquet:"is_paratext"`
	Authors  []*Authorship `parquet:"authorships,list"`
}
type InstRow struct {
	ID      *string `parquet:"id"`
	Country *string `parquet:"country_code"`
	Name    *string `parquet:"display_name"`
	Geo     *struct {
		Country *string `parquet:"country_code"`
	} `parquet:"geo"`
}
type FlatWork struct {
	ID        uint64   `parquet:"id"`
	Year      int32    `parquet:"year"`
	Type      string   `parquet:"type"`
	XPAC      bool     `parquet:"is_xpac"`
	Paratext  bool     `parquet:"is_paratext"`
	Countries []string `parquet:"countries,list"`
}
type Evidence struct {
	Author        int    `json:"authorship_index"`
	Source        string `json:"source"`
	Country       string `json:"country,omitempty"`
	InstitutionID string `json:"institution_id,omitempty"`
	Raw           string `json:"raw_affiliation,omitempty"`
	Segment       string `json:"matched_segment,omitempty"`
	Decision      string `json:"decision"`
	Reason        string `json:"reason,omitempty"`
}
type Patch struct {
	ID               uint64   `parquet:"id"`
	WorkID           string   `parquet:"work_id"`
	Year             int32    `parquet:"year"`
	Type             string   `parquet:"type"`
	XPAC             bool     `parquet:"is_xpac"`
	Original         []string `parquet:"original_countries,list"`
	Candidates       []string `parquet:"candidate_countries,list"`
	Conservative     []string `parquet:"conservative_countries,list"`
	Added            []string `parquet:"added_countries,list"`
	InstitutionAdded []string `parquet:"institution_added,list"`
	TextAdded        []string `parquet:"text_added,list"`
	ReviewOnly       []string `parquet:"review_only_countries,list"`
	WasUnknown       bool     `parquet:"was_unknown"`
	NewlyKnown       bool     `parquet:"newly_known"`
	Conflict         bool     `parquet:"conflict"`
	RuleVersion      string   `parquet:"rule_version,dict"`
	Evidence         string   `parquet:"evidence_json"`
}
type Stats struct {
	Works              uint64 `json:"works"`
	Known              uint64 `json:"baseline_known"`
	Missing            uint64 `json:"baseline_missing"`
	WithAuthors        uint64 `json:"with_authorships"`
	WithRaw            uint64 `json:"with_raw_text"`
	MissingWithRaw     uint64 `json:"missing_with_raw_text"`
	MissingWithInst    uint64 `json:"missing_with_institution_id"`
	NewInstitution     uint64 `json:"newly_known_institution"`
	NewText            uint64 `json:"newly_known_text"`
	NewBoth            uint64 `json:"newly_known_both"`
	NewCombined        uint64 `json:"newly_known_combined"`
	KnownExtended      uint64 `json:"already_known_extended"`
	ReviewUnknown      uint64 `json:"unknown_with_review_only"`
	Conflicts          uint64 `json:"works_with_conflict"`
	AmbiguousText      uint64 `json:"works_with_ambiguous_text"`
	KnownTextMatched   uint64 `json:"known_with_text_country"`
	KnownTextSubset    uint64 `json:"known_text_all_countries_in_baseline"`
	KnownTextNovel     uint64 `json:"known_text_has_novel_country"`
	KnownTextDisjoint  uint64 `json:"known_text_no_country_in_baseline"`
	TextMatched        uint64 `json:"works_with_text_country"`
	MissingTextMatched uint64 `json:"missing_with_text_country"`
	EmbeddedNew        uint64 `json:"works_with_new_embedded_country"`
	LookupNew          uint64 `json:"works_with_new_lookup_country"`
	PatchRows          uint64 `json:"patch_rows"`
}

func (s *Stats) add(t Stats) {
	s.Works += t.Works
	s.Known += t.Known
	s.Missing += t.Missing
	s.WithAuthors += t.WithAuthors
	s.WithRaw += t.WithRaw
	s.MissingWithRaw += t.MissingWithRaw
	s.MissingWithInst += t.MissingWithInst
	s.NewInstitution += t.NewInstitution
	s.NewText += t.NewText
	s.NewBoth += t.NewBoth
	s.NewCombined += t.NewCombined
	s.KnownExtended += t.KnownExtended
	s.ReviewUnknown += t.ReviewUnknown
	s.Conflicts += t.Conflicts
	s.AmbiguousText += t.AmbiguousText
	s.KnownTextMatched += t.KnownTextMatched
	s.KnownTextSubset += t.KnownTextSubset
	s.KnownTextNovel += t.KnownTextNovel
	s.KnownTextDisjoint += t.KnownTextDisjoint
	s.TextMatched += t.TextMatched
	s.MissingTextMatched += t.MissingTextMatched
	s.EmbeddedNew += t.EmbeddedNew
	s.LookupNew += t.LookupNew
	s.PatchRows += t.PatchRows
}

type Validation struct {
	Matched  uint64 `json:"matched_works"`
	Subset   uint64 `json:"matched_country_present_in_baseline"`
	Novel    uint64 `json:"matched_country_absent_from_baseline"`
	Disjoint uint64 `json:"all_text_countries_absent_from_baseline"`
}
type Group struct {
	Corpus    string `json:"corpus"`
	Dimension string `json:"dimension"`
	Value     string `json:"value"`
	Stats     Stats  `json:"stats"`
}
type FileReport struct {
	Version          string                `json:"version"`
	RuleSHA          string                `json:"rules_sha256"`
	BuildSHA         string                `json:"build_sha256"`
	Index            int                   `json:"index"`
	Source           checkpoint.Artifact   `json:"source"`
	SourceModNS      int64                 `json:"source_mod_ns"`
	Cache            checkpoint.Artifact   `json:"cache"`
	RawRows          int64                 `json:"raw_rows"`
	Eligible         uint64                `json:"eligible"`
	BaselineSHA      string                `json:"baseline_sha256"`
	CacheBaselineSHA string                `json:"cache_baseline_sha256"`
	Patch            checkpoint.Artifact   `json:"patch"`
	Anchors          checkpoint.Artifact   `json:"known_affiliation_anchors"`
	Unresolved       checkpoint.Artifact   `json:"unresolved_affiliations"`
	AnchorRows       int64                 `json:"anchor_rows"`
	UnresolvedRows   int64                 `json:"unresolved_rows"`
	Groups           []Group               `json:"groups"`
	Validation       map[string]Validation `json:"text_validation_by_country"`
	Examples         map[string][]Patch    `json:"examples"`
	Seconds          float64               `json:"seconds"`
	SchemaSHA        string                `json:"output_schema_sha256,omitempty"`
	CompatibleOrigin checkpoint.Artifact   `json:"compatible_origin_checkpoint,omitempty"`
}
type Config struct {
	Manifest, Cache, Dictionary, Work                string
	Workers, CPUs, Files                             int
	Indices                                          string
	MemoryGiB                                        uint64
	Mode                                             string
	RawReaders                                       int
	RawBufferGiB                                     uint64
	CompatibleWork, CompatibleBuild, CompatibleProof string
	CoreWork, AllWork, CoreCounters, AllCounters     string
}
type Env struct {
	cfg                    Config
	rules                  Rules
	ruleSHA, buildSHA      string
	institutions           map[string][]string
	institutionGeo         map[string][]string
	institutionNames       map[string][]NameRef
	scope                  policy.Scope
	cache                  cache.Manifest
	manifestSHA            string
	paths                  []string
	expected               []int64
	readers                chan struct{}
	buffers                *byteBudget
	schemaSHA              string
	compatibilityArtifacts []checkpoint.Artifact
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	var c Config
	flag.StringVar(&c.Manifest, "manifest", "", "raw manifest")
	flag.StringVar(&c.Cache, "cache", "", "normalized cache")
	flag.StringVar(&c.Dictionary, "dictionary", "", "snapshot dictionary JSON")
	flag.StringVar(&c.Work, "work", "", "isolated enrichment directory")
	flag.IntVar(&c.Workers, "workers", 36, "file workers")
	flag.IntVar(&c.CPUs, "cpus", 40, "Go CPU limit")
	flag.IntVar(&c.Files, "files", 0, "pilot file count; zero full")
	flag.StringVar(&c.Indices, "indices", "", "pilot source indices")
	flag.Uint64Var(&c.MemoryGiB, "memory-gib", 96, "Go heap soft limit")
	flag.StringVar(&c.Mode, "mode", "scan", "scan or citations")
	flag.IntVar(&c.RawReaders, "raw-readers", 0, "0=legacy projected reads; >0 bounded sequential whole-file readers")
	flag.Uint64Var(&c.RawBufferGiB, "raw-buffer-gib", 48, "maximum whole-file buffers")
	flag.StringVar(&c.CompatibleWork, "compatible-work", "", "explicit read-only legacy evaluator checkpoints to import")
	flag.StringVar(&c.CompatibleBuild, "compatible-build-sha256", "", "explicit semantically equivalent legacy binary SHA256")
	flag.StringVar(&c.CompatibleProof, "compatibility-proof", "", "passed independent three-shard byte-equivalence proof")
	flag.StringVar(&c.CoreWork, "core-work", "/mnt/nvme1/backup/openalex-stats/core-goal-v1", "core dimensions checkpoint")
	flag.StringVar(&c.AllWork, "all-work", "/mnt/nvme1/backup/openalex-stats/all-concept-hits-v1", "all dimensions checkpoint")
	flag.StringVar(&c.CoreCounters, "core-counters", "/mnt/nvme2/public/openalex-stats/core-counts-v2/3b00c22308c75534", "core target counters")
	flag.StringVar(&c.AllCounters, "all-counters", "/mnt/nvme2/public/openalex-stats/all-concept-hits-counts/9859f49a51abd149", "all target counters")
	flag.Parse()
	if c.Work == "" || c.Workers < 1 || c.Workers > 46 || c.CPUs < 1 || c.CPUs > 46 || c.MemoryGiB < 1 || c.RawReaders < 0 || c.RawReaders > 8 || c.RawBufferGiB > 48 || c.RawReaders > 0 && c.RawBufferGiB == 0 || (c.CompatibleWork == "") != (c.CompatibleBuild == "") {
		return fmt.Errorf("invalid isolated evaluator config")
	}
	runtime.GOMAXPROCS(c.CPUs)
	debug.SetMemoryLimit(int64(c.MemoryGiB << 30))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go guard(ctx, cancel)
	if e := os.MkdirAll(c.Work, 0755); e != nil {
		return e
	}
	unlock, e := checkpoint.LockDirectory(c.Work)
	if e != nil {
		return e
	}
	defer unlock()
	if c.Mode == "citations" {
		return citations(ctx, c)
	}
	if c.Manifest == "" || c.Cache == "" || c.Dictionary == "" {
		return fmt.Errorf("manifest/cache/dictionary required")
	}
	env, e := prepare(c)
	if e != nil {
		return e
	}
	indices := make([]int, 0, len(env.paths))
	if c.Indices != "" {
		for _, v := range strings.Split(c.Indices, ",") {
			i, e := strconv.Atoi(v)
			if e != nil || i < 0 || i >= len(env.paths) {
				return fmt.Errorf("bad source index")
			}
			indices = append(indices, i)
		}
	} else {
		for i := range env.paths {
			if c.Files > 0 && i >= c.Files {
				break
			}
			indices = append(indices, i)
		}
	}
	if len(indices) == 0 {
		return fmt.Errorf("no sources")
	}
	seen := map[int]bool{}
	for _, i := range indices {
		if seen[i] {
			return fmt.Errorf("duplicate source")
		}
		seen[i] = true
	}
	start := time.Now()
	jobs := make(chan int)
	reports := make([]FileReport, len(env.paths))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var first error
	completed := 0
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	for w := 0; w < min(c.Workers, len(indices)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					continue
				}
				r, reused, e := env.process(ctx, i)
				mu.Lock()
				if e != nil {
					if first == nil {
						first = fmt.Errorf("part %d: %w", i, e)
						stop()
					}
				} else {
					reports[i] = r
					completed++
					log(map[string]any{"stage": "part_complete", "index": i, "completed": completed, "total": len(indices), "rows": r.RawRows, "eligible": r.Eligible, "patch_rows": r.PatchRows(), "reused": reused, "seconds": r.Seconds, "elapsed": time.Since(start).Seconds()})
				}
				mu.Unlock()
			}
		}()
	}
	for _, i := range indices {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break
		}
	}
	close(jobs)
	wg.Wait()
	if first != nil {
		return first
	}
	if e = ctx.Err(); e != nil {
		return e
	}
	return env.summarize(reports, indices, start)
}
func (r FileReport) PatchRows() uint64 {
	for _, g := range r.Groups {
		if g.Corpus == "all" && g.Dimension == "window" && g.Value == "all_years" {
			return g.Stats.PatchRows
		}
	}
	return 0
}
func log(v any) { _ = json.NewEncoder(os.Stderr).Encode(v) }
func guard(ctx context.Context, cancel context.CancelFunc) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b, e := os.ReadFile("/proc/meminfo")
			if e == nil {
				for _, line := range strings.Split(string(b), "\n") {
					if strings.HasPrefix(line, "MemAvailable:") {
						var kb uint64
						_, _ = fmt.Sscanf(line, "MemAvailable: %d kB", &kb)
						if kb > 0 && kb < 64<<20 {
							log(map[string]any{"stage": "memory_guard", "available_kib": kb})
							cancel()
						}
					}
				}
			}
		}
	}
}

func prepare(c Config) (*Env, error) {
	e := &Env{cfg: c, institutions: map[string][]string{}, institutionGeo: map[string][]string{}, institutionNames: map[string][]NameRef{}}
	if c.RawReaders > 0 {
		e.readers = make(chan struct{}, c.RawReaders)
		e.buffers = newByteBudget(c.RawBufferGiB << 30)
	}
	e.schemaSHA = outputSchemaSHA()
	var err error
	e.rules, err = loadRules(c.Dictionary)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(e.rules)
	sum := sha256.Sum256(b)
	e.ruleSHA = hex.EncodeToString(sum[:])
	if err = checkpoint.JSON(filepath.Join(c.Work, "rules.json"), e.rules); err != nil {
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	a, err := checkpoint.Hash(exe)
	if err != nil {
		return nil, err
	}
	e.buildSHA = a.SHA256
	a, err = checkpoint.Hash(c.Manifest)
	if err != nil {
		return nil, err
	}
	e.manifestSHA = a.SHA256
	b, err = os.ReadFile(filepath.Join(c.Cache, "manifest.json"))
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &e.cache); err != nil {
		return nil, err
	}
	if !e.cache.Complete || e.cache.SourceManifest != a.SHA256 {
		return nil, fmt.Errorf("cache/raw manifest mismatch")
	}
	var m struct {
		Entities []struct {
			Entity string `json:"entity"`
			Files  []struct {
				URL  string `json:"url"`
				Meta struct {
					Rows int64 `json:"record_count"`
				} `json:"meta"`
			} `json:"files"`
		} `json:"entities"`
	}
	b, err = os.ReadFile(c.Manifest)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	root := filepath.Dir(c.Manifest)
	var instPaths []string
	for _, entity := range m.Entities {
		if entity.Entity != "works" && entity.Entity != "institutions" {
			continue
		}
		for _, f := range entity.Files {
			pieces := strings.SplitN(f.URL, "/parquet/", 2)
			if len(pieces) != 2 {
				return nil, fmt.Errorf("noncanonical source URL")
			}
			p := filepath.Join(root, filepath.FromSlash(pieces[1]))
			if entity.Entity == "works" {
				e.paths = append(e.paths, p)
				e.expected = append(e.expected, f.Meta.Rows)
			} else {
				instPaths = append(instPaths, p)
			}
		}
	}
	if len(e.paths) != len(e.cache.Parts) {
		return nil, fmt.Errorf("source/cache inventory mismatch")
	}
	for i, p := range e.paths {
		if e.cache.Parts[i].Index != i || e.cache.Parts[i].InputPath != p || e.cache.Parts[i].Rows != e.expected[i] {
			return nil, fmt.Errorf("source/cache part identity mismatch %d", i)
		}
	}
	var institutionArtifacts []checkpoint.Artifact
	institutionStudy := map[string]uint64{}
	for _, p := range instPaths {
		a, err := checkpoint.Hash(p)
		if err != nil {
			return nil, err
		}
		institutionArtifacts = append(institutionArtifacts, a)
		if err = each[InstRow](p, func(row InstRow) error {
			institutionStudy["rows"]++
			if row.ID != nil && row.Country != nil && *row.ID != "" && e.rules.valid[*row.Country] {
				e.institutions[*row.ID] = addUnique(e.institutions[*row.ID], *row.Country)
				institutionStudy["valid_top_level_country"]++
				if name := normalizedRaw(str(row.Name)); len(name) >= 12 {
					e.institutionNames[name] = append(e.institutionNames[name], NameRef{*row.ID, *row.Country})
				}
			}
			if row.Geo != nil && e.rules.valid[str(row.Geo.Country)] {
				institutionStudy["valid_geo_country"]++
				if !e.rules.valid[str(row.Country)] && str(row.ID) != "" {
					e.institutionGeo[*row.ID] = addUnique(e.institutionGeo[*row.ID], *row.Geo.Country)
					institutionStudy["geo_country_without_valid_top_level_country"]++
				} else if str(row.Geo.Country) != str(row.Country) {
					institutionStudy["top_level_geo_country_disagreement"]++
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	e.scope, _ = policy.New("all", 0, policy.DefaultBlacklist)
	if c.CompatibleWork != "" {
		if err = e.checkCompatibilityProof(); err != nil {
			return nil, err
		}
	}
	dictionaryArtifact, err := checkpoint.Hash(c.Dictionary)
	if err != nil {
		return nil, err
	}
	var maximumSource int64
	for _, p := range e.cache.Parts {
		if p.InputSize > maximumSource {
			maximumSource = p.InputSize
		}
	}
	identity := map[string]any{"rules_sha256": e.ruleSHA, "build_sha256": e.buildSHA, "output_schema_sha256": e.schemaSHA, "compatibility_evidence": e.compatibilityArtifacts, "maximum_raw_file_bytes": maximumSource, "raw_file_buffers_max_bytes": c.RawBufferGiB << 30, "source_manifest": a, "dictionary": dictionaryArtifact, "raw_source_files": len(e.paths), "expected_rows": e.cache.Rows, "institution_files": institutionArtifacts, "institution_ids_with_country": len(e.institutions), "institution_geo_study": institutionStudy, "secondary_candidates_applied_in_v1": false, "config": c, "production_mutated": false}
	if err = checkpoint.JSON(filepath.Join(c.Work, "identity.json"), identity); err != nil {
		return nil, err
	}
	log(map[string]any{"stage": "prepared", "institution_ids": len(e.institutions), "files": len(e.paths), "rules_sha256": e.ruleSHA, "build_sha256": e.buildSHA})
	return e, nil
}

func each[T any](path string, fn func(T) error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("parquet projection: %v", p)
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
		for _, row := range buf[:n] {
			if e2 := fn(row); e2 != nil {
				return e2
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
func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
func truth(b *bool) bool { return b != nil && *b }
func baseCountries(authors []*Authorship) (out []string) {
	for _, a := range authors {
		if a != nil {
			for _, v := range a.Countries {
				out = addUnique(out, str(v))
			}
		}
	}
	return uniqueSorted(out)
}
func hashBaseline(h hash.Hash, id uint64, year int32, typ string, xpac, paratext bool, countries []string) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], id)
	_, _ = h.Write(b[:])
	binary.LittleEndian.PutUint32(b[:4], uint32(year))
	_, _ = h.Write(b[:4])
	b[0] = 0
	if xpac {
		b[0] |= 1
	}
	if paratext {
		b[0] |= 2
	}
	_, _ = h.Write(b[:1])
	put := func(s string) {
		binary.LittleEndian.PutUint64(b[:], uint64(len(s)))
		_, _ = h.Write(b[:])
		_, _ = io.WriteString(h, s)
	}
	put(typ)
	binary.LittleEndian.PutUint64(b[:], uint64(len(countries)))
	_, _ = h.Write(b[:])
	for _, v := range countries {
		put(v)
	}
}

func (e *Env) process(ctx context.Context, index int) (out FileReport, reused bool, err error) {
	start := time.Now()
	path := e.paths[index]
	metaPath := filepath.Join(e.cfg.Work, "parts", fmt.Sprintf("part-%06d.json", index))
	patchPath := filepath.Join(e.cfg.Work, "patch", fmt.Sprintf("part-%06d.parquet", index))
	anchorPath := filepath.Join(e.cfg.Work, "anchors", fmt.Sprintf("part-%06d.parquet", index))
	unresolvedPath := filepath.Join(e.cfg.Work, "unresolved", fmt.Sprintf("part-%06d.parquet", index))
	info, err := os.Stat(path)
	if err != nil {
		return out, false, err
	}
	if checkpoint.ReadJSON(metaPath, &out) == nil && out.Version == ruleVersion && out.RuleSHA == e.ruleSHA && e.acceptReportBuild(out) && out.Source.Path == path && out.Source.Bytes == info.Size() && out.SourceModNS == info.ModTime().UnixNano() && out.RawRows == e.expected[index] && out.BaselineSHA == out.CacheBaselineSHA && out.Patch.Path == patchPath && out.Patch.Verify() == nil && out.Anchors.Path == anchorPath && out.Anchors.Verify() == nil && out.Unresolved.Path == unresolvedPath && out.Unresolved.Verify() == nil {
		return out, true, nil
	}
	if e.cfg.CompatibleWork != "" {
		if r, ok, er := e.importCompatible(ctx, index, info); ok || er != nil {
			return r, ok, er
		}
	}
	out = FileReport{Version: ruleVersion, RuleSHA: e.ruleSHA, BuildSHA: e.buildSHA, Index: index, SourceModNS: info.ModTime().UnixNano(), Validation: map[string]Validation{}, Examples: map[string][]Patch{}, SchemaSHA: e.schemaSHA}
	groups := map[string]*Group{}
	get := func(c, d, v string) *Stats {
		k := c + "|" + d + "|" + v
		g := groups[k]
		if g == nil {
			g = &Group{Corpus: c, Dimension: d, Value: v}
			groups[k] = g
		}
		return &g.Stats
	}
	baseline := sha256.New()
	readRows := func(fn func(RawWork) error) error { return each[RawWork](path, fn) }
	var bufferedArtifact checkpoint.Artifact
	if e.cfg.RawReaders > 0 {
		data, artifact, release, er := e.bufferSource(ctx, path, info.Size())
		if er != nil {
			return out, false, er
		}
		defer release()
		bufferedArtifact = artifact
		readRows = func(fn func(RawWork) error) error { return eachMemory(data, fn) }
	}
	aw, err := newSidecar[Anchor](anchorPath)
	if err != nil {
		return out, false, err
	}
	defer aw.abort()
	uw, err := newSidecar[Unresolved](unresolvedPath)
	if err != nil {
		return out, false, err
	}
	defer uw.abort()
	out.Patch, err = checkpoint.Write(patchPath, func(w io.Writer) error {
		pw := parquet.NewGenericWriter[Patch](w, parquet.Compression(&zstd.Codec{}), parquet.MaxRowsPerRowGroup(16384))
		pending := make([]Patch, 0, 128)
		flush := func() error {
			if len(pending) == 0 {
				return nil
			}
			_, er := pw.Write(pending)
			clear(pending)
			pending = pending[:0]
			return er
		}
		er := readRows(func(r RawWork) error {
			if out.RawRows%4096 == 0 {
				if er := ctx.Err(); er != nil {
					return er
				}
			}
			out.RawRows++
			id, er := scan.ParseWorkID(str(r.ID))
			if er != nil {
				return er
			}
			year := int32(0)
			if r.Year != nil {
				year = *r.Year
			}
			original := baseCountries(r.Authors)
			hashBaseline(baseline, id, year, str(r.Type), truth(r.XPAC), truth(r.Paratext), original)
			if !e.scope.Includes(model.Work{ID: id, Year: int(year), Type: str(r.Type), IsXPAC: truth(r.XPAC), IsParatext: truth(r.Paratext)}) {
				return nil
			}
			out.Eligible++
			p, s, matched := e.evaluate(r, id, year, original)
			anchors, unresolved := e.extraEvidence(r, id, year, original, s.NewCombined == 1)
			if er = aw.write(anchors); er != nil {
				return er
			}
			if unresolved != nil {
				if er = uw.write([]Unresolved{*unresolved}); er != nil {
					return er
				}
			}
			baseCorpus := "core"
			if truth(r.XPAC) {
				baseCorpus = "xpac"
			}
			for _, corpus := range []string{baseCorpus, "all"} {
				get(corpus, "window", "all_years").add(s)
				get(corpus, "year", strconv.Itoa(int(year))).add(s)
				get(corpus, "type", str(r.Type)).add(s)
				if year >= 2000 && year <= 2026 {
					get(corpus, "window", "2000_2026").add(s)
					get(corpus, "output_type", str(r.Type)).add(s)
				}
			}
			if len(original) > 0 {
				for _, c := range matched {
					key := baseCorpus + "/" + c
					v := out.Validation[key]
					v.Matched++
					if contains(original, c) {
						v.Subset++
					} else {
						v.Novel++
						if len(intersection(matched, original)) == 0 {
							v.Disjoint++
						}
					}
					out.Validation[key] = v
				}
			}
			if p != nil {
				pending = append(pending, *p)
				class := baseCorpus + "/"
				if p.Conflict {
					class += "conflict"
				} else if p.NewlyKnown && len(p.InstitutionAdded) > 0 && len(p.TextAdded) > 0 {
					class += "both"
				} else if p.NewlyKnown && len(p.InstitutionAdded) > 0 {
					class += "institution"
				} else if p.NewlyKnown {
					class += "text"
				} else if len(original) > 0 && len(p.Added) > 0 {
					class += "known_extension"
				} else {
					class += "review_only"
				}
				if len(out.Examples[class]) < 3 {
					out.Examples[class] = append(out.Examples[class], *p)
				}
				if len(pending) == cap(pending) {
					return flush()
				}
			}
			return nil
		})
		if er != nil {
			_ = pw.Close()
			return er
		}
		if er = flush(); er != nil {
			return er
		}
		return pw.Close()
	})
	if err != nil {
		return out, false, err
	}
	if out.Anchors, err = aw.finish(); err != nil {
		return out, false, err
	}
	out.AnchorRows = aw.rows
	if out.Unresolved, err = uw.finish(); err != nil {
		return out, false, err
	}
	out.UnresolvedRows = uw.rows
	if out.RawRows != e.expected[index] {
		return out, false, fmt.Errorf("raw row conservation %d != %d", out.RawRows, e.expected[index])
	}
	out.BaselineSHA = hex.EncodeToString(baseline.Sum(nil))
	flatHash := sha256.New()
	var flatRows int64
	cp := e.cache.Parts[index]
	if err = each[FlatWork](cp.Path, func(r FlatWork) error {
		hashBaseline(flatHash, r.ID, r.Year, r.Type, r.XPAC, r.Paratext, r.Countries)
		flatRows++
		return nil
	}); err != nil {
		return out, false, err
	}
	out.CacheBaselineSHA = hex.EncodeToString(flatHash.Sum(nil))
	if flatRows != out.RawRows || out.BaselineSHA != out.CacheBaselineSHA {
		return out, false, fmt.Errorf("all-row raw versus production-cache country/identity hash mismatch")
	}
	out.Cache, err = checkpoint.Hash(cp.Path)
	if err != nil {
		return out, false, err
	}
	if out.Cache.Bytes != cp.Bytes || out.Cache.SHA256 != cp.SHA256 {
		return out, false, fmt.Errorf("cache artifact hash mismatch")
	}
	if e.cfg.RawReaders > 0 {
		out.Source = bufferedArtifact
	} else {
		out.Source, err = checkpoint.Hash(path)
	}
	if err != nil {
		return out, false, err
	}
	after, err := os.Stat(path)
	if err != nil {
		return out, false, err
	}
	if info.Size() != after.Size() || info.ModTime() != after.ModTime() {
		return out, false, fmt.Errorf("source changed during evaluation")
	}
	for _, g := range groups {
		out.Groups = append(out.Groups, *g)
	}
	sort.Slice(out.Groups, func(i, j int) bool {
		a, b := out.Groups[i], out.Groups[j]
		return a.Corpus+"/"+a.Dimension+"/"+a.Value < b.Corpus+"/"+b.Dimension+"/"+b.Value
	})
	out.Seconds = time.Since(start).Seconds()
	err = checkpoint.JSON(metaPath, out)
	return out, false, err
}

func intersection(a, b []string) (out []string) {
	for _, s := range a {
		if contains(b, s) {
			out = append(out, s)
		}
	}
	return
}
