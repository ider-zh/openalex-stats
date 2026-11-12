package web

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"openalexstats/internal/checkpoint"
	"openalexstats/internal/eci"
	"openalexstats/internal/methods"
)

// This is a hand-sized HTTP method-selection fixture, not a validation of the
// reflection solver (which has its own independent numerical audits).
func sidecarFixture(t *testing.T, change func(*methods.Manifest, []modelResult)) (string, string) {
	t.Helper()
	base := fixture(t, false)
	side := t.TempDir()
	b, err := os.ReadFile(filepath.Join(base, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	var run map[string]any
	if err = json.Unmarshal(b, &run); err != nil {
		t.Fatal(err)
	}
	run["run_id"] = "test-core-methods"
	jsonFile(t, base, "run.json", run)
	jsonFile(t, base, "completion.json", map[string]any{"run_id": "test-core-methods"})
	b, err = os.ReadFile(filepath.Join(base, "eci.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []modelResult
	if err = json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		rows[i].ScenarioID = "current"
		if rows[i].Result.Status == "ok" {
			rows[i].Result.ECI = []float64{-1, 1}
			rows[i].Result.PCI = []float64{-1, 1}
		}
	}
	completionBytes, err := os.ReadFile(filepath.Join(base, "completion.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := methods.Manifest{Version: 1, BaseRunID: "test-core-methods", Corpus: "core", ScenarioID: "current", SourceCompletionSHA256: bytesSHA(completionBytes), Methods: []methods.Definition{{ID: eci.ReflectionsMethodID, DisplayName: "Java 反射法·修正版", CountryIterations: 16, DisciplineIterations: 17, Normalization: "population-z", Models: "java-reflections-v1.json"}}}
	if change != nil {
		change(&manifest, rows)
	}
	jsonFile(t, side, "java-reflections-v1.json", rows)
	jsonFile(t, side, "audit.json", map[string]any{"status": "fixture"})
	jsonFile(t, side, "sensitivity.json", []any{})
	modelHash, err := checkpoint.Hash(filepath.Join(side, "java-reflections-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest.Methods[0].ModelsSHA256 = modelHash.SHA256
	jsonFile(t, side, "manifest.json", manifest)
	var artifacts []checkpoint.Artifact
	for _, name := range []string{"manifest.json", "java-reflections-v1.json", "audit.json", "sensitivity.json"} {
		a, e := checkpoint.Hash(filepath.Join(side, name))
		if e != nil {
			t.Fatal(e)
		}
		a.Path = name
		artifacts = append(artifacts, a)
	}
	jsonFile(t, side, "completion.json", methods.Completion{Version: 1, Status: "complete", BaseRunID: manifest.BaseRunID, ManifestSHA256: artifacts[0].SHA256, Artifacts: artifacts})
	return base, side
}

func TestMethodIsolationRanksCSVAndDownloads(t *testing.T) {
	base, side := sidecarFixture(t, nil)
	h, err := NewWithMethods(base, side)
	if err != nil {
		t.Fatal(err)
	}
	p := params("eci")
	defaultResult := series(t, h, p)
	if defaultResult.Query.Method != DefaultMethod || defaultResult.Method.ID != DefaultMethod {
		t.Fatal("old URL lost spectral default", defaultResult)
	}
	p.Set("method", eci.ReflectionsMethodID)
	reflection := series(t, h, p)
	if !reflection.MethodApplies || reflection.Method.CountryIterations != 16 || reflection.Method.DisciplineIterations != 17 || reflection.Method.SourceRunID != "test-core-methods" {
		t.Fatal("missing method provenance", reflection.Method)
	}
	closeValue(t, reflection.Series[0].Points[0].Value, -1)
	closeValue(t, defaultResult.Series[0].Points[0].Value, 1.2)
	if *reflection.Series[0].Points[0].Rank != 2 || *defaultResult.Series[0].Points[0].Rank != 1 {
		t.Fatal("method switched score without rank")
	}
	for _, metric := range []string{"publications", "citations", "share", "rca"} {
		p.Set("metric", metric)
		java := series(t, h, p)
		p.Set("method", DefaultMethod)
		spectral := series(t, h, p)
		p.Set("method", eci.ReflectionsMethodID)
		if java.MethodApplies || !reflect.DeepEqual(java.Series, spectral.Series) {
			t.Fatalf("%s changed with method", metric)
		}
	}
	p.Set("metric", "pci")
	p.Del("discipline")
	p.Set("disciplines", "f1,f2")
	pci := series(t, h, p)
	if len(pci.Series) != 2 || pci.Series[0].Country != "GLOBAL" {
		t.Fatal("method PCI dimensions wrong")
	}
	closeValue(t, pci.Series[0].Points[0].Value, -1)
	if *pci.Series[0].Points[0].Rank != 2 {
		t.Fatal("PCI reused other method's global rank")
	}
	w := request(t, h, "/api/series.csv?"+p.Encode())
	records, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if records[0][17] != "method" || records[1][17] != eci.ReflectionsMethodID || records[1][18] != "16" || records[1][19] != "17" || records[1][20] != "test-core-methods" || records[1][21] != "true" {
		t.Fatal("CSV method provenance missing", records[:2])
	}
	w = request(t, h, "/download?file=eci.json&method="+eci.ReflectionsMethodID)
	if w.Code != 200 || w.Header().Get("X-OpenAlex-Method") != eci.ReflectionsMethodID || !strings.Contains(w.Body.String(), "-1") {
		t.Fatal("method download routed to base", w.Code)
	}
	if w = request(t, h, "/download?file=method-sensitivity.json&method="+eci.ReflectionsMethodID); w.Code != 200 {
		t.Fatal("no safe sensitivity download", w.Code)
	}
	p.Set("method", "unknown")
	if w = request(t, h, "/api/series?"+p.Encode()); w.Code != 400 {
		t.Fatal("unknown method accepted")
	}
	if w = request(t, h, "/download?file=../run.json&method="+eci.ReflectionsMethodID); w.Code != 400 {
		t.Fatal("method path traversal accepted")
	}
}

func TestMethodSidecarSourceNetworkAndChecksumGates(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*methods.Manifest, []modelResult)
	}{
		{"wrong_corpus", func(m *methods.Manifest, _ []modelResult) { m.Corpus = "all" }},
		{"wrong_source_completion", func(m *methods.Manifest, _ []modelResult) { m.SourceCompletionSHA256 = strings.Repeat("0", 64) }},
		{"different_network", func(_ *methods.Manifest, rows []modelResult) { rows[0].Result.Advantage = [][]bool{{true}} }},
		{"wrong_iterations", func(m *methods.Manifest, _ []modelResult) { m.Methods[0].CountryIterations = 4 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			base, side := sidecarFixture(t, test.change)
			if _, err := NewWithMethods(base, side); err == nil {
				t.Fatal("untrusted method accepted")
			}
		})
	}
	base, side := sidecarFixture(t, nil)
	if err := os.WriteFile(filepath.Join(side, "java-reflections-v1.json"), []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewWithMethods(base, side); err == nil {
		t.Fatal("tampered method artifact accepted")
	}
}
