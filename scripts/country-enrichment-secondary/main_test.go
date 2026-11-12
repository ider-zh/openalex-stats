package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/model"
	"openalexstats/internal/store"
)

func TestNormalizationMatchesFrozenPrimary(t *testing.T) {
	for _, s := range []struct{ in, want string }{{" A\tB\u00a0C\n", "a b c"}, {"Université,  FRANCE", "université, france"}, {"India", "india"}} {
		if got := norm(s.in); got != s.want {
			t.Errorf("%q -> %q", s.in, got)
		}
	}
}

func TestSparseCitationOrdinalsAndUnknownTargets(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "dimensions.bin")
	columns := store.New()
	for _, w := range []model.Work{{ID: 101, Countries: []string{"US"}}, {ID: 333}, {ID: 202, Countries: []string{"CN"}}, {ID: 444}, {ID: 987, Countries: []string{"GB"}}} {
		if e := columns.Append(w); e != nil {
			t.Fatal(e)
		}
	}
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = columns.WriteTo(f); e != nil {
		t.Fatal(e)
	}
	_ = f.Close()
	flags := map[uint64]byte{333: 1, 444: 3}
	targets, e := findTargets(path, 5, flags, map[uint64]bool{333: true}, "all")
	if e != nil {
		t.Fatal(e)
	}
	if len(targets[1]) != 1 || targets[1][0].ordinal != 1 || len(targets[3]) != 1 || targets[3][0].ordinal != 3 {
		t.Fatal("sparse ID/ordinal mapping changed", targets)
	}
	core, e := findTargets(path, 5, flags, map[uint64]bool{333: true}, "core")
	if e != nil || len(core[1]) != 0 || len(core[3]) != 1 {
		t.Fatal("core scope changed", core, e)
	}
	if _, e = findTargets(path, 5, map[uint64]byte{987: 1}, map[uint64]bool{}, "all"); e == nil {
		t.Fatal("already-known target accepted")
	}
	work, out, analysis := filepath.Join(root, "work"), filepath.Join(root, "secondary"), filepath.Join(root, "analysis")
	for _, d := range []string{work, out, analysis, filepath.Join(root, "counts")} {
		if e = os.MkdirAll(d, 0755); e != nil {
			t.Fatal(e)
		}
	}
	var artifacts []checkpoint.Artifact
	for p := 0; p < 64; p++ {
		var data []byte
		if p < 5 {
			data = make([]byte, 27*8)
			switch p {
			case 0:
				binary.LittleEndian.PutUint64(data, 2)
			case 1:
				binary.LittleEndian.PutUint64(data, 2)
			case 3:
				binary.LittleEndian.PutUint64(data[8:], 3)
			}
		}
		file := filepath.Join(root, "counts", fmt.Sprintf("target-%03d.counts", p))
		if e = os.WriteFile(file, data, 0600); e != nil {
			t.Fatal(e)
		}
		a, e := checkpoint.Hash(file)
		if e != nil {
			t.Fatal(e)
		}
		artifacts = append(artifacts, a)
	}
	dim, e := checkpoint.Hash(path)
	if e != nil {
		t.Fatal(e)
	}
	rep := primaryCitationReport{Corpus: "core", Rows: 5, Dimensions: dim, Counters: artifacts}
	rep.Total.Events = 7
	rep.Total.Missing = 5
	all := rep
	all.Corpus = "all"
	if e = checkpoint.JSON(filepath.Join(work, "citation-summary.json"), map[string]any{"status": "passed", "reports": []primaryCitationReport{rep, all}, "completed_at": time.Now().UTC()}); e != nil {
		t.Fatal(e)
	}
	if e = checkpoint.JSON(filepath.Join(analysis, "secondary-invariants.json"), []map[string]uint64{{"secondary_rows": 2, "unique_ids": 2, "overlap_with_v1_newly_known": 0, "invalid_secondary_rows": 0}}); e != nil {
		t.Fatal(e)
	}
	f, e = os.Create(filepath.Join(analysis, "secondary-patches.parquet"))
	if e != nil {
		t.Fatal(e)
	}
	writer := parquet.NewGenericWriter[secondaryCandidate](f)
	if _, e = writer.Write([]secondaryCandidate{{ID: 333, Geo: true, Countries: []string{"FR"}, Review: true}, {ID: 444, Geo: true, Dictionary: true, Countries: []string{"US"}, Review: true}}); e != nil {
		t.Fatal(e)
	}
	if e = writer.Close(); e != nil {
		t.Fatal(e)
	}
	_ = f.Close()
	if e = conditionalCitations(work, out, analysis, 2); e != nil {
		t.Fatal(e)
	}
	var result struct {
		Status  string `json:"status"`
		Reports []struct {
			Extra conditionalYear `json:"secondary_additional_review_events"`
		} `json:"reports"`
	}
	if e = checkpoint.ReadJSON(filepath.Join(out, "conditional-citation-summary.json"), &result); e != nil {
		t.Fatal(e)
	}
	if result.Status != "passed" || len(result.Reports) != 2 {
		t.Fatal(result)
	}
	for _, r := range result.Reports {
		if r.Extra.Union != 5 || r.Extra.Geo != 5 || r.Extra.Dictionary != 3 || r.Extra.Both != 3 {
			t.Fatal("conditional graph counters changed", r)
		}
	}
}
