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
	"path/filepath"
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
	coreEnriched := fs.String("data-core-enriched", "", "completed country-enriched core; enables original/enriched selector")
	allEnriched := fs.String("data-all-enriched", "", "matching country-enriched core+XPAC")
	methodsCoreEnriched := fs.String("methods-core-enriched", "", "country-enriched core complexity-method sidecar")
	methodsAllEnriched := fs.String("methods-all-enriched", "", "country-enriched all complexity-method sidecar")
	profilesRoot := fs.String("complexity-profiles-dir", "", "precomputed original-core/original-all/enriched-core/enriched-all profile bundle root")
	address := fs.String("listen", "127.0.0.1:8899", "bind address; use SSH forwarding or explicitly bind LAN")
	waitForListen := fs.Duration("wait-for-listen", 0, "after loading results, wait up to this duration for an occupied listener during warm handoff (max 15m)")
	if e := fs.Parse(args); e != nil {
		return e
	}
	if fs.NArg() != 0 || (*data != "" && (*core != "" || *all != "")) || (*data == "" && *core == "") {
		return fmt.Errorf("serve requires --data, or --data-core [--data-all]; these forms are mutually exclusive")
	}
	if *waitForListen < 0 || *waitForListen > 15*time.Minute {
		return fmt.Errorf("wait-for-listen must be between 0 and 15m")
	}
	if (*data != "" && (*methodsCore != "" || *methodsAll != "")) || (*all == "" && *methodsAll != "") {
		return fmt.Errorf("method sidecars require matching --data-core/--data-all catalog entries")
	}
	hasEnriched := *coreEnriched != "" || *allEnriched != "" || *methodsCoreEnriched != "" || *methodsAllEnriched != ""
	if *profilesRoot != "" && !hasEnriched {
		return fmt.Errorf("complexity profiles require the matching country-version catalog")
	}
	if hasEnriched && (*data != "" || *coreEnriched == "" || ((*all == "") != (*allEnriched == ""))) {
		return fmt.Errorf("country-enriched versions require --data-core-enriched and the same core/all corpora as the original catalog")
	}
	if *allEnriched == "" && *methodsAllEnriched != "" {
		return fmt.Errorf("enriched method sidecars require the matching enriched corpus")
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
		if hasEnriched {
			enrichedDirs := map[string]string{"core": *coreEnriched}
			if *allEnriched != "" {
				enrichedDirs["all"] = *allEnriched
			}
			enrichedMethods := map[string]string{}
			if *methodsCoreEnriched != "" {
				enrichedMethods["core"] = *methodsCoreEnriched
			}
			if *methodsAllEnriched != "" {
				enrichedMethods["all"] = *methodsAllEnriched
			}
			originalProfiles, enrichedProfiles := map[string]string{}, map[string]string{}
			if *profilesRoot != "" {
				for corpus := range dirs {
					originalProfiles[corpus] = filepath.Join(*profilesRoot, "original-"+corpus)
					enrichedProfiles[corpus] = filepath.Join(*profilesRoot, "enriched-"+corpus)
				}
			}
			handler, e = web.NewCountryVersionCatalog(web.CountryVersionConfig{Datasets: dirs, MethodDirectories: methodDirs, ProfileDirectories: originalProfiles}, web.CountryVersionConfig{Datasets: enrichedDirs, MethodDirectories: enrichedMethods, ProfileDirectories: enrichedProfiles})
		} else {
			handler, e = web.NewCatalogWithMethods(dirs, methodDirs)
		}
	}
	if e != nil {
		return e
	}
	listener, e := listenForHandoff(*address, *waitForListen, func() { fmt.Fprintln(os.Stderr, "OpenAlex results loaded; waiting for previous listener at", *address) })
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

func listenForHandoff(address string, wait time.Duration, ready func()) (net.Listener, error) {
	deadline := time.Now().Add(wait)
	reported := false
	for {
		listener, err := net.Listen("tcp", address)
		if err == nil {
			return listener, nil
		}
		// Winsock's WSAEADDRINUSE (10048) is distinct from Go's portable
		// syscall.EADDRINUSE value on Windows.
		inUse := errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, syscall.Errno(10048))
		if wait <= 0 || !inUse || !time.Now().Before(deadline) {
			return nil, err
		}
		if !reported {
			ready()
			reported = true
		}
		time.Sleep(200 * time.Millisecond)
	}
}
