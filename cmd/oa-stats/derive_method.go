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

	"openalexstats/internal/checkpoint"
	"openalexstats/internal/methods"
)

func deriveMethodCommand(args []string) error {
	fs := flag.NewFlagSet("derive-method", flag.ContinueOnError)
	data := fs.String("data", "", "completed immutable core or all result directory")
	output := fs.String("output", "", "new, separate immutable method sidecar directory")
	workers := fs.Int("workers", min(8, runtime.NumCPU()), "parallel model workers")
	build := fs.String("build-id", "", "defaults to executable SHA256")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected derive-method arguments")
	}
	if *build == "" {
		exe, e := os.Executable()
		if e != nil {
			return e
		}
		a, e := checkpoint.Hash(exe)
		if e != nil {
			return e
		}
		*build = a.SHA256
	}
	previous := runtime.GOMAXPROCS(*workers)
	defer runtime.GOMAXPROCS(previous)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	m, e := methods.Derive(ctx, methods.Config{Data: *data, Output: *output, BuildID: *build, Workers: *workers})
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(m)
}
