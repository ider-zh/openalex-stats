package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"openalexstats/internal/audit"
	"openalexstats/internal/checkpoint"
)

func auditCommand(args []string) error {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	data := fs.String("data", "", "completed immutable production results")
	cache := fs.String("cache", "", "complete normalized source cache")
	work := fs.String("work", "", "separate audit evidence and DuckDB temporary directory")
	duckdb := fs.String("duckdb", "duckdb", "DuckDB CLI executable")
	threads := fs.Int("threads", 46, "DuckDB worker budget")
	memory := fs.String("memory-limit", "512GB", "DuckDB memory limit")
	modelsOnly := fs.Bool("models-only", false, "independent model/export/sign checks without rerunning the full reference SQL join")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *modelsOnly {
		if *data == "" || *work == "" {
			return fmt.Errorf("model audit requires --data and --work")
		}
		dataDir, err := filepath.Abs(*data)
		if err != nil {
			return err
		}
		workDir, err := filepath.Abs(*work)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dataDir, workDir)
		if err != nil || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)) {
			return fmt.Errorf("model audit work directory must be outside immutable results")
		}
		unlock, err := checkpoint.LockDirectory(workDir)
		if err != nil {
			return err
		}
		defer unlock()
		report, err := audit.AuditModels(ctx, dataDir, filepath.Join(workDir, "model-audit.json"))
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(report)
	}
	progress := func(stage string, detail any) {
		_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"time": time.Now().UTC(), "stage": stage, "detail": detail})
	}
	report, err := audit.Run(ctx, audit.Config{ResultDir: *data, CacheDir: *cache, WorkDir: *work, DuckDB: *duckdb, Threads: *threads, MemoryLimit: *memory}, progress)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(report)
}
