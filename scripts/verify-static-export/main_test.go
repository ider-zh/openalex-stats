package main

import (
	"compress/gzip"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/parquet-go/parquet-go"
)

func testMatrix(t *testing.T, root, name string, total [2]float64, records map[int][2]float64) {
	t.Helper()
	b := make([]byte, 16+4*16)
	copy(b, "OAQ1")
	binary.LittleEndian.PutUint16(b[4:], 2000)
	binary.LittleEndian.PutUint16(b[6:], 1)
	binary.LittleEndian.PutUint16(b[8:], 3)
	for i, v := range total {
		binary.LittleEndian.PutUint64(b[16+i*8:], math.Float64bits(v))
	}
	for ci, pair := range records {
		for i, v := range pair {
			binary.LittleEndian.PutUint64(b[32+ci*16+i*8:], math.Float64bits(v))
		}
	}
	p := filepath.Join(root, filepath.FromSlash(name))
	if e := os.MkdirAll(filepath.Dir(p), 0755); e != nil {
		t.Fatal(e)
	}
	f, e := os.Create(p)
	if e != nil {
		t.Fatal(e)
	}
	w := gzip.NewWriter(f)
	if _, e = w.Write(b); e != nil {
		t.Fatal(e)
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
}
func transportFixture(t *testing.T) (*verifier, dataset, string) {
	t.Helper()
	root := t.TempDir()
	input := t.TempDir()
	d := dataset{Prefix: "/data/test", Meta: metadata{Countries: []label{{"CN"}, {"US"}}, Layers: []layer{{Scheme: "topics", Level: "field", Labels: []label{{"f1"}, {"f2"}}}}, Scenarios: []label{{"main"}}, Start: 2000, End: 2000}}
	rows := []sourceRow{{"main", "CN", 2000, "topics", "field", "f1", 2, 4}, {"main", "US", 2000, "topics", "field", "f1", 6, 12}, {"main", "CN", 2000, "topics", "field", "f2", 2, 0}, {"main", "US", 2000, "topics", "field", "f2", 0, 4}, {"main", "UNKNOWN", 2000, "topics", "field", "f1", 100, 100}, {"main", "CN", 2000, "topics", "field", "UNKNOWN", 20, 50}}
	f, e := os.Create(filepath.Join(input, "statistics.parquet"))
	if e != nil {
		t.Fatal(e)
	}
	w := parquet.NewGenericWriter[sourceRow](f)
	if _, e = w.Write(rows); e != nil {
		t.Fatal(e)
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	_ = f.Close()
	testMatrix(t, root, "data/test/counts/0/0/0.bin.gz", [2]float64{8, 16}, map[int][2]float64{0: {2, 4}, 1: {6, 12}, 2: {100, 100}})
	testMatrix(t, root, "data/test/counts/0/0/1.bin.gz", [2]float64{2, 4}, map[int][2]float64{0: {2, 0}, 1: {0, 4}})
	testMatrix(t, root, "data/test/counts/0/0/2.bin.gz", [2]float64{}, map[int][2]float64{0: {20, 50}})
	testMatrix(t, root, "data/test/counts/0/0/margins.bin.gz", [2]float64{10, 20}, map[int][2]float64{0: {4, 4}, 1: {6, 16}})
	return &verifier{root: root, matrices: map[string]*matrix{}, models: map[string]modelShard{}}, d, input
}
func TestTransportAuditRejectsCountAndDenominatorCorruption(t *testing.T) {
	v, d, input := transportFixture(t)
	var valid summary
	if e := auditCounts(v, d, input, &valid); e != nil {
		t.Fatal(e)
	}
	if valid.SourceRows != 6 || valid.NonzeroMatrix != 6 {
		t.Fatal("fixture universe changed", valid)
	}
	testMatrix(t, v.root, "data/test/counts/0/0/0.bin.gz", [2]float64{8, 16}, map[int][2]float64{0: {2.25, 4}, 1: {6, 12}, 2: {100, 100}})
	v.matrices = map[string]*matrix{}
	var bad summary
	if e := auditCounts(v, d, input, &bad); e == nil || bad.CountDifferences != 1 {
		t.Fatal("accepted a wrong country value", bad)
	}
	v, d, input = transportFixture(t)
	testMatrix(t, v.root, "data/test/counts/0/0/margins.bin.gz", [2]float64{110, 120}, map[int][2]float64{0: {24, 54}, 1: {6, 16}})
	v.matrices = map[string]*matrix{}
	bad = summary{}
	if e := auditCounts(v, d, input, &bad); e == nil || bad.MarginDifferences == 0 {
		t.Fatal("accepted UNKNOWN in global denominators", bad)
	}
}
func TestTransportRankUsesEveryActiveSourceEntityAndCompetitionTies(t *testing.T) {
	r, n, e := ranked([]string{"US", "CN", "GB"}, []int{2, 0, 1}, []float64{1, 1, -.5})
	if e != nil || n != 3 || r["US"].rank != 1 || r["GB"].rank != 1 || r["CN"].rank != 3 {
		t.Fatal(r, n, e)
	}
}
