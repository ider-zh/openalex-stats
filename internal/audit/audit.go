// Package audit runs a DuckDB implementation independent of the Go graph and
// aggregation kernels, after an immutable production result has completed.
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"openalexstats/internal/cache"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/production"
	"openalexstats/internal/results"
)

type Config struct {
	ResultDir   string `json:"result_dir"`
	CacheDir    string `json:"cache_dir"`
	WorkDir     string `json:"work_dir"`
	DuckDB      string `json:"duckdb"`
	Threads     int    `json:"threads"`
	MemoryLimit string `json:"memory_limit"`
}
type Progress func(stage string, detail any)
type FileHash struct {
	Path     string `json:"path"`
	Bytes    int64  `json:"bytes"`
	SHA256   string `json:"sha256"`
	Verified bool   `json:"verified"`
}
type Check struct {
	Name      string  `json:"name"`
	Expected  float64 `json:"expected"`
	Observed  float64 `json:"observed"`
	Delta     float64 `json:"delta"`
	Tolerance float64 `json:"tolerance"`
	Passed    bool    `json:"passed"`
}
type Report struct {
	SelectedByCorpus   map[string]uint64    `json:"selected_by_corpus,omitempty"`
	SelectedTypeCounts map[string][3]uint64 `json:"selected_type_counts_core_xpac_unknown,omitempty"`
	Version            string               `json:"version"`
	Status             string               `json:"status"`
	Passed             bool                 `json:"passed"`
	RunID              string               `json:"run_id"`
	Config             Config               `json:"config"`
	StartedAt          time.Time            `json:"started_at"`
	CompletedAt        time.Time            `json:"completed_at"`
	DuckDBVersion      string               `json:"duckdb_version"`
	SQLSeconds         float64              `json:"sql_seconds"`
	Sources            []FileHash           `json:"sources"`
	Outputs            []FileHash           `json:"outputs"`
	Evidence           []FileHash           `json:"evidence"`
	Checks             []Check              `json:"checks"`
	FailedChecks       int                  `json:"failed_checks"`
	ModelAuditFile     string               `json:"model_audit_file,omitempty"`
	ModelAuditPassed   bool                 `json:"model_audit_passed"`
	ModelChecks        int                  `json:"model_checks"`
	Failure            string               `json:"failure,omitempty"`
	Notes              []string             `json:"notes"`
}

var auditedBlacklist = []string{"editorial", "erratum", "retraction", "paratext", "peer-review", "reference-entry", "libguides", "supplementary-materials", "dataset", "database", "software", "grant", "standard", "book", "dissertation"}
var evidenceNames = []string{"query.sql", "duckdb.stdout.log", "duckdb.stderr.log", "inventory.json", "q.json", "publication_year.json", "citation_year.json", "output_year.json", "output_cells.json", "output_counts.json", "output_integrity.json", "model-audit.json"}
var memoryPattern = regexp.MustCompile(`(?i)^[1-9][0-9]*(?:\.[0-9]+)?\s*(?:KB|MB|GB|TB|KIB|MIB|GIB|TIB)$`)

// Run requires completed production artifacts and refuses unsupported statistical
// choices rather than substituting defaults. It never starts the Go production
// computation. Config.WorkDir is an audit-owned directory (not ResultDir or
// CacheDir itself); its known evidence files are replaced on an explicit rerun.
func Run(ctx context.Context, cfg Config, progress Progress) (report Report, err error) {
	if ctx == nil {
		return report, errors.New("audit: nil context")
	}
	if cfg.Threads == 0 {
		cfg.Threads = 46
	}
	if cfg.MemoryLimit == "" {
		cfg.MemoryLimit = "512GB"
	}
	if cfg.DuckDB == "" {
		cfg.DuckDB = "duckdb"
	}
	if cfg.Threads < 1 || cfg.Threads > 128 || !memoryPattern.MatchString(cfg.MemoryLimit) || cfg.ResultDir == "" || cfg.CacheDir == "" || cfg.WorkDir == "" {
		return report, errors.New("audit: invalid directories, thread count or memory limit")
	}
	for _, p := range []*string{&cfg.ResultDir, &cfg.CacheDir, &cfg.WorkDir} {
		*p, err = filepath.Abs(*p)
		if err != nil {
			return report, err
		}
	}
	if cfg.WorkDir == cfg.ResultDir || cfg.WorkDir == cfg.CacheDir {
		return report, errors.New("audit: work directory must be separate from result/cache root")
	}
	if err := os.MkdirAll(cfg.WorkDir, 0755); err != nil {
		return report, err
	}
	unlock, err := checkpoint.LockDirectory(cfg.WorkDir)
	if err != nil {
		return report, err
	}
	defer unlock()
	for _, name := range evidenceNames {
		if err = os.Remove(filepath.Join(cfg.WorkDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return report, err
		}
	}
	if progress == nil {
		progress = func(string, any) {}
	}
	report = Report{Version: "independent-duckdb-audit-v2", Status: "running", Config: cfg, StartedAt: time.Now().UTC(), Checks: []Check{}, Notes: []string{
		"Independent implementation: normalized Parquet and exported result files only; Go edge/counter shards are not read.",
		"Corpus is read from the completed run: core requires is_xpac=false and quality_null_xpac=false; all includes both core and XPAC (plus flagged unknown-XPAC rows), subject to the same blacklist/paratext filters. Pre-2000 cited works remain in Q. The all-corpus graph is rebuilt jointly, never a sum of separate induced graphs.",
		"Citation year is citing publication year (2000..2026), both endpoints in Q, repeated references deduplicated, same-work loops excluded; author self-citations retained.",
		"Annual totals include UNKNOWN, with independent distinct-country and distinct-Concept assignment counts for every threshold/layer; whole Concepts do not conserve raw Works/edges; all 216 ECI/PCI models additionally undergo independent RCA/M and nonsymmetric-operator numerical checks from the main cube. Shares are not fully recomputed cell by cell.",
		"Nine independently weighted cells: CN/US/GB x Topics Fields 17/11 and Concepts Level 0 Biology, threshold .3 with within-level deduplication and the published Concept weighting (fractional or whole).",
		"One complete reference join emits annual edge counts and all representative target weights together. Hash verification also reads source bytes but performs no citation computation.",
		"Cache contents are SHA256 verified before SQL; source size/mtime is checked again afterwards. Inputs are trusted immutable artifacts, not protected against deliberate same-size/same-mtime concurrent mutation.",
	}}
	defer func() {
		report.CompletedAt = time.Now().UTC()
		if err != nil {
			report.Status, report.Passed, report.Failure = "failed", false, err.Error()
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				report.Status = "canceled"
			}
		}
		for _, name := range evidenceNames {
			if h, e := hashFile(context.Background(), filepath.Join(cfg.WorkDir, name)); e == nil {
				report.Evidence = append(report.Evidence, h)
			} else if err == nil {
				err = fmt.Errorf("audit: cannot hash required evidence %s: %w", name, e)
				report.Status, report.Passed, report.Failure = "failed", false, err.Error()
			}
		}
		if e := checkpoint.JSON(filepath.Join(cfg.WorkDir, "audit.json"), report); e != nil && err == nil {
			err = e
			report.Status, report.Passed, report.Failure = "failed", false, e.Error()
		}
	}()
	var run production.Report
	if err = readJSON(filepath.Join(cfg.ResultDir, "run.json"), &run); err != nil {
		return report, err
	}
	report.RunID = run.RunID
	var scenarios []results.Scenario
	if err = readJSON(filepath.Join(cfg.ResultDir, "scenarios.json"), &scenarios); err != nil {
		return report, err
	}
	if err = supported(run, scenarios); err != nil {
		return report, err
	}
	var manifest cache.Manifest
	if err = readJSON(filepath.Join(cfg.CacheDir, "manifest.json"), &manifest); err != nil {
		return report, err
	}
	if !manifest.Complete || manifest.FormatVersion != cache.FormatVersion || len(manifest.Parts) == 0 || manifest.SourceManifest != run.Config.Input.ManifestSHA256 || len(manifest.Parts) != len(run.Config.Input.Paths) || uint64(manifest.Rows) != run.SourceRows {
		return report, errors.New("audit: complete full cache identity does not match production inputs")
	}
	inputSet := map[string]bool{}
	for _, path := range run.Config.Input.Paths {
		abs, e := filepath.Abs(path)
		if e != nil {
			return report, e
		}
		inputSet[abs] = true
	}
	paths := make([]string, len(manifest.Parts))
	expected := make([]FileHash, len(manifest.Parts))
	stamps := make([]fileStamp, len(manifest.Parts))
	for i, part := range manifest.Parts {
		if !inputSet[part.InputPath] || part.Path == "" || part.SHA256 == "" || part.Rows < 0 {
			return report, errors.New("audit: cache part identity is invalid")
		}
		delete(inputSet, part.InputPath)
		paths[i] = part.Path
		expected[i] = FileHash{Path: part.Path, Bytes: part.Bytes, SHA256: part.SHA256}
		stamps[i], err = stamp(part.Path)
		if err != nil {
			return report, err
		}
	}
	if len(inputSet) != 0 {
		return report, errors.New("audit: cache part source coverage differs")
	}
	usedCacheKey, e := cacheIdentity(manifest, run.Config.Input.Paths)
	if e != nil {
		return report, e
	}
	var methodology struct {
		RunID    string `json:"run_id"`
		CacheKey string `json:"normalized_cache_key"`
	}
	if e = readJSON(filepath.Join(cfg.ResultDir, "methodology.json"), &methodology); e != nil {
		return report, fmt.Errorf("audit: published cache identity required: %w", e)
	}
	if methodology.RunID != run.RunID || methodology.CacheKey != usedCacheKey {
		return report, errors.New("audit: normalized cache content identity differs from the cache used by production")
	}
	progress("verify_cache_hashes", map[string]any{"parts": len(expected)})
	report.Sources, err = verifyFiles(ctx, expected, min(cfg.Threads, 8), progress)
	if err != nil {
		return report, err
	}
	if h, e := hashFile(ctx, filepath.Join(cfg.CacheDir, "manifest.json")); e == nil {
		report.Sources = append(report.Sources, h)
	} else {
		return report, e
	}
	var completion struct {
		Version   int        `json:"version"`
		RunID     string     `json:"run_id"`
		Run       FileHash   `json:"run"`
		Artifacts []FileHash `json:"artifacts"`
	}
	if err = readJSON(filepath.Join(cfg.ResultDir, "completion.json"), &completion); err != nil {
		return report, fmt.Errorf("audit: result completion marker required: %w", err)
	}
	if completion.Version != 1 || completion.RunID != run.RunID || len(completion.Artifacts) == 0 {
		return report, errors.New("audit: invalid result completion identity")
	}
	files := append([]FileHash{completion.Run}, completion.Artifacts...)
	completionHash, e := hashFile(ctx, filepath.Join(cfg.ResultDir, "completion.json"))
	if e != nil {
		return report, e
	}
	files = append(files, completionHash)
	seenArtifacts := map[string]bool{}
	for _, artifact := range files {
		if filepath.Dir(artifact.Path) != cfg.ResultDir {
			return report, errors.New("audit: completion artifact is outside result directory")
		}
		name := filepath.Base(artifact.Path)
		if seenArtifacts[name] {
			return report, errors.New("audit: duplicate completion artifact")
		}
		seenArtifacts[name] = true
	}
	for _, required := range []string{"run.json", "scenarios.json", "cube.parquet", "statistics.parquet", "methodology.json", "dictionary.json", "eci.json", "country_eci.parquet", "discipline_pci.parquet"} {
		if !seenArtifacts[required] {
			return report, fmt.Errorf("audit: completion marker omits %s", required)
		}
	}
	report.Outputs, err = verifyFiles(ctx, files, min(cfg.Threads, 8), progress)
	if err != nil {
		return report, err
	}
	if err = os.MkdirAll(filepath.Join(cfg.WorkDir, "duckdb-temp"), 0755); err != nil {
		return report, err
	}
	sql := buildSQLForCorpus(cfg, paths, run.Config.Scope.Blacklist, run.Config.Scope.Corpus, scenarios)
	if err = os.WriteFile(filepath.Join(cfg.WorkDir, "query.sql"), []byte(sql), 0600); err != nil {
		return report, err
	}
	version, e := exec.CommandContext(ctx, cfg.DuckDB, "--version").CombinedOutput()
	if e != nil {
		return report, fmt.Errorf("audit: DuckDB version check failed: %w (%s)", e, strings.TrimSpace(string(version)))
	}
	report.DuckDBVersion = strings.TrimSpace(string(version))
	progress("duckdb_oracle", map[string]any{"status": "started", "threads": cfg.Threads, "memory_limit": cfg.MemoryLimit, "sql": filepath.Join(cfg.WorkDir, "query.sql")})
	started := time.Now()
	err = execute(ctx, cfg, sql)
	report.SQLSeconds = time.Since(started).Seconds()
	if err != nil {
		return report, err
	}
	for i, path := range paths {
		current, e := stamp(path)
		if e != nil || current != stamps[i] {
			return report, fmt.Errorf("audit: normalized input changed during oracle query: %s", path)
		}
	}
	// Result files are much smaller than the normalized graph and are rehashed
	// after SQL so comparisons bind to exactly the initially published outputs.
	if _, err = verifyFiles(ctx, files, min(cfg.Threads, 8), progress); err != nil {
		return report, fmt.Errorf("audit: outputs changed during oracle query: %w", err)
	}
	if err = compareEvidence(cfg.WorkDir, run, &report, scenarios); err != nil {
		return report, err
	}
	progress("model_audit", map[string]any{"status": "started", "models": 216})
	report.ModelAuditFile = filepath.Join(cfg.WorkDir, "model-audit.json")
	modelReport, modelErr := AuditModels(ctx, cfg.ResultDir, report.ModelAuditFile)
	report.ModelAuditPassed = modelReport.Passed
	report.ModelChecks = len(modelReport.Models)
	report.check("models/expected_slots", 216, float64(len(modelReport.Models)), false)
	for _, model := range modelReport.Models {
		value := 0.0
		if model.Passed {
			value = 1
		}
		report.check("model/"+modelID(model.Year, model.Scheme, model.Level, model.Metric), 1, value, false)
	}
	for _, exported := range modelReport.Exports {
		value := 0.0
		if exported.Passed {
			value = 1
		}
		report.check("model_export/"+exported.File, 1, value, false)
	}
	if modelErr != nil {
		return report, modelErr
	}
	if report.FailedChecks != 0 {
		return report, fmt.Errorf("audit: %d of %d independent checks failed", report.FailedChecks, len(report.Checks))
	}
	report.Status, report.Passed = "passed", true
	progress("audit_complete", map[string]any{"passed": true, "checks": len(report.Checks), "sql_seconds": report.SQLSeconds})
	return report, nil
}

// cacheIdentity binds the oracle to the exact normalized bytes used by the
// controlled production-v1 schema. This is metadata hashing, not a reuse of its
// graph/count implementation. Production orders parts by the raw input list.
func cacheIdentity(m cache.Manifest, inputs []string) (string, error) {
	type identity struct {
		Source, SHA string
		Rows        int64
	}
	bySource := map[string]cache.Part{}
	for _, p := range m.Parts {
		bySource[p.InputPath] = p
	}
	parts := make([]identity, len(inputs))
	for i, input := range inputs {
		absolute, err := filepath.Abs(input)
		if err != nil {
			return "", err
		}
		p, ok := bySource[absolute]
		if !ok {
			return "", errors.New("audit: cache identity source missing")
		}
		parts[i] = identity{p.SourceFingerprint, p.SHA256, p.Rows}
	}
	return checkpoint.Key(struct {
		Version, Build, Manifest, Input string
		Parts                           []identity
	}{m.FormatVersion, m.BuildID, m.SourceManifest, m.InputSHA256, parts})
}

func supported(run production.Report, scenarios []results.Scenario) error {
	if run.Version != "production-v1" || run.Status != "full_snapshot_complete" || run.RunID == "" || (run.Config.Scope.Corpus != "core" && run.Config.Scope.Corpus != "all") || run.Config.Scope.CorpusYearStart != 0 || run.Config.YearStart != 2000 || run.Config.YearEnd != 2026 || run.Config.IncludeWorkSelfLoops || run.Config.Input.Truncated || run.Config.MaxRows != 0 || run.Config.Input.ManifestSHA256 == "" || run.Config.Input.ManifestFiles < 1 || run.Config.Input.ManifestFiles != len(run.Config.Input.Paths) || uint64(run.Config.Input.ManifestWorks) != run.SourceRows {
		return errors.New("audit: unsupported production scope; requires completed full core/all, pre-2000 targets, 2000..2026 and excluded same-work loops")
	}
	a, b := append([]string(nil), auditedBlacklist...), append([]string(nil), run.Config.Scope.Blacklist...)
	slices.Sort(a)
	slices.Sort(b)
	if !slices.Equal(a, b) {
		return errors.New("audit: unsupported blacklist")
	}
	return validateAuditScenarios(scenarios)
}

func execute(ctx context.Context, cfg Config, sql string) error {
	out, err := os.Create(filepath.Join(cfg.WorkDir, "duckdb.stdout.log"))
	if err != nil {
		return err
	}
	defer out.Close()
	errLog, err := os.Create(filepath.Join(cfg.WorkDir, "duckdb.stderr.log"))
	if err != nil {
		return err
	}
	defer errLog.Close()
	command := exec.CommandContext(ctx, cfg.DuckDB, "-bail", ":memory:")
	command.Stdin, command.Stdout, command.Stderr = strings.NewReader(sql), out, errLog
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("audit: DuckDB oracle failed: %w; inspect duckdb.stderr.log", err)
	}
	return nil
}

type fileStamp struct{ Size, Modified int64 }

func stamp(path string) (fileStamp, error) {
	f, err := os.Stat(path)
	if err != nil {
		return fileStamp{}, err
	}
	if !f.Mode().IsRegular() {
		return fileStamp{}, errors.New("audit: source is not a regular file")
	}
	return fileStamp{f.Size(), f.ModTime().UnixNano()}, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func hashFile(ctx context.Context, path string) (FileHash, error) {
	f, err := os.Open(path)
	if err != nil {
		return FileHash{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, contextReader{ctx, f})
	if err != nil {
		return FileHash{}, err
	}
	return FileHash{Path: path, Bytes: n, SHA256: hex.EncodeToString(h.Sum(nil)), Verified: true}, nil
}
func verifyFiles(ctx context.Context, files []FileHash, workers int, progress Progress) ([]FileHash, error) {
	out := make([]FileHash, len(files))
	errs := make([]error, len(files))
	jobs := make(chan int)
	var wg sync.WaitGroup
	var progressMu sync.Mutex
	completed := 0
	for range min(workers, max(1, len(files))) {
		wg.Go(func() {
			for i := range jobs {
				out[i], errs[i] = hashFile(ctx, files[i].Path)
				if errs[i] == nil && (out[i].Bytes != files[i].Bytes || out[i].SHA256 != files[i].SHA256) {
					errs[i] = fmt.Errorf("audit: hash mismatch: %s", files[i].Path)
				}
				progressMu.Lock()
				completed++
				if completed == 1 || completed%100 == 0 || completed == len(files) {
					progress("hash_checked", map[string]any{"files": completed, "total": len(files)})
				}
				progressMu.Unlock()
			}
		})
	}
	for i := range files {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return out, err
		}
	}
	return out, nil
}
func readJSON(path string, value any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(value)
}
func (r *Report) check(name string, expected, observed float64, fractional bool) {
	tolerance := 0.0
	if fractional {
		tolerance = 1e-6 + 1e-10*math.Abs(expected)
	}
	delta := observed - expected
	pass := !math.IsNaN(observed) && !math.IsInf(observed, 0) && math.Abs(delta) <= tolerance
	r.Checks = append(r.Checks, Check{name, expected, observed, delta, tolerance, pass})
	if !pass {
		r.FailedChecks++
	}
}
