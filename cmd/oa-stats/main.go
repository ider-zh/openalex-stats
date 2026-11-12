package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"openalexstats/internal/aggregate"
	"openalexstats/internal/pipeline"
	"openalexstats/internal/policy"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "oa-stats:", err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: oa-stats inspect|run|prepare|compute|audit|serve|export-site|derive-method [flags]")
	}
	command := os.Args[1]
	if command == "derive-method" {
		return deriveMethodCommand(os.Args[2:])
	}
	if command == "export-site" {
		return exportSiteCommand(os.Args[2:])
	}
	if command == "audit" {
		return auditCommand(os.Args[2:])
	}
	if command == "prepare" {
		return prepareCommand(os.Args[2:])
	}
	if command == "serve" {
		return serveCommand(os.Args[2:])
	}
	if command == "compute" {
		return computeCommand(os.Args[2:])
	}
	if command != "inspect" && command != "run" {
		return fmt.Errorf("unknown command %q", command)
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	manifest := fs.String("manifest", "", "snapshot manifest.json")
	input := fs.String("input", "", "comma-separated explicit Parquet files (instead of manifest)")
	files := fs.Int("files", 0, "maximum input files; 0 uses all paths")
	maxRows := fs.Int64("max-rows", 100000, "raw rows per pass; 0 is unrestricted full input")
	workers := fs.Int("workers", min(runtime.NumCPU(), 46), "global Go CPU concurrency and graph workers")
	readers := fs.Int("readers", 2, "concurrent Parquet file readers")
	yearStart := fs.Int("year-start", 2000, "first output year")
	yearEnd := fs.Int("year-end", 2026, "last output year (2026 snapshot is partial)")
	corpus := fs.String("corpus", "core", "core or all; provisional unless explicitly agreed")
	corpusStart := fs.Int("corpus-year-start", 0, "0 retains pre-2000 targets; 2000 restricts corpus publication years")
	countryMode := fs.String("country-mode", "fractional", "fractional or whole")
	conceptMode := fs.String("concept-mode", "whole", "whole counts each matching concept once; fractional is historical allocation")
	threshold := fs.Float64("concept-threshold", 0.3, "minimum concept score [0,1]")
	includeLoops := fs.Bool("include-work-self-loops", false, "include same-work self references (default excludes; does not filter author self-citations)")
	policyFile := fs.String("policy", "", "optional document-scope-proposal.json blacklist")
	memoryGiB := fs.Uint64("memory-gib", 4, "application planning budget GiB; Go soft limit is 85%")
	output := fs.String("output", "", "new immutable output directory (required for run)")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	var inputs []string
	if *input != "" {
		inputs = strings.Split(*input, ",")
	}
	resolved, err := pipeline.Resolve(*manifest, inputs, *files)
	if err != nil {
		return err
	}
	if command == "inspect" {
		return json.NewEncoder(os.Stdout).Encode(resolved)
	}
	if *memoryGiB == 0 || *memoryGiB > 1<<20 {
		return fmt.Errorf("invalid memory-gib")
	}
	blacklist, err := policy.LoadBlacklist(*policyFile)
	if err != nil {
		return err
	}
	scope, err := policy.New(*corpus, *corpusStart, blacklist)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	cfg := pipeline.Config{Input: resolved, Scope: scope, Weighting: aggregate.Config{CountryMode: *countryMode, ConceptMode: *conceptMode, ConceptThreshold: *threshold}, YearStart: *yearStart, YearEnd: *yearEnd, Workers: *workers, Readers: *readers, MaxRows: *maxRows, MaxMemoryBytes: *memoryGiB << 30, OutputDir: *output}
	cfg.IncludeWorkSelfLoops = *includeLoops
	progress := func(stage string, detail any) {
		_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"time": time.Now().UTC(), "stage": stage, "detail": detail})
	}
	report, err := pipeline.Run(ctx, cfg, progress)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}
