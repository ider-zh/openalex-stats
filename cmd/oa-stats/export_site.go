package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"openalexstats/internal/web"
)

func exportSiteCommand(args []string) error {
	fs := flag.NewFlagSet("export-site", flag.ContinueOnError)
	core := fs.String("data-core", "", "completed immutable core result")
	all := fs.String("data-all", "", "optional completed immutable core+XPAC result")
	methodsCore := fs.String("methods-core", "", "optional immutable core method sidecar")
	methodsAll := fs.String("methods-all", "", "optional immutable core+XPAC method sidecar")
	out := fs.String("output", "", "new immutable static query export directory")
	workers := fs.Int("workers", min(runtime.NumCPU(), 8), "parallel static shard compression workers")
	release := fs.String("release-base", "https://github.com/ider-zh/openalex-stats/releases/download/snapshot-20260626-v1", "HTTPS release asset base URL")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	r, e := web.ExportStatic(ctx, web.StaticExportConfig{CoreDirectory: *core, AllDirectory: *all, MethodsCoreDirectory: *methodsCore, MethodsAllDirectory: *methodsAll, OutputDirectory: *out, Workers: *workers, ReleaseBaseURL: *release}, func(stage string, detail any) {
		_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"time": time.Now().UTC(), "stage": stage, "detail": detail})
	})
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(r)
}
