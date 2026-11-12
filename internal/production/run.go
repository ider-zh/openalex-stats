package production

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/aggregate"
	"openalexstats/internal/cache"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/graph"
	"openalexstats/internal/model"
	"openalexstats/internal/pipeline"
	"openalexstats/internal/policy"
	"openalexstats/internal/results"
	"openalexstats/internal/store"
)

type Config struct {
	Base                    pipeline.Config    `json:"base"`
	CacheDirectory          string             `json:"cache_directory"`
	WorkDirectory           string             `json:"work_directory"`
	CounterDirectory        string             `json:"counter_directory"`
	DictionaryManifest      string             `json:"dictionary_manifest"`
	BuildID                 string             `json:"build_id"`
	Scenarios               []results.Scenario `json:"scenarios,omitempty"`
	EdgeCacheDirectory      string             `json:"edge_cache_directory,omitempty"`
	IndexWorkers            int                `json:"index_workers"`
	IndexShards             int                `json:"index_shards"`
	SerialIndex             bool               `json:"serial_index"`
	EdgeWorkers             int                `json:"edge_workers"`
	CountWorkers            int                `json:"count_workers"`
	Partitions              int                `json:"partitions"`
	MaxPartBufferBytes      uint64             `json:"max_part_buffer_bytes"`
	HostAvailableFloorBytes uint64             `json:"host_available_floor_bytes"`
}
type Report struct {
	Version                                  string                              `json:"version"`
	Status                                   string                              `json:"status"`
	RunID                                    string                              `json:"run_id"`
	BuildID                                  string                              `json:"build_id"`
	DictionaryManifestFingerprint            checkpoint.Artifact                 `json:"dictionary_manifest_fingerprint"`
	DictionarySHA256                         string                              `json:"dictionary_sha256"`
	Config                                   pipeline.Config                     `json:"config"`
	Production                               Config                              `json:"production"`
	StartedAt                                time.Time                           `json:"started_at"`
	CompletedAt                              time.Time                           `json:"completed_at"`
	StageSeconds                             map[string]float64                  `json:"stage_seconds"`
	StageResources                           map[string]pipeline.ResourceSummary `json:"stage_resources"`
	Resources                                pipeline.ResourceSummary            `json:"resources"`
	SourceRows                               uint64                              `json:"source_rows"`
	SelectedWorks                            int                                 `json:"selected_works"`
	Inventory                                Inventory                           `json:"inventory"`
	Graph                                    EdgeStats                           `json:"graph"`
	Quality                                  aggregate.Quality                   `json:"quality"`
	DimensionBytes, IndexBytes, CounterBytes uint64
	DimensionReused                          bool       `json:"dimension_reused"`
	EdgePartsReused                          int        `json:"edge_parts_reused"`
	CountPartitionsReused                    int        `json:"count_partitions_reused"`
	CubeRows                                 int        `json:"cube_rows"`
	StatisticsRows                           int        `json:"statistics_rows"`
	Models                                   int        `json:"models"`
	Validation                               Validation `json:"validation"`
	Notes                                    []string   `json:"notes"`
}
type Validation struct {
	RawRowConservation         bool `json:"raw_row_conservation"`
	SelectedRowConservation    bool `json:"selected_row_conservation"`
	EdgeCounterConservation    bool `json:"edge_counter_conservation"`
	FractionalCubeConservation bool `json:"fractional_cube_conservation"`
	AssignmentConservation     bool `json:"country_fractional_assignment_conservation"`
	WholeCountIntegrality      bool `json:"whole_count_integrality"`
	ScenarioRawCountsEqual     bool `json:"scenario_raw_counts_equal"`
}
type outputCompletion struct {
	Version   int                   `json:"version"`
	RunID     string                `json:"run_id"`
	Run       checkpoint.Artifact   `json:"run"`
	Artifacts []checkpoint.Artifact `json:"artifacts"`
}

func Run(ctx context.Context, cfg Config, progress Progress) (report Report, err error) {
	invokedAt := time.Now().UTC()
	base := cfg.Base
	if base.Workers < 1 || base.Workers > 128 || base.Readers < 1 || base.Readers > base.Workers || base.MaxRows != 0 || base.YearStart < 1 || base.YearEnd < base.YearStart || base.YearEnd-base.YearStart > 255 || base.MaxMemoryBytes < 256<<20 || base.MaxMemoryBytes > 1<<50 {
		return report, fmt.Errorf("invalid production execution settings; use unrestricted per-file inputs")
	}
	if cfg.WorkDirectory == "" || cfg.CacheDirectory == "" || base.OutputDir == "" || cfg.DictionaryManifest == "" || cfg.BuildID == "" {
		return report, fmt.Errorf("production requires cache/work/output/dictionary/build identities")
	}
	if cfg.Partitions == 0 {
		cfg.Partitions = 64
	}
	if cfg.Partitions < 1 || cfg.Partitions > 256 {
		return report, fmt.Errorf("invalid target partition count")
	}
	if cfg.EdgeWorkers == 0 {
		cfg.EdgeWorkers = base.Workers
	}
	if cfg.CountWorkers == 0 {
		cfg.CountWorkers = min(base.Workers, 16)
	}
	if cfg.EdgeWorkers < 1 || cfg.CountWorkers < 1 {
		return report, fmt.Errorf("invalid stage workers")
	}
	if cfg.IndexWorkers == 0 {
		cfg.IndexWorkers = min(base.Workers, 32)
	}
	if cfg.IndexShards == 0 {
		cfg.IndexShards = 256
	}
	if cfg.IndexWorkers < 1 || cfg.IndexWorkers > base.Workers || cfg.IndexShards < 1 || cfg.IndexShards > 65536 {
		return report, fmt.Errorf("invalid parallel index budget")
	}
	if cfg.EdgeCacheDirectory == "" {
		cfg.EdgeCacheDirectory = filepath.Join(cfg.WorkDirectory, "edges")
	}
	if cfg.CounterDirectory == "" {
		cfg.CounterDirectory = filepath.Join(cfg.WorkDirectory, "counters")
	}
	if progress == nil {
		progress = func(string, any) {}
	}
	scope, e := policy.New(base.Scope.Corpus, base.Scope.CorpusYearStart, base.Scope.Blacklist)
	if e != nil {
		return report, e
	}
	base.Scope = scope
	scenarios, e := results.ResolveScenarios(cfg.Scenarios)
	if e != nil {
		return report, e
	}
	cfg.Scenarios = scenarios
	var mainScenario results.Scenario
	for _, s := range scenarios {
		if s.ForECI() {
			mainScenario = s
		}
	}
	base.Weighting = mainScenario.Config()
	cfg.Base = base
	unlock, e := checkpoint.LockDirectory(cfg.WorkDirectory)
	if e != nil {
		return report, e
	}
	defer unlock()
	outputUnlock, e := checkpoint.LockDirectory(base.OutputDir)
	if e != nil {
		return report, e
	}
	defer outputUnlock()
	edgeUnlock, e := checkpoint.LockDirectory(cfg.EdgeCacheDirectory)
	if e != nil {
		return report, e
	}
	defer edgeUnlock()
	oldProcs := runtime.GOMAXPROCS(base.Workers)
	defer runtime.GOMAXPROCS(oldProcs)
	oldMemory := debug.SetMemoryLimit(int64(base.MaxMemoryBytes * 85 / 100))
	defer debug.SetMemoryLimit(oldMemory)
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	stopGuard := startGuard(ctx, cancel, base.MaxMemoryBytes, cfg.HostAvailableFloorBytes)
	defer stopGuard()
	stopResources := pipeline.StartResourceSampler()
	resourcesStopped := false
	defer func() {
		if !resourcesStopped {
			stopResources()
		}
	}()
	validationStarted := time.Now()
	stopValidation := pipeline.StartResourceSampler()
	progress("cache_validation", map[string]any{"status": "started"})
	normalized, e := loadCacheParallel(ctx, cfg.CacheDirectory, base.Input, base.Readers, progress)
	validationResources := stopValidation()
	validationSeconds := time.Since(validationStarted).Seconds()
	if e != nil {
		return report, e
	}
	progress("cache_validation", map[string]any{"status": "complete", "seconds": validationSeconds})
	cacheKey, e := stableCacheKey(normalized)
	if e != nil {
		return report, e
	}
	qKey, e := checkpoint.Key(struct {
		Version, Cache string
		Scope          policy.Scope
	}{"q-columns-v1", cacheKey, scope})
	if e != nil {
		return report, e
	}
	graphKey, e := checkpoint.Key(struct {
		Version, Q             string
		Start, End, Partitions int
		ExcludeLoops           bool
	}{"bucketed-reference-v2", qKey, base.YearStart, base.YearEnd, cfg.Partitions, !base.IncludeWorkSelfLoops})
	if e != nil {
		return report, e
	}
	// A shared counter root is safe across different Q/graph configurations.
	targetDirectory := filepath.Join(cfg.CounterDirectory, graphKey[:16])
	counterUnlock, e := checkpoint.LockDirectory(targetDirectory)
	if e != nil {
		return report, e
	}
	defer counterUnlock()
	dictionaryManifest, e := checkpoint.Hash(cfg.DictionaryManifest)
	if e != nil {
		return report, e
	}
	progress("dictionary_validation", map[string]any{"status": "started"})
	dictionaryStarted := time.Now()
	stopDictionary := pipeline.StartResourceSampler()
	dictionary, e := results.LoadDictionary(ctx, cfg.DictionaryManifest)
	dictionaryResources := stopDictionary()
	dictionarySeconds := time.Since(dictionaryStarted).Seconds()
	if e != nil {
		return report, e
	}
	dictionaryKey, e := checkpoint.Key(dictionary)
	if e != nil {
		return report, e
	}
	progress("dictionary_validation", map[string]any{"status": "complete", "seconds": dictionarySeconds})
	runID, e := checkpoint.Key(struct {
		Version, Graph, Build, DictionaryManifest, Dictionary string
		Scenarios                                             []results.Scenario
	}{"production-v2", graphKey, cfg.BuildID, dictionaryManifest.SHA256, dictionaryKey, scenarios})
	if e != nil {
		return report, e
	}
	workIdentity := struct{ Q, Graph string }{qKey, graphKey}
	var previousIdentity struct{ Q, Graph string }
	identityPath := filepath.Join(cfg.WorkDirectory, "identity.json")
	if e = checkpoint.ReadJSON(identityPath, &previousIdentity); e == nil && previousIdentity != workIdentity {
		return report, fmt.Errorf("work directory belongs to a different Q/graph; use another directory")
	}
	if e = checkpoint.JSON(identityPath, workIdentity); e != nil {
		return report, e
	}
	if prior, e := verifiedCompletion(base.OutputDir, runID); e == nil {
		return prior, nil
	}
	if e = os.MkdirAll(base.OutputDir, 0755); e != nil {
		return report, e
	}
	var previous Report
	if e = checkpoint.ReadJSON(filepath.Join(base.OutputDir, "run.json"), &previous); e == nil && previous.RunID != "" && previous.RunID != runID {
		return report, fmt.Errorf("output directory belongs to a different immutable run")
	}
	report = Report{Version: "production-v1", Status: "running", RunID: runID, BuildID: cfg.BuildID, Config: base, Production: cfg, StartedAt: time.Now().UTC(), StageSeconds: map[string]float64{}, StageResources: map[string]pipeline.ResourceSummary{}, Notes: []string{fmt.Sprintf("Corpus=%s; corpus year lower bound=%d; all scope parameters recorded explicitly.", scope.Corpus, scope.CorpusYearStart), "Same-work loops are excluded unless explicitly overridden; author self-citations are retained.", fmt.Sprintf("Eight ECI models use %s. Other weighting and score sensitivity statistics are separate scenarios.", mainScenario.ID), "Unknown country/discipline rows remain in statistics; ECI/RCA matrix denominators use known cells.", "2026 is partial through snapshot date. Missing/invalid/disconnected model scores are not zero scores.", "Raw reference graph reflects snapshot coverage; absent snapshot targets cannot be inferred."}}
	report.StartedAt = invokedAt
	report.DictionaryManifestFingerprint = dictionaryManifest
	report.DictionarySHA256 = dictionaryKey
	report.StageSeconds["cache_validation"] = validationSeconds
	report.StageResources["cache_validation"] = validationResources
	report.StageSeconds["dictionary_validation"] = dictionarySeconds
	report.StageResources["dictionary_validation"] = dictionaryResources
	defer func() {
		report.Resources = stopResources()
		resourcesStopped = true
		report.CompletedAt = time.Now().UTC()
		if err != nil {
			report.Status = "failed"
			if cause := context.Cause(ctx); cause != nil {
				report.Notes = append(report.Notes, cause.Error())
			}
			report.Notes = append(report.Notes, err.Error())
		}
		if e := checkpoint.JSON(filepath.Join(base.OutputDir, "run.json"), report); e != nil && err == nil {
			err = e
		}
		if err == nil && (report.Status == "full_snapshot_complete" || report.Status == "pilot_complete") {
			if e := publishCompletion(base.OutputDir, report); e != nil {
				err = e
			}
		}
	}()
	stage := func(name string, work func() error) error {
		progress(name, map[string]any{"status": "started"})
		started := time.Now()
		stop := pipeline.StartResourceSampler()
		e := work()
		report.StageSeconds[name] = time.Since(started).Seconds()
		report.StageResources[name] = stop()
		_ = checkpoint.JSON(filepath.Join(cfg.WorkDirectory, "attempt-latest.json"), report)
		progress(name, map[string]any{"seconds": report.StageSeconds[name], "status": map[bool]string{true: "failed", false: "complete"}[e != nil]})
		return e
	}
	var dims dimensionResult
	if err = stage("dimensions", func() error {
		var e error
		dims, e = dimensions(ctx, normalized.Paths(), cfg.WorkDirectory, qKey, scope, base.MaxMemoryBytes, base.Readers, progress)
		return e
	}); err != nil {
		return report, err
	}
	report.Inventory = dims.Checkpoint.Inventory
	report.SourceRows = report.Inventory.Rows
	report.SelectedWorks = dims.Columns.Len()
	report.DimensionBytes = dims.Columns.EstimatedBytes()
	report.DimensionReused = dims.Reused
	if report.SourceRows != uint64(normalized.Rows) || uint64(len(dims.AllIDs)) != report.SourceRows {
		return report, fmt.Errorf("cache/dimension raw-row conservation failed")
	}
	report.Validation.RawRowConservation = true
	var excluded uint64
	for _, n := range report.Inventory.ExcludedReasons {
		excluded += n
	}
	if excluded+report.Inventory.Selected != report.SourceRows || report.Inventory.Selected != uint64(report.SelectedWorks) {
		return report, fmt.Errorf("selected/excluded row conservation failed")
	}
	report.Validation.SelectedRowConservation = true
	var index, allIndex *graph.Index
	if err = stage("indices", func() error {
		qEstimate, e := graph.EstimateIndexBytes(dims.Columns.Len())
		if e != nil {
			return e
		}
		allEstimate, e := graph.EstimateIndexBytes(len(dims.AllIDs))
		if e != nil {
			return e
		}
		peakEstimate := qEstimate + allEstimate
		if !cfg.SerialIndex {
			qMemory, e := graph.EstimateParallelIndexBytes(dims.Columns.Len(), cfg.IndexWorkers, cfg.IndexShards)
			if e != nil {
				return e
			}
			allMemory, e := graph.EstimateParallelIndexBytes(len(dims.AllIDs), cfg.IndexWorkers, cfg.IndexShards)
			if e != nil {
				return e
			}
			qEstimate, allEstimate = qMemory.FinalBytes, allMemory.FinalBytes
			peakEstimate = qMemory.PeakBytes + allMemory.PeakBytes
		}
		counterBytes := uint64(dims.Columns.Len()) * uint64(base.YearEnd-base.YearStart+1) * 8
		if dims.Columns.EstimatedBytes()+uint64(cap(dims.AllIDs))*8+peakEstimate+counterBytes > base.MaxMemoryBytes*70/100 {
			return fmt.Errorf("dimensions+indices+counters exceed 70%% memory budget")
		}
		buildIndex := func(ids []uint64) (*graph.Index, error) {
			if cfg.SerialIndex {
				return graph.NewIndexContext(ctx, ids)
			}
			return graph.NewParallelIndexContext(ctx, ids, cfg.IndexWorkers, cfg.IndexShards)
		}
		progress("index_configuration", map[string]any{"serial": cfg.SerialIndex, "workers": cfg.IndexWorkers, "shards": cfg.IndexShards, "estimated_peak_index_bytes": peakEstimate})
		index, e = buildIndex(dims.Columns.IDs())
		if e != nil {
			return e
		}
		allIndex, e = buildIndex(dims.AllIDs)
		report.IndexBytes = qEstimate + allEstimate
		return e
	}); err != nil {
		return report, err
	}
	dims.AllIDs = nil
	var edges EdgeManifest
	if err = stage("reference_projection", func() error {
		var e error
		edges, e = ProjectEdges(ctx, normalized.Paths(), index, EdgeOptions{Directory: cfg.EdgeCacheDirectory, Key: graphKey, Workers: cfg.EdgeWorkers, Partitions: cfg.Partitions, YearStart: base.YearStart, YearEnd: base.YearEnd, Scope: scope, ExcludeSelfLoops: !base.IncludeWorkSelfLoops, AllIndex: allIndex, Columns: dims.Columns, MaxPartBufferBytes: cfg.MaxPartBufferBytes}, progress)
		return e
	}); err != nil {
		return report, err
	}
	report.Graph = edges.Stats
	report.EdgePartsReused = edges.ReusedParts
	if report.Graph.MemberSourceRecords != uint64(report.SelectedWorks) || report.Graph.SourceRecords != report.SourceRows {
		return report, fmt.Errorf("source graph coverage differs from dimension corpus")
	}
	index = nil
	allIndex = nil
	runtime.GC()
	var counts *Counts
	if err = stage("citation_counts", func() error {
		var e error
		counts, e = ReduceCounts(ctx, edges, dims.Columns.Len(), CountOptions{Directory: targetDirectory, Key: graphKey, Workers: cfg.CountWorkers, MaxBytes: base.MaxMemoryBytes * 50 / 100}, progress)
		return e
	}); err != nil {
		return report, err
	}
	report.CounterBytes = counts.Bytes()
	report.CountPartitionsReused = counts.ReusedPartitions
	report.Graph.CounterBytes = counts.Bytes()
	report.Validation.EdgeCounterConservation = counts.Sum == edges.Stats.RetainedEdges
	if err = stage("dictionary", func() error {
		var e error
		dictionary, e = reconcileVocabulary(dictionary, dims.Columns.Vocabulary())
		return e
	}); err != nil {
		return report, err
	}
	var inputs []results.Input
	if err = stage("aggregate", func() error {
		var e error
		inputs, e = reduceDense(ctx, dims.Columns, counts, dictionary, scenarios, base, progress)
		return e
	}); err != nil {
		return report, err
	}
	counts = nil
	runtime.GC()
	var mainRows []aggregate.Row
	for _, input := range inputs {
		if input.Scenario.ForECI() {
			mainRows = input.Rows
			report.Quality = input.Quality
		}
	}
	if err = validateInputs(inputs, report.Graph.RetainedEdges); err != nil {
		return report, err
	}
	report.Validation.FractionalCubeConservation = true
	report.Validation.AssignmentConservation = true
	report.Validation.WholeCountIntegrality = true
	report.Validation.ScenarioRawCountsEqual = true
	report.CubeRows = len(mainRows)
	var derived []results.Row
	var coverage []results.Coverage
	if err = stage("derive", func() error { var e error; derived, coverage, e = results.Derive(inputs); return e }); err != nil {
		return report, err
	}
	report.StatisticsRows = len(derived)
	var models []pipeline.ModelResult
	if err = stage("eci", func() error {
		var e error
		models, e = pipeline.ComputeModels(ctx, mainRows, base.Workers, base.YearStart, base.YearEnd, &dictionary)
		return e
	}); err != nil {
		return report, err
	}
	report.Models = len(models)
	if err = stage("export", func() error {
		if e := results.Export(base.OutputDir, derived, coverage, scenarios, models, dictionary); e != nil {
			return e
		}
		if e := writeMainCube(filepath.Join(base.OutputDir, "cube.parquet"), mainRows); e != nil {
			return e
		}
		return checkpoint.JSON(filepath.Join(base.OutputDir, "methodology.json"), map[string]any{"run_id": runID, "build_id": cfg.BuildID, "corpus": scope, "citation_year": "citing publication year", "citation_attribution": "cited work countries and discipline", "both_endpoints_in_Q": true, "exclude_same_work_loops": !base.IncludeWorkSelfLoops, "author_self_citations": "retained", "output_year_start": base.YearStart, "output_year_end": base.YearEnd, "partial_years": []int{2026}, "main_scenario": mainScenario.ID, "scenarios": scenarios, "rca_world_denominator": "known-country x known-discipline matrix sum", "source_manifest_sha256": base.Input.ManifestSHA256, "normalized_cache_key": cacheKey, "graph_key": graphKey, "go_version": runtime.Version(), "statistical_specification": "docs/counting-policy.md", "concepts_counting": mainScenario.ConceptMode, "concept_hit_rule": "deduplicate same-level labels; whole mode counts every qualifying hit once; scores filter tags, not proportional weights", "national_total_rule": "distinct Works per country/year; do not sum overlapping subject whole counts"})
	}); err != nil {
		return report, err
	}
	report.Status = "pilot_complete"
	if base.Input.ManifestSHA256 != "" && !base.Input.Truncated && base.Input.ManifestFiles == len(base.Input.Paths) {
		if report.SourceRows != uint64(base.Input.ManifestWorks) {
			return report, fmt.Errorf("full source manifest row count mismatch")
		}
		report.Status = "full_snapshot_complete"
	}
	return report, nil
}

func stableCacheKey(m cache.Manifest) (string, error) {
	type identity struct {
		Source, SHA string
		Rows        int64
	}
	parts := make([]identity, len(m.Parts))
	for i, p := range m.Parts {
		parts[i] = identity{p.SourceFingerprint, p.SHA256, p.Rows}
	}
	return checkpoint.Key(struct {
		Version, Build, Manifest, Input string
		Parts                           []identity
	}{m.FormatVersion, m.BuildID, m.SourceManifest, m.InputSHA256, parts})
}
func loadCache(ctx context.Context, directory string, input pipeline.Input, progress Progress) (cache.Manifest, error) {
	var m cache.Manifest
	if e := checkpoint.ReadJSON(filepath.Join(directory, "manifest.json"), &m); e != nil {
		return m, e
	}
	if !m.Complete || m.FormatVersion != cache.FormatVersion {
		return m, fmt.Errorf("normalized cache not complete or format unsupported")
	}
	if input.ManifestSHA256 != "" && m.SourceManifest != input.ManifestSHA256 {
		return m, fmt.Errorf("cache belongs to a different snapshot manifest")
	}
	bySource := map[string]cache.Part{}
	for _, p := range m.Parts {
		bySource[p.InputPath] = p
	}
	selected := make([]cache.Part, len(input.Paths))
	var rows, bytes int64
	for i, path := range input.Paths {
		if e := ctx.Err(); e != nil {
			return m, e
		}
		absolute, e := filepath.Abs(path)
		if e != nil {
			return m, e
		}
		p, ok := bySource[absolute]
		if !ok {
			return m, fmt.Errorf("cache missing raw source %s", path)
		}
		stat, e := os.Stat(path)
		if e != nil {
			return m, e
		}
		if stat.Size() != p.InputSize || stat.ModTime().UnixNano() != p.InputModTimeNS {
			return m, fmt.Errorf("raw input changed since projection: %s", path)
		}
		if e = (checkpoint.Artifact{Path: p.Path, Bytes: p.Bytes, SHA256: p.SHA256}).Verify(); e != nil {
			return m, e
		}
		selected[i] = p
		rows += p.Rows
		bytes += p.Bytes
		if i%100 == 0 {
			progress("cache_verified", map[string]any{"files": i + 1, "total": len(input.Paths)})
		}
	}
	m.Parts = selected
	m.Rows = rows
	m.Bytes = bytes
	return m, nil
}
func reconcileVocabulary(dictionary results.Dictionary, v store.Vocabulary) (results.Dictionary, error) {
	var rows []aggregate.Row
	appendID := func(country, scheme, level, id string) {
		country = strings.TrimSpace(country)
		id = strings.TrimSpace(id)
		if country == "" {
			country = aggregate.Unknown
		}
		if id == "" {
			return
		}
		rows = append(rows, aggregate.Row{Country: country, Scheme: scheme, Level: level, DisciplineID: id})
	}
	for _, c := range v.Countries {
		appendID(c, "topics", "field", aggregate.Unknown)
	}
	for _, f := range v.Fields {
		appendID(aggregate.Unknown, "topics", "field", f)
	}
	for _, s := range v.Subfields {
		appendID(aggregate.Unknown, "topics", "subfield", s)
	}
	for l, ids := range v.Concepts {
		for _, id := range ids {
			appendID(aggregate.Unknown, "concepts", fmt.Sprint(l), id)
		}
	}
	return dictionary.Reconcile(rows)
}
func reduceDense(ctx context.Context, columns *store.Columns, counts *Counts, dictionary results.Dictionary, scenarios []results.Scenario, cfg pipeline.Config, progress Progress) ([]results.Input, error) {
	bytes, e := results.EstimateDenseBytes(dictionary, scenarios, cfg.YearStart, cfg.YearEnd)
	if e != nil {
		return nil, e
	}
	workers := min(cfg.Workers, max(1, columns.Len()))
	if bytes > 0 {
		workers = min(workers, max(1, int((cfg.MaxMemoryBytes*20/100)/bytes)))
	}
	if bytes*uint64(workers) > cfg.MaxMemoryBytes*25/100 {
		return nil, fmt.Errorf("dense accumulators exceed memory budget")
	}
	progress("aggregate_workers", map[string]any{"workers": workers, "bytes_per_worker": bytes})
	parts := make([]*results.Dense, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for p := 0; p < workers; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			dense, e := results.NewDense(dictionary, scenarios, cfg.YearStart, cfg.YearEnd)
			if e != nil {
				errs[p] = e
				return
			}
			parts[p] = dense
			var scratch model.Work
			for i := columns.Len() * p / workers; i < columns.Len()*(p+1)/workers; i++ {
				if i%16384 == 0 {
					if e = ctx.Err(); e != nil {
						errs[p] = e
						return
					}
				}
				columns.View(i, &scratch)
				if e = dense.AddSeries(scratch, counts.YearCounts(i)); e != nil {
					errs[p] = e
					return
				}
			}
		}(p)
	}
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return nil, e
		}
	}
	for p := 1; p < len(parts); p++ {
		if e = parts[0].Merge(parts[p]); e != nil {
			return nil, e
		}
		parts[p] = nil
	}
	return parts[0].Inputs(), nil
}
func validateInputs(inputs []results.Input, edges uint64) error {
	var publications *uint64
	for _, input := range inputs {
		q := input.Quality
		if q.CitationEvents != edges {
			return fmt.Errorf("scenario %s raw citations %d != graph %d", input.Scenario.ID, q.CitationEvents, edges)
		}
		if publications == nil {
			v := q.PublicationWorks
			publications = &v
		} else if *publications != q.PublicationWorks {
			return fmt.Errorf("scenario raw publication count changed")
		}
		if input.Scenario.CountryMode == "fractional" {
			sums := map[string][2]float64{}
			for _, r := range input.Rows {
				k := r.Scheme + "/" + r.Level
				v := sums[k]
				v[0] += r.Publications
				v[1] += r.Citations
				sums[k] = v
			}
			for _, k := range []string{"topics/field", "topics/subfield", "concepts/0", "concepts/1"} {
				v := sums[k]
				qualityKey := strings.Replace(k, "/", ".", 1)
				pubAssignments := q.PublicationAssignments[qualityKey]
				citAssignments := q.CitationAssignments[qualityKey]
				if pubAssignments < q.PublicationWorks || citAssignments < edges {
					return fmt.Errorf("label assignments cannot drop raw events for %s/%s", input.Scenario.ID, k)
				}
				wantPub, wantCit := q.PublicationWorks, edges
				if strings.HasPrefix(k, "concepts/") && input.Scenario.ConceptMode == "whole" {
					wantPub, wantCit = pubAssignments, citAssignments
				}
				if !near(v[0], float64(wantPub)) || !near(v[1], float64(wantCit)) {
					return fmt.Errorf("country-fractional assignment conservation failed for %s/%s", input.Scenario.ID, k)
				}
			}
		}
		if input.Scenario.CountryMode == "whole" {
			for _, r := range input.Rows {
				if r.Scheme == "topics" || input.Scenario.ConceptMode == "whole" {
					if math.IsNaN(r.Publications) || math.IsInf(r.Publications, 0) || math.IsNaN(r.Citations) || math.IsInf(r.Citations, 0) || r.Publications < 0 || r.Citations < 0 || math.Trunc(r.Publications) != r.Publications || math.Trunc(r.Citations) != r.Citations {
						return fmt.Errorf("whole count must be nonnegative finite integer for %s/%s/%s/%d", input.Scenario.ID, r.Scheme, r.DisciplineID, r.Year)
					}
				}
			}
		}
	}
	return nil
}
func near(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= 1e-6+1e-10*max(a, b)
}
func writeMainCube(path string, rows []aggregate.Row) error {
	_, e := checkpoint.Write(path, func(w io.Writer) error {
		writer := parquet.NewGenericWriter[aggregate.Row](w)
		for start := 0; start < len(rows); start += 4096 {
			if _, e := writer.Write(rows[start:min(start+4096, len(rows))]); e != nil {
				_ = writer.Close()
				return e
			}
		}
		return writer.Close()
	})
	return e
}

var outputFiles = []string{"statistics.parquet", "cube.parquet", "country_eci.parquet", "discipline_pci.parquet", "rca.parquet", "coverage.json", "scenarios.json", "dictionary.json", "eci.json", "model_diagnostics.json", "methodology.json"}

func publishCompletion(directory string, report Report) error {
	completion := outputCompletion{Version: 1, RunID: report.RunID}
	var e error
	completion.Run, e = checkpoint.Hash(filepath.Join(directory, "run.json"))
	if e != nil {
		return e
	}
	for _, name := range outputFiles {
		artifact, e := checkpoint.Hash(filepath.Join(directory, name))
		if e != nil {
			return e
		}
		completion.Artifacts = append(completion.Artifacts, artifact)
	}
	return checkpoint.JSON(filepath.Join(directory, "completion.json"), completion)
}
func verifiedCompletion(directory, runID string) (Report, error) {
	var out Report
	var complete outputCompletion
	if e := checkpoint.ReadJSON(filepath.Join(directory, "completion.json"), &complete); e != nil {
		return out, e
	}
	if complete.Version != 1 || complete.RunID != runID || len(complete.Artifacts) != len(outputFiles) {
		return out, fmt.Errorf("completion identity mismatch")
	}
	if complete.Run.Path != filepath.Join(directory, "run.json") {
		return out, fmt.Errorf("completion run path mismatch")
	}
	if e := complete.Run.Verify(); e != nil {
		return out, e
	}
	for i, artifact := range complete.Artifacts {
		if artifact.Path != filepath.Join(directory, outputFiles[i]) {
			return out, fmt.Errorf("completion artifact path mismatch")
		}
		if e := artifact.Verify(); e != nil {
			return out, e
		}
	}
	if e := checkpoint.ReadJSON(filepath.Join(directory, "run.json"), &out); e != nil {
		return out, e
	}
	if out.RunID != runID || (out.Status != "full_snapshot_complete" && out.Status != "pilot_complete") {
		return out, fmt.Errorf("run completion status invalid")
	}
	return out, nil
}
