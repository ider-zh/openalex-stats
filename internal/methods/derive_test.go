package methods

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"openalexstats/internal/checkpoint"
	"openalexstats/internal/eci"
	"openalexstats/internal/pipeline"
	"openalexstats/internal/policy"
	"openalexstats/internal/results"
)

func baseFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	r, err := eci.Compute(eci.Matrix{Countries: []string{"A", "B", "C"}, Disciplines: []string{"x", "y", "z"}, Values: [][]float64{{10, 1, 1}, {5, 5, 1}, {1, 5, 10}}})
	if err != nil {
		t.Fatal(err)
	}
	var models []Model
	for _, l := range [][2]string{{"topics", "field"}, {"topics", "subfield"}, {"concepts", "0"}, {"concepts", "1"}} {
		for _, metric := range []string{"publications", "citations"} {
			models = append(models, Model{ScenarioID: results.MainScenarioID, Year: 2000, Scheme: l[0], Level: l[1], Metric: metric, Result: r})
		}
	}
	run := struct {
		RunID  string          `json:"run_id"`
		Status string          `json:"status"`
		Config pipeline.Config `json:"config"`
	}{"fixture-run", "full_snapshot_complete", pipeline.Config{YearStart: 2000, YearEnd: 2000, Scope: policy.Scope{Corpus: "core"}}}
	for name, v := range map[string]any{"run.json": run, "eci.json": models, "scenarios.json": results.DefaultScenarios()} {
		if err := checkpoint.JSON(filepath.Join(root, name), v); err != nil {
			t.Fatal(err)
		}
	}
	var artifacts []checkpoint.Artifact
	for _, name := range []string{"run.json", "eci.json", "scenarios.json"} {
		a, err := checkpoint.Hash(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		artifacts = append(artifacts, a)
	}
	if err := checkpoint.JSON(filepath.Join(root, "completion.json"), map[string]any{"run_id": "fixture-run", "artifacts": artifacts, "run": artifacts[0]}); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestDeriveSidecarAndIndependentFiniteAudit(t *testing.T) {
	base := baseFixture(t)
	before, _ := checkpoint.Hash(filepath.Join(base, "eci.json"))
	out := filepath.Join(t.TempDir(), "method")
	m, err := Derive(context.Background(), Config{Data: base, Output: out, BuildID: "fixture", Workers: 3})
	if err != nil {
		t.Fatal(err)
	}
	if m.BaseRunID != "fixture-run" || m.Corpus != "core" || len(m.Methods) != 1 || m.Methods[0].CountryIterations != 16 || m.Methods[0].DisciplineIterations != 17 {
		t.Fatal(m)
	}
	var a Audit
	if err = checkpoint.ReadJSON(filepath.Join(out, "audit.json"), &a); err != nil {
		t.Fatal(err)
	}
	if a.Status != "passed" || a.Models != 8 || a.ValidModels != 8 || a.NetworkChecks != 8 || a.IndependentIterationChecks != 8 || !a.BaseArtifactsUnchanged {
		t.Fatal(a)
	}
	var c Completion
	if err = checkpoint.ReadJSON(filepath.Join(out, "completion.json"), &c); err != nil {
		t.Fatal(err)
	}
	manifest, _ := checkpoint.Hash(filepath.Join(out, "manifest.json"))
	if c.Status != "complete" || manifest.SHA256 != c.ManifestSHA256 {
		t.Fatal(c)
	}
	for _, artifact := range c.Artifacts {
		artifact.Path = filepath.Join(out, artifact.Path)
		if err = artifact.Verify(); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := checkpoint.Hash(filepath.Join(base, "eci.json"))
	if before != after {
		t.Fatal("base changed")
	}
	if _, err = Derive(context.Background(), Config{Data: base, Output: out, BuildID: "fixture", Workers: 1}); err == nil {
		t.Fatal("overwrote completed sidecar")
	}
	var comparisons []Comparison
	if err = checkpoint.ReadJSON(filepath.Join(out, "sensitivity.json"), &comparisons); err != nil || len(comparisons) != 24 {
		t.Fatal(len(comparisons), err)
	}
}

func TestDeriveRejectsTamperedBaseAndNestedOutput(t *testing.T) {
	base := baseFixture(t)
	if _, err := Derive(context.Background(), Config{Data: base, Output: filepath.Join(base, "nested"), BuildID: "fixture", Workers: 1}); err == nil {
		t.Fatal("accepted nested result output")
	}
	b, _ := os.ReadFile(filepath.Join(base, "eci.json"))
	var data any
	_ = json.Unmarshal(b, &data)
	b = append(b, ' ')
	if err := os.WriteFile(filepath.Join(base, "eci.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Derive(context.Background(), Config{Data: base, Output: filepath.Join(t.TempDir(), "sidecar"), BuildID: "fixture", Workers: 1}); err == nil {
		t.Fatal("accepted tampered source")
	}
}
