package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"openalexstats/internal/cache"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/pipeline"
)

func prepareCommand(args []string) error {
	fs := flag.NewFlagSet("prepare", flag.ContinueOnError)
	manifest := fs.String("manifest", "", "source snapshot manifest")
	input := fs.String("input", "", "explicit comma-separated raw Parquet paths")
	files := fs.Int("files", 0, "limit input files; 0 full manifest")
	dir := fs.String("cache", "", "dedicated normalized cache directory")
	workers := fs.Int("workers", 24, "parallel independent source files")
	cpus := fs.Int("cpus", min(runtime.NumCPU(), 46), "global Go CPU budget")
	memoryGiB := fs.Uint64("memory-gib", 640, "total application planning budget")
	rowGroup := fs.Int64("row-group-rows", 100000, "normalized Parquet rows per row group")
	build := fs.String("cache-build-id", "normalized-projection-go-v1", "projection semantics/build identity")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if fs.NArg() != 0 || *dir == "" || *workers < 1 || *cpus < 1 || *workers > 1024 || *memoryGiB < 1 || *memoryGiB > 1<<20 {
		return fmt.Errorf("invalid prepare options")
	}
	var inputs []string
	if *input != "" {
		inputs = strings.Split(*input, ",")
	}
	resolved, e := pipeline.Resolve(*manifest, inputs, *files)
	if e != nil {
		return e
	}
	old := runtime.GOMAXPROCS(*cpus)
	defer runtime.GOMAXPROCS(old)
	oldLimit := debug.SetMemoryLimit(int64((*memoryGiB << 30) * 85 / 100))
	defer debug.SetMemoryLimit(oldLimit)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	stop := pipeline.StartResourceSampler()
	started := time.Now()
	expected := int64(0)
	if resolved.ManifestSHA256 != "" && !resolved.Truncated {
		expected = resolved.ManifestWorks
	}
	result, e := cache.Prepare(ctx, resolved.Paths, cache.Options{Directory: *dir, Workers: *workers, BuildID: *build, ManifestSHA256: resolved.ManifestSHA256, ExpectedRows: expected, RowGroupRows: *rowGroup}, func(p cache.Progress) {
		_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"time": time.Now().UTC(), "stage": "prepare", "detail": p})
	})
	resources := stop()
	report := map[string]any{"status": "complete", "elapsed_seconds": time.Since(started).Seconds(), "resources": resources, "workers": *workers, "cpus": *cpus, "memory_gib": *memoryGiB, "manifest": result}
	if e != nil {
		report["status"] = "failed"
		report["error"] = e.Error()
	}
	if *dir != "" {
		if err := checkpoint.JSON(*dir+"/prepare-run.json", report); err != nil && e == nil {
			return err
		}
	}
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}
