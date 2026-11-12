package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"openalexstats/internal/aggregate"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/pipeline"
	"openalexstats/internal/policy"
	"openalexstats/internal/production"
	"openalexstats/internal/results"
)

func computeCommand(args []string) error {
	fs := flag.NewFlagSet("compute", flag.ContinueOnError)
	manifest := fs.String("manifest", "", "raw source manifest")
	input := fs.String("input", "", "explicit comma-separated source files for a partial verification run")
	files := fs.Int("files", 0, "source file limit (marks partial)")
	dictionary := fs.String("dictionary-manifest", "", "taxonomy manifest (defaults to --manifest)")
	cacheDir := fs.String("cache", "", "complete normalized cache")
	workDir := fs.String("work", "", "dedicated resumable working directory")
	counterDir := fs.String("counters", "", "counter checkpoint root, optionally a separate NVMe")
	output := fs.String("output", "", "immutable statistical result directory")
	workers := fs.Int("workers", min(runtime.NumCPU(), 46), "global Go CPU and dense reduction workers")
	readers := fs.Int("readers", 0, "dimension/input verification workers (0=min(workers,24))")
	edgeWorkers := fs.Int("edge-workers", 0, "independent normalized source files (0=workers)")
	countWorkers := fs.Int("count-workers", 16, "parallel target owners")
	indexWorkers := fs.Int("index-workers", 0, "index construction workers (0=min(workers,32))")
	indexShards := fs.Int("index-shards", 256, "exact index map shards")
	serialIndex := fs.Bool("serial-index", false, "use serial baseline index for controlled comparison")
	edgeCache := fs.String("edge-cache", "", "dedicated reference checkpoint directory (default work/edges)")
	partitions := fs.Int("partitions", 64, "target partitions [1,256]")
	yearStart := fs.Int("year-start", 2000, "first output year")
	yearEnd := fs.Int("year-end", 2026, "last output year; 2026 partial")
	corpus := fs.String("corpus", "core", "core or all")
	corpusStart := fs.Int("corpus-year-start", 0, "0 preserves older cited targets")
	policyPath := fs.String("policy", "", "optional blacklist specification")
	scenariosPath := fs.String("scenarios", "", "optional JSON scenario array; default country whole/fractional and Concepts thresholds 0/.3/.5")
	includeLoops := fs.Bool("include-work-self-loops", false, "retain invalid same-work reference loops for a sensitivity run")
	memory := fs.Uint64("memory-gib", 640, "application planning/RSS budget")
	floor := fs.Uint64("host-available-floor-gib", 64, "cancel safely if host available RAM stays below this floor")
	partBuffer := fs.Uint64("edge-part-buffer-gib", 8, "maximum two edge numeric buffers per source task")
	buildID := fs.String("build-id", "", "source/build identity; defaults to executable SHA256")
	profile := fs.String("pprof", "", "optional loopback pprof address, e.g.127.0.0.1:8898")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if fs.NArg() != 0 || *memory < 1 || *memory > 1<<20 || *partBuffer < 1 || *partBuffer > 1024 || *floor > 1<<20 {
		return fmt.Errorf("invalid compute options")
	}
	if *readers == 0 {
		*readers = min(*workers, 24)
	}
	var inputs []string
	if *input != "" {
		inputs = strings.Split(*input, ",")
	}
	resolved, e := pipeline.Resolve(*manifest, inputs, *files)
	if e != nil {
		return e
	}
	if *dictionary == "" {
		*dictionary = *manifest
	}
	if *dictionary == "" {
		return fmt.Errorf("explicit inputs require --dictionary-manifest")
	}
	blacklist, e := policy.LoadBlacklist(*policyPath)
	if e != nil {
		return e
	}
	scope, e := policy.New(*corpus, *corpusStart, blacklist)
	if e != nil {
		return e
	}
	var scenarios []results.Scenario
	if *scenariosPath != "" {
		content, e := os.ReadFile(*scenariosPath)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(content, &scenarios); e != nil {
			return fmt.Errorf("invalid scenarios: %w", e)
		}
	}
	if *buildID == "" {
		executable, e := os.Executable()
		if e != nil {
			return e
		}
		artifact, e := checkpoint.Hash(executable)
		if e != nil {
			return e
		}
		*buildID = artifact.SHA256
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *profile != "" {
		host, _, e := net.SplitHostPort(*profile)
		if e != nil {
			return e
		}
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			return fmt.Errorf("pprof must bind loopback")
		}
		listener, e := net.Listen("tcp", *profile)
		if e != nil {
			return e
		}
		server := &http.Server{Handler: http.DefaultServeMux, ReadHeaderTimeout: 5 * time.Second}
		defer server.Close()
		go func() { _ = server.Serve(listener) }()
	}
	base := pipeline.Config{Input: resolved, Scope: scope, Weighting: aggregate.Config{CountryMode: "fractional", ConceptMode: "whole", ConceptThreshold: .3}, YearStart: *yearStart, YearEnd: *yearEnd, Workers: *workers, Readers: *readers, MaxRows: 0, MaxMemoryBytes: *memory << 30, OutputDir: *output, IncludeWorkSelfLoops: *includeLoops}
	cfg := production.Config{Base: base, CacheDirectory: *cacheDir, WorkDirectory: *workDir, CounterDirectory: *counterDir, DictionaryManifest: *dictionary, BuildID: *buildID, EdgeWorkers: *edgeWorkers, CountWorkers: *countWorkers, Partitions: *partitions, MaxPartBufferBytes: *partBuffer << 30, HostAvailableFloorBytes: *floor << 30}
	cfg.Scenarios = scenarios
	cfg.IndexWorkers, cfg.IndexShards, cfg.SerialIndex = *indexWorkers, *indexShards, *serialIndex
	cfg.EdgeCacheDirectory = *edgeCache
	report, e := production.Run(ctx, cfg, func(stage string, detail any) {
		_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"time": time.Now().UTC(), "stage": stage, "detail": detail})
	})
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}
