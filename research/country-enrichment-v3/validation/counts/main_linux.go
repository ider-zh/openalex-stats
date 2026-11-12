//go:build linux

// Attach original integer incoming-reference events, preserving early targets.
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"
)

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func ids(path string) ([]byte, uint64, uint64) {
	f, e := os.Open(path)
	must(e)
	st, e := f.Stat()
	must(e)
	d, e := syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	must(e)
	f.Close()
	if string(d[:8]) != "OACOLS01" {
		panic("magic")
	}
	p := 16 + binary.LittleEndian.Uint64(d[8:16])
	n := binary.LittleEndian.Uint64(d[p : p+8])
	return d, p + 8, n
}

type target struct {
	ID      uint64 `json:"id"`
	Ordinal uint64 `json:"ordinal"`
	XPAC    bool   `json:"is_xpac"`
	Year    int32  `json:"year"`
	Stratum string `json:"stratum"`
}
type result struct {
	ID           uint64   `json:"id"`
	Stratum      string   `json:"stratum"`
	Year         int32    `json:"publication_year"`
	CoreEligible bool     `json:"core_eligible"`
	AllOrdinal   uint64   `json:"all_ordinal"`
	CoreOrdinal  *uint64  `json:"core_ordinal"`
	Core         []uint64 `json:"core_incoming_reference_events_2000_2026"`
	All          []uint64 `json:"all_incoming_reference_events_2000_2026"`
}

func openCounts(dir string) []*os.File {
	paths, e := filepath.Glob(filepath.Join(dir, "target-*.counts"))
	must(e)
	sort.Strings(paths)
	if len(paths) != 64 {
		panic("count partitions not64")
	}
	fs := make([]*os.File, len(paths))
	for i, p := range paths {
		fs[i], e = os.Open(p)
		must(e)
	}
	return fs
}
func read(fs []*os.File, ord uint64) []uint64 {
	b := make([]byte, 27*8)
	n, e := fs[ord%64].ReadAt(b, int64(ord/64*27*8))
	must(e)
	if n != len(b) {
		panic("short counts")
	}
	r := make([]uint64, 27)
	for i := range r {
		r[i] = binary.LittleEndian.Uint64(b[i*8 : i*8+8])
	}
	return r
}
func main() {
	if len(os.Args) != 7 {
		panic("counts sample_json core_dimensions all_dimensions core_counts all_counts output")
	}
	start := time.Now()
	b, e := os.ReadFile(os.Args[1])
	must(e)
	var s struct {
		Works []target `json:"works"`
	}
	must(json.Unmarshal(b, &s))
	wanted := map[uint64]bool{}
	for _, t := range s.Works {
		if !t.XPAC {
			wanted[t.ID] = true
		}
	}
	d, p, n := ids(os.Args[2])
	defer syscall.Munmap(d)
	type pair struct{ id, ord uint64 }
	found := make(chan pair, len(wanted))
	var wg sync.WaitGroup
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			lo, hi := n*uint64(w)/16, n*uint64(w+1)/16
			for i := lo; i < hi; i++ {
				v := binary.LittleEndian.Uint64(d[p+i*8 : p+i*8+8])
				if wanted[v] {
					found <- pair{v, i}
				}
			}
		}(w)
	}
	wg.Wait()
	close(found)
	coreOrd := map[uint64]uint64{}
	for x := range found {
		if _, ok := coreOrd[x.id]; ok {
			panic("duplicate core id")
		}
		coreOrd[x.id] = x.ord
	}
	if len(coreOrd) != len(wanted) {
		panic("core IDs missing")
	}
	ad, ap, an := ids(os.Args[3])
	defer syscall.Munmap(ad)
	if an != 404079112 || n != 269194661 {
		panic("selected Q shape mismatch")
	}
	core, all := openCounts(os.Args[4]), openCounts(os.Args[5])
	defer func() {
		for _, f := range append(core, all...) {
			f.Close()
		}
	}()
	var out []result
	for _, t := range s.Works {
		if binary.LittleEndian.Uint64(ad[ap+t.Ordinal*8:ap+t.Ordinal*8+8]) != t.ID {
			panic("all ordinal ID mismatch")
		}
		r := result{ID: t.ID, Year: t.Year, Stratum: t.Stratum, CoreEligible: !t.XPAC, AllOrdinal: t.Ordinal, All: read(all, t.Ordinal)}
		if !t.XPAC {
			o := coreOrd[t.ID]
			r.CoreOrdinal = &o
			r.Core = read(core, o)
		}
		out = append(out, r)
	}
	b, e = json.MarshalIndent(map[string]any{"status": "passed", "sample_size": len(out), "core_eligible_targets": len(coreOrd), "count_kind": "Original integer deduplicated retained reference edges grouped by citing publication year 2000..2026; not Concepts label assignments or cumulative cited_by_count", "core_ineligible": "XPAC target absent from core Q; core array is null rather than an observed zero", "works": out, "elapsed_seconds": time.Since(start).Seconds(), "production_mutated": false}, "", "  ")
	must(e)
	must(os.WriteFile(os.Args[6], b, 0644))
	fmt.Printf("counts=%d coreeligible=%d seconds=%.2f\n", len(out), len(coreOrd), time.Since(start).Seconds())
}
