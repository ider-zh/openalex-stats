// Merge independently gated UNKNOWN-only overlays without modifying any source.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
)

type gate struct {
	Route string `json:"route"`
	Path  string `json:"path"`
	SHA   string `json:"sha256"`
}
type input struct {
	Route string `json:"route"`
	Patch string `json:"patch_path"`
	SHA   string `json:"patch_sha256"`
	Gates []gate `json:"gates"`
}
type source struct {
	ID        uint64   `parquet:"id"`
	Year      int32    `parquet:"year"`
	XPAC      bool     `parquet:"is_xpac"`
	Countries []string `parquet:"proposed_countries,list"`
	Original  []string `parquet:"original_countries,list"`
	Route     string   `parquet:"source_route"`
}
type merged struct {
	ID         uint64   `parquet:"id"`
	Year       int32    `parquet:"year"`
	XPAC       bool     `parquet:"is_xpac"`
	Countries  []string `parquet:"proposed_countries,list"`
	Original   []string `parquet:"original_countries,list"`
	Routes     []string `parquet:"source_routes,list"`
	Complete   string   `parquet:"country_set_complete"`
	Divergence bool     `parquet:"source_set_divergence"`
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func union(a, b []string) []string {
	s := map[string]bool{}
	for _, v := range a {
		s[v] = true
	}
	for _, v := range b {
		s[v] = true
	}
	out := make([]string, 0, len(s))
	for v := range s {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
func add(m map[uint64]*merged, r source, route string, valid map[string]bool) error {
	if r.ID == 0 || r.Route != route || len(r.Original) > 0 || len(r.Countries) == 0 {
		return fmt.Errorf("invalid identity/route/UNKNOWN-only set W%d", r.ID)
	}
	cs := append([]string(nil), r.Countries...)
	sort.Strings(cs)
	for i, c := range cs {
		if !valid[c] || c == "UNKNOWN" || (i > 0 && cs[i-1] == c) {
			return fmt.Errorf("invalid country W%d %q", r.ID, c)
		}
	}
	if old, ok := m[r.ID]; ok {
		if old.Year != r.Year || old.XPAC != r.XPAC {
			return fmt.Errorf("cross-source identity conflict W%d", r.ID)
		}
		for _, rr := range old.Routes {
			if rr == route {
				return fmt.Errorf("duplicate source Work W%d %s", r.ID, route)
			}
		}
		// Partial author coverage may yield different, simultaneously supported
		// country sets. This is observable divergence, not proof of a conflict.
		old.Divergence = old.Divergence || !equal(old.Countries, cs)
		old.Countries = union(old.Countries, cs)
		old.Routes = union(old.Routes, []string{route})
	} else {
		m[r.ID] = &merged{ID: r.ID, Year: r.Year, XPAC: r.XPAC, Countries: cs, Original: []string{}, Routes: []string{route}, Complete: "unknown"}
	}
	return nil
}
func must(e error) {
	if e != nil {
		panic(e)
	}
}
func hash(path string) string {
	f, e := os.Open(path)
	must(e)
	defer f.Close()
	h := sha256.New()
	_, e = io.Copy(h, f)
	must(e)
	return hex.EncodeToString(h.Sum(nil))
}
func load(path string, v any) { b, e := os.ReadFile(path); must(e); must(json.Unmarshal(b, v)) }
func save(path string, v any) {
	b, e := json.MarshalIndent(v, "", "  ")
	must(e)
	must(os.WriteFile(path, append(b, '\n'), 0600))
}
func gateBinding(inputRoute, declaredRoute, wrapperRoute, status string) error {
	if status != "passed" || wrapperRoute != declaredRoute || (declaredRoute != inputRoute && declaredRoute != inputRoute+"/net_new") {
		return fmt.Errorf("passed gate is not bound to source/domain %s: %s/%s/%s", inputRoute, declaredRoute, wrapperRoute, status)
	}
	return nil
}
func main() {
	inputs := flag.String("inputs", "", "predeclared ordered source patch/gate list")
	dict := flag.String("dictionary", "", "immutable original dictionary")
	out := flag.String("out", "", "new immutable output directory")
	flag.Parse()
	if *inputs == "" || *dict == "" || *out == "" {
		panic("all arguments required")
	}
	if _, e := os.Stat(*out); !os.IsNotExist(e) {
		panic("output already exists")
	}
	var ins []input
	load(*inputs, &ins)
	if len(ins) == 0 {
		panic("no inputs")
	}
	var d struct {
		Countries []struct {
			ID string `json:"id"`
		} `json:"countries"`
	}
	load(*dict, &d)
	valid := map[string]bool{}
	for _, v := range d.Countries {
		valid[v.ID] = true
	}
	all := map[uint64]*merged{}
	gs := map[string]gate{}
	routes := map[string]bool{}
	counts := map[string]any{}
	for _, in := range ins {
		if routes[in.Route] || len(in.Gates) == 0 {
			panic("duplicate route or missing gate")
		}
		routes[in.Route] = true
		if hash(in.Patch) != in.SHA {
			panic("patch hash mismatch")
		}
		overallGate, netGate := false, false
		for _, g := range in.Gates {
			if hash(g.Path) != g.SHA {
				panic("gate hash mismatch")
			}
			var v struct {
				Status string `json:"status"`
				Route  string `json:"route"`
			}
			load(g.Path, &v)
			must(gateBinding(in.Route, g.Route, v.Route, v.Status))
			overallGate = overallGate || g.Route == in.Route
			netGate = netGate || g.Route == in.Route+"/net_new"
			gs[g.Path] = g
		}
		if !overallGate || (in.Route != "v1_strict_text_only" && !netGate) {
			panic("external source requires separately bound overall and net-new gates")
		}
		f, e := os.Open(in.Patch)
		must(e)
		pr := parquet.NewGenericReader[source](f)
		buf := make([]source, 4096)
		before := len(all)
		n := 0
		for {
			k, e := pr.Read(buf)
			for _, r := range buf[:k] {
				must(add(all, r, in.Route, valid))
				n++
			}
			if e == io.EOF {
				break
			}
			must(e)
		}
		must(pr.Close())
		must(f.Close())
		counts[in.Route] = map[string]int{"patch_rows": n, "net_new_works_in_declared_order": len(all) - before}
	}
	ids := make([]uint64, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	must(os.MkdirAll(*out, 0700))
	target := filepath.Join(*out, "validated-merged-patch.parquet")
	f, e := os.Create(target)
	must(e)
	w := parquet.NewGenericWriter[merged](f, parquet.Compression(&zstd.Codec{}))
	buffer := make([]merged, 0, 4096)
	divergence := 0
	for _, id := range ids {
		r := all[id]
		if r.Divergence {
			divergence++
		}
		buffer = append(buffer, *r)
		if len(buffer) == cap(buffer) {
			_, e = w.Write(buffer)
			must(e)
			buffer = buffer[:0]
		}
	}
	_, e = w.Write(buffer)
	must(e)
	must(w.Close())
	must(f.Close())
	gates := make([]gate, 0, len(gs))
	for _, g := range gs {
		gates = append(gates, g)
	}
	sort.Slice(gates, func(i, j int) bool { return gates[i].Path < gates[j].Path })
	save(filepath.Join(*out, "overlay-manifest.json"), map[string]any{"status": "passed_validated_overlay", "version": "v3-merged", "rows": len(ids), "patch_sha256": hash(target), "gates": gates, "inputs": ins, "inputs_sha256": hash(*inputs), "route_counts": counts, "source_set_divergence_works": divergence, "country_set_complete": "unknown", "original_version_preserved": true, "production_applied": false, "note": "Only independently supported original-UNKNOWN countries unioned. Different partial source sets are marked divergence. Identity inconsistencies fail without silently dropping rows."})
	fmt.Printf("merged=%d divergence=%d output=%s\n", len(ids), divergence, *out)
}
