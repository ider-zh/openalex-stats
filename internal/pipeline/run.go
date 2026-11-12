// Package pipeline composes a two-pass, memory-budgeted Go statistics runner.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/aggregate"
	"openalexstats/internal/graph"
	"openalexstats/internal/model"
	"openalexstats/internal/policy"
	"openalexstats/internal/scan"
	"openalexstats/internal/store"
)

type Config struct {
	Input                Input            `json:"input"`
	Scope                policy.Scope     `json:"scope"`
	Weighting            aggregate.Config `json:"weighting"`
	YearStart            int              `json:"year_start"`
	YearEnd              int              `json:"year_end"`
	Workers              int              `json:"workers"`
	Readers              int              `json:"readers"`
	MaxRows              int64            `json:"max_rows"`
	MaxMemoryBytes       uint64           `json:"max_memory_bytes"`
	OutputDir            string           `json:"output_dir"`
	IncludeWorkSelfLoops bool             `json:"include_work_self_loops"`
}
type Report struct {
	Version             string             `json:"version"`
	Status              string             `json:"status"`
	Config              Config             `json:"config"`
	StartedAt           time.Time          `json:"started_at"`
	CompletedAt         time.Time          `json:"completed_at"`
	StageSeconds        map[string]float64 `json:"stage_seconds"`
	DimensionScan       scan.Stats         `json:"dimension_scan"`
	ReferenceScan       scan.Stats         `json:"reference_scan"`
	SelectedWorks       int                `json:"selected_works"`
	DimensionBytes      uint64             `json:"dimension_allocated_bytes"`
	IndexEstimatedBytes uint64             `json:"index_estimated_bytes"`
	CounterBytes        uint64             `json:"counter_bytes"`
	Graph               graph.Stats        `json:"graph"`
	Quality             aggregate.Quality  `json:"quality"`
	CubeRows            int                `json:"cube_rows"`
	Models              int                `json:"models"`
	Resources           ResourceSummary    `json:"resources"`
	InputMetadataSHA256 string             `json:"input_metadata_sha256"`
	Notes               []string           `json:"notes"`
}
type Progress func(stage string, detail any)

func Run(ctx context.Context, cfg Config, progress Progress) (report Report, err error) {
	scope, scopeErr := policy.New(cfg.Scope.Corpus, cfg.Scope.CorpusYearStart, cfg.Scope.Blacklist)
	if scopeErr != nil {
		return report, scopeErr
	}
	cfg.Scope = scope
	if cfg.Workers < 1 || cfg.Workers > 1024 || cfg.Readers < 1 || cfg.Readers > cfg.Workers {
		return report, fmt.Errorf("invalid worker/reader settings")
	}
	if cfg.YearStart < 1 || cfg.YearEnd < cfg.YearStart || cfg.YearEnd-cfg.YearStart > 200 {
		return report, fmt.Errorf("invalid year range")
	}
	if cfg.MaxRows < 0 || cfg.MaxMemoryBytes < 64<<20 || cfg.MaxMemoryBytes > 1<<50 {
		return report, fmt.Errorf("invalid row or memory limit")
	}
	if cfg.OutputDir == "" {
		return report, fmt.Errorf("output directory required")
	}
	if _, err = aggregate.New(cfg.Weighting); err != nil {
		return report, err
	}
	// An output directory is immutable for one attempt; reruns use a new directory.
	// This prevents a failed rerun from presenting a previous success report.
	if err = os.MkdirAll(filepath.Dir(cfg.OutputDir), 0755); err != nil {
		return report, err
	}
	if err = os.Mkdir(cfg.OutputDir, 0755); err != nil {
		return report, fmt.Errorf("output directory must not already exist: %w", err)
	}
	oldProcs := runtime.GOMAXPROCS(cfg.Workers)
	defer runtime.GOMAXPROCS(oldProcs)
	oldLimit := debug.SetMemoryLimit(int64(cfg.MaxMemoryBytes * 85 / 100))
	defer debug.SetMemoryLimit(oldLimit)
	report = Report{Version: "go-pipeline-v1", Status: "running", Config: cfg, StartedAt: time.Now().UTC(), StageSeconds: map[string]float64{}, Notes: []string{
		"Scope, country weights and concept threshold are explicit run parameters; this prototype does not declare unresolved statistical choices final.",
		"CPU/RAM throughput targets require calibration. This runner does not yet implement NVMe projection checkpoints or dynamic worker tuning.",
		"Only edges with both endpoints in the selected input corpus count. Partial-input pilot results are not world statistics.",
	}}
	stopResources := startResourceSampler()
	defer func() {
		report.Resources = stopResources()
		report.CompletedAt = time.Now().UTC()
		if err != nil {
			report.Status = "failed"
			report.Notes = append(report.Notes, err.Error())
		}
		if e := writeJSON(filepath.Join(cfg.OutputDir, "run.json"), report); e != nil && err == nil {
			err = e
		}
	}()
	if progress == nil {
		progress = func(string, any) {}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stamps, stampErr := fileStamps(cfg.Input.Paths)
	if stampErr != nil {
		return report, stampErr
	}
	dimensionDigest := newPassDigest()
	referenceDigest := newPassDigest()
	columns := store.New()
	started := time.Now()
	progress("dimensions", map[string]any{"files": len(cfg.Input.Paths)})
	report.DimensionScan, err = scan.Parquet(ctx, cfg.Input.Paths, scan.Options{ReadDimensions: true, MaxRows: cfg.MaxRows, ReaderWorkers: cfg.Readers}, func(w model.Work) error {
		selected := cfg.Scope.Includes(w)
		dimensionDigest.add(w, selected)
		if !selected {
			return nil
		}
		if e := columns.Append(w); e != nil {
			return e
		}
		if columns.Len()%4096 == 0 {
			if e := checkBudget(columns, cfg); e != nil {
				return e
			}
			progress("dimensions", map[string]any{"selected_works": columns.Len(), "dimension_bytes": columns.EstimatedBytes()})
		}
		return nil
	})
	if err != nil {
		return report, err
	}
	if err = checkBudget(columns, cfg); err != nil {
		return report, err
	}
	if err = verifyStamps(cfg.Input.Paths, stamps); err != nil {
		return report, err
	}
	report.StageSeconds["dimensions"] = time.Since(started).Seconds()
	report.SelectedWorks = columns.Len()
	report.DimensionBytes = columns.EstimatedBytes()
	if report.DimensionScan.NullXPACFlags > 0 {
		report.Notes = append(report.Notes, "Null XPAC flags were observed; scanner coalesces them false. Corpus assignment needs review.")
	}
	started = time.Now()
	progress("index", map[string]int{"selected_works": columns.Len()})
	index, e := graph.NewIndexContext(ctx, columns.IDs())
	if e != nil {
		return report, e
	}
	report.IndexEstimatedBytes = index.EstimatedBytes()
	report.StageSeconds["index"] = time.Since(started).Seconds()
	started = time.Now()
	progress("citations", map[string]int{"workers": cfg.Workers})
	sources := make(chan model.Work, cfg.Workers*2)
	type scanResult struct {
		stats scan.Stats
		err   error
	}
	scanDone := make(chan scanResult, 1)
	go func() {
		stats, e := scan.Parquet(ctx, cfg.Input.Paths, scan.Options{ReadReferences: true, MaxRows: cfg.MaxRows, ReaderWorkers: cfg.Readers}, func(w model.Work) error {
			selected := cfg.Scope.Includes(w)
			referenceDigest.add(w, selected)
			if !selected {
				return nil
			}
			w.References = append([]uint64(nil), w.References...)
			select {
			case sources <- w:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		close(sources)
		scanDone <- scanResult{stats, e}
	}()
	counts, e := graph.Count(ctx, index, sources, graph.Config{Workers: cfg.Workers, YearStart: cfg.YearStart, YearEnd: cfg.YearEnd, MaxCounterBytes: cfg.MaxMemoryBytes * 75 / 100, ExcludeSelfLoops: !cfg.IncludeWorkSelfLoops})
	if e != nil {
		cancel()
	}
	scanned := <-scanDone
	report.ReferenceScan = scanned.stats
	if e != nil {
		return report, e
	}
	if scanned.err != nil {
		return report, scanned.err
	}
	if report.ReferenceScan.Rows != report.DimensionScan.Rows {
		return report, fmt.Errorf("two-pass row count mismatch")
	}
	if dimensionDigest.sum() != referenceDigest.sum() {
		return report, fmt.Errorf("two-pass input metadata/corpus digest mismatch")
	}
	report.InputMetadataSHA256 = dimensionDigest.sum()
	if err = verifyStamps(cfg.Input.Paths, stamps); err != nil {
		return report, err
	}
	report.Graph = counts.Stats()
	report.CounterBytes = uint64(columns.Len()) * uint64(cfg.YearEnd-cfg.YearStart+1) * 8
	report.StageSeconds["citations"] = time.Since(started).Seconds()
	started = time.Now()
	progress("aggregate", nil)
	combined, e := reduce(ctx, columns, counts, cfg)
	if e != nil {
		return report, e
	}
	rows := combined.Rows()
	report.CubeRows = len(rows)
	report.Quality = combined.Quality()
	report.StageSeconds["aggregate"] = time.Since(started).Seconds()
	started = time.Now()
	progress("eci", map[string]int{"cube_rows": len(rows)})
	models, e := computeModels(ctx, rows, cfg.Workers, cfg.YearStart, cfg.YearEnd)
	if e != nil {
		return report, e
	}
	report.Models = len(models)
	report.StageSeconds["eci"] = time.Since(started).Seconds()
	started = time.Now()
	if err = writeCube(filepath.Join(cfg.OutputDir, "cube.parquet"), rows); err != nil {
		return report, err
	}
	if err = writeJSON(filepath.Join(cfg.OutputDir, "eci.json"), models); err != nil {
		return report, err
	}
	report.StageSeconds["export"] = time.Since(started).Seconds()
	report.Status = "pilot_complete"
	if cfg.MaxRows == 0 && cfg.Input.ManifestSHA256 != "" && !cfg.Input.Truncated {
		if report.DimensionScan.Rows != cfg.Input.ManifestWorks {
			return report, fmt.Errorf("full-manifest row count mismatch")
		}
		report.Status = "full_snapshot_complete"
	}
	progress("complete", map[string]any{"status": report.Status, "output_dir": cfg.OutputDir})
	return report, nil
}

func checkBudget(c *store.Columns, cfg Config) error {
	indexBytes, e := graph.EstimateIndexBytes(c.Len())
	if e != nil {
		return e
	}
	cells := uint64(c.Len()) * uint64(cfg.YearEnd-cfg.YearStart+1) * 8
	// Reserve 30% for slices growing during append, decode queues, reduction maps,
	// GC and runtime. This is a guard, not a process RSS hard limit.
	if c.EstimatedBytes()+indexBytes+cells > cfg.MaxMemoryBytes*70/100 {
		return fmt.Errorf("planned dimensions/index/counters exceed 70%% memory budget at %d selected works", c.Len())
	}
	return nil
}
func reduce(ctx context.Context, c *store.Columns, counts *graph.Counts, cfg Config) (*aggregate.Aggregator, error) {
	workers := min(cfg.Workers, 16, max(1, c.Len()))
	parts := make([]*aggregate.Aggregator, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for p := 0; p < workers; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			a, e := aggregate.New(cfg.Weighting)
			if e != nil {
				errs[p] = e
				return
			}
			parts[p] = a
			var w model.Work
			for i := c.Len() * p / workers; i < c.Len()*(p+1)/workers; i++ {
				if i%4096 == 0 {
					if e = ctx.Err(); e != nil {
						errs[p] = e
						return
					}
				}
				c.View(i, &w)
				if w.Year >= cfg.YearStart && w.Year <= cfg.YearEnd {
					if e = a.AddPublication(w); e != nil {
						errs[p] = e
						return
					}
				}
				for y, count := range counts.YearCounts(i) {
					if count > 0 {
						if e = a.AddCitation(w, cfg.YearStart+y, count); e != nil {
							errs[p] = e
							return
						}
					}
				}
			}
		}(p)
	}
	wg.Wait()
	a, _ := aggregate.New(cfg.Weighting)
	for i := range parts {
		if errs[i] != nil {
			return nil, errs[i]
		}
		if e := a.Merge(parts[i]); e != nil {
			return nil, e
		}
	}
	return a, nil
}
func writeJSON(path string, v any) error {
	f, e := os.Create(path + ".tmp")
	if e != nil {
		return e
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	e = enc.Encode(v)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(path+".tmp", path)
}
func writeCube(path string, rows []aggregate.Row) error {
	f, e := os.Create(path + ".tmp")
	if e != nil {
		return e
	}
	w := parquet.NewGenericWriter[aggregate.Row](f)
	for start := 0; start < len(rows); start += 4096 {
		if _, e = w.Write(rows[start:min(start+4096, len(rows))]); e != nil {
			_ = w.Close()
			_ = f.Close()
			return e
		}
	}
	if e = w.Close(); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(path+".tmp", path)
}
