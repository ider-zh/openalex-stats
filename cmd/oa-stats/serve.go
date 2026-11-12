package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"openalexstats/internal/web"
)

func serveCommand(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	data := fs.String("data", "", "completed result directory")
	core := fs.String("data-core", "", "completed core directory for a corpus catalog")
	all := fs.String("data-all", "", "completed core+XPAC directory, requires --data-core")
	methodsCore := fs.String("methods-core", "", "optional immutable core complexity-method sidecar")
	methodsAll := fs.String("methods-all", "", "optional immutable all-corpus complexity-method sidecar")
	address := fs.String("listen", "127.0.0.1:8899", "bind address; use SSH forwarding or explicitly bind LAN")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if fs.NArg() != 0 || (*data != "" && (*core != "" || *all != "")) || (*data == "" && *core == "") {
		return fmt.Errorf("serve requires --data, or --data-core [--data-all]; these forms are mutually exclusive")
	}
	if (*data != "" && (*methodsCore != "" || *methodsAll != "")) || (*all == "" && *methodsAll != "") {
		return fmt.Errorf("method sidecars require matching --data-core/--data-all catalog entries")
	}
	var handler http.Handler
	var e error
	if *data != "" {
		handler, e = web.New(*data)
	} else {
		dirs := map[string]string{"core": *core}
		if *all != "" {
			dirs["all"] = *all
		}
		methodDirs := map[string]string{}
		if *methodsCore != "" {
			methodDirs["core"] = *methodsCore
		}
		if *methodsAll != "" {
			methodDirs["all"] = *methodsAll
		}
		handler, e = web.NewCatalogWithMethods(dirs, methodDirs)
	}
	if e != nil {
		return e
	}
	listener, e := net.Listen("tcp", *address)
	if e != nil {
		return e
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	fmt.Fprintln(os.Stderr, "OpenAlex statistics listening at", listener.Addr())
	select {
	case e = <-done:
		if errors.Is(e, http.ErrServerClosed) {
			return nil
		}
		return e
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		e = server.Shutdown(shutdown)
		serveErr := <-done
		if e != nil {
			return e
		}
		if !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return nil
	}
}
