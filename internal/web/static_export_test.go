package web

import (
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openalexstats/internal/checkpoint"
)

func staticCompletionFixture(t *testing.T, root string) {
	t.Helper()
	content, e := os.ReadFile(filepath.Join(root, "run.json"))
	if e != nil {
		t.Fatal(e)
	}
	var record map[string]any
	if e = json.Unmarshal(content, &record); e != nil {
		t.Fatal(e)
	}
	record["run_id"] = strings.Repeat("a", 64)
	jsonFile(t, root, "run.json", record)
	run, err := checkpoint.Hash(filepath.Join(root, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var artifacts []checkpoint.Artifact
	for _, entry := range entries {
		if entry.Name() == "run.json" || entry.Name() == "completion.json" || entry.IsDir() {
			continue
		}
		a, e := checkpoint.Hash(filepath.Join(root, entry.Name()))
		if e != nil {
			t.Fatal(e)
		}
		artifacts = append(artifacts, a)
	}
	jsonFile(t, root, "completion.json", map[string]any{"version": 1, "run_id": record["run_id"], "run": run, "artifacts": artifacts})
}
func readStaticMatrix(t *testing.T, path string) []byte {
	t.Helper()
	f, e := os.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	r, e := gzip.NewReader(f)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	b, e := io.ReadAll(r)
	if e != nil {
		t.Fatal(e)
	}
	if string(b[:4]) != "OAQ1" {
		t.Fatal("bad binary version")
	}
	return b
}
func staticPair(b []byte, ci, yi, basis int) float64 {
	years := int(binary.LittleEndian.Uint16(b[6:]))
	return math.Float64frombits(binary.LittleEndian.Uint64(b[16+years*16+(ci*years+yi)*16+basis*8:]))
}

func TestStaticExportPreservesCountsMarginsModelsAndReplay(t *testing.T) {
	core := fixture(t, true)
	staticCompletionFixture(t, core)
	out := filepath.Join(t.TempDir(), "export")
	cfg := StaticExportConfig{CoreDirectory: core, OutputDirectory: out, Workers: 2}
	r, e := ExportStatic(context.Background(), cfg, nil)
	if e != nil {
		t.Fatal(e)
	}
	if r.Corpora[0].InputCells == 0 || r.Corpora[0].NonzeroCells == 0 || r.Files < 10 {
		t.Fatal("incomplete export", r)
	}
	b, e := os.ReadFile(filepath.Join(out, "data/site.json"))
	if e != nil {
		t.Fatal(e)
	}
	var site staticSite
	if e = json.Unmarshal(b, &site); e != nil {
		t.Fatal(e)
	}
	d := site.Datasets["core"]
	var meta struct {
		Countries []Label `json:"countries"`
		Layers    []Layer `json:"layers"`
		Scenarios []struct {
			ID string `json:"id"`
		} `json:"scenarios"`
	}
	if e = json.Unmarshal(d.Meta, &meta); e != nil {
		t.Fatal(e)
	}
	cn, us, field, f1, empty, scenario := -1, -1, -1, -1, -1, -1
	for i, c := range meta.Countries {
		if c.ID == "CN" {
			cn = i
		}
		if c.ID == "US" {
			us = i
		}
	}
	for i, l := range meta.Layers {
		if l.Scheme == "topics" && l.Level == "field" {
			field = i
			for j, label := range l.Labels {
				if label.ID == "f1" {
					f1 = j
				}
				if label.ID == "empty" {
					empty = j
				}
			}
		}
	}
	for i, s := range meta.Scenarios {
		if s.ID == "fractional-concept-0.3" {
			scenario = i
		}
	}
	if cn < 0 || us < 0 || field < 0 || f1 < 0 || empty < 0 || scenario < 0 {
		t.Fatal("metadata dictionary missing")
	}
	base := filepath.Join(out, filepath.FromSlash(d.Prefix[1:]), "counts", fmt.Sprint(scenario), fmt.Sprint(field))
	matrix := readStaticMatrix(t, filepath.Join(base, fmt.Sprint(f1)+".bin.gz"))
	if staticPair(matrix, cn, 0, 0) != 2 || staticPair(matrix, us, 0, 1) != 12 || staticPair(matrix, len(meta.Countries), 0, 0) != 100 {
		t.Fatal("counts, implicit zeros or UNKNOWN lost")
	}
	margins := readStaticMatrix(t, filepath.Join(base, "margins.bin.gz"))
	if staticPair(margins, cn, 0, 0) != 4 || staticPair(margins, us, 0, 0) != 6 {
		t.Fatal("RCA country denominator included UNKNOWN")
	}
	total := math.Float64frombits(binary.LittleEndian.Uint64(margins[16:]))
	col := math.Float64frombits(binary.LittleEndian.Uint64(matrix[16:]))
	if total != 10 || col != 8 {
		t.Fatal("RCA global/discipline denominators changed", total, col)
	}
	zero := readStaticMatrix(t, filepath.Join(base, fmt.Sprint(empty)+".bin.gz"))
	if staticPair(zero, cn, 0, 0) != 0 {
		t.Fatal("empty dictionary label not exported")
	}
	if d.Downloads["run.json"].Path == "" || d.Downloads["run.json"].SHA256 == "" || len(d.Models) == 0 {
		t.Fatal("missing provenance/model lookups")
	}
	replayed, e := ExportStatic(context.Background(), cfg, nil)
	if e != nil {
		t.Fatal(e)
	}
	if replayed.Bytes != r.Bytes || replayed.Files != r.Files {
		t.Fatal("non-deterministic replay")
	}
	for i, a := range r.Artifacts {
		if a != replayed.Artifacts[i] {
			t.Fatal("replay artifact changed", a)
		}
	}
}

func TestStaticExportRejectsChangedSourcesAndNestedOutput(t *testing.T) {
	core := fixture(t, true)
	staticCompletionFixture(t, core)
	if _, e := ExportStatic(context.Background(), StaticExportConfig{CoreDirectory: core, OutputDirectory: filepath.Join(core, "export"), Workers: 1}, nil); e == nil {
		t.Fatal("allowed output in immutable result")
	}
	f, e := os.OpenFile(filepath.Join(core, "run.json"), os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	_, _ = f.WriteString(" ")
	_ = f.Close()
	if _, e = ExportStatic(context.Background(), StaticExportConfig{CoreDirectory: core, OutputDirectory: t.TempDir(), Workers: 1}, nil); e == nil {
		t.Fatal("accepted changed completion source")
	}
}

func TestStaticBinaryKeepsExactBinary64AndZero(t *testing.T) {
	value := 1.0 / 3
	b := encodeStaticMatrix(2000, 2, 2, [][2]float64{{value, 9}, {0, 0}}, [][4]float64{{1, 0, value, 7}})
	if math.Float64bits(staticPair(b, 1, 0, 0)) != math.Float64bits(value) || staticPair(b, 0, 0, 0) != 0 || staticPair(b, 1, 1, 1) != 0 {
		t.Fatal("float64 precision or implicit zeros changed")
	}
}
