package eci

import (
	"encoding/json"
	"math"
	"testing"
)

func TestFiniteReflectionsReferenceAndPreservedNetwork(t *testing.T) {
	m := Matrix{Countries: []string{"A", "B", "C"}, Disciplines: []string{"x", "y", "z"}, Values: [][]float64{{10, 1, 1}, {5, 5, 1}, {1, 5, 10}}}
	base, err := Compute(m)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(base)
	r, diag, err := ReflectionsFromPrepared(base, DefaultReflectionConfig())
	if err != nil || r.Status != "ok" {
		t.Fatal(r.Status, err)
	}
	// Independent old-Java raw n=16 calculation archived in numeric-comparison.
	want := []float64{-1.1180341609017561, -0.1909827270774697, 1.3090168879792037}
	for i, v := range want {
		if math.Abs(r.ECI[i]-v) > 2e-10 {
			t.Fatalf("ECI[%d]=%.16g want %.16g", i, r.ECI[i], v)
		}
	}
	for _, scores := range [][]float64{r.ECI, r.PCI} {
		mean, sq := 0.0, 0.0
		for _, v := range scores {
			mean += v
			sq += v * v
		}
		if math.Abs(mean) > 1e-9 || math.Abs(sq/float64(len(scores))-1) > 1e-12 {
			t.Fatal("not population z", scores)
		}
	}
	// kp17 is the degree-normalized projection of kc16, standardized separately.
	projected := []float64{(r.ECI[0] + r.ECI[1]) / 2, (r.ECI[1] + r.ECI[2]) / 2, r.ECI[2]}
	standardize(projected)
	for i, v := range projected {
		if math.Abs(v-r.PCI[i]) > 1e-9 {
			t.Fatal("PCI odd iteration", projected, r.PCI)
		}
	}
	after, _ := json.Marshal(base)
	if string(before) != string(after) {
		t.Fatal("modified original spectral result")
	}
	if diag.CountrySignalULPs < 32 || r.Residual <= 0 {
		t.Fatal("missing finite-iteration diagnostics", diag, r.Residual)
	}
	short, _, _ := ReflectionsFromPrepared(base, ReflectionConfig{4, 5})
	if math.Abs(short.ECI[0]-r.ECI[0]) < 1e-3 {
		t.Fatal("silently substituted spectral vector for finite reflections")
	}
	_, firstStep, err := ReflectionsFromPrepared(base, ReflectionConfig{2, 3})
	if err != nil || firstStep.CountryTwoStepMaxChange != .5 {
		t.Fatal("two-step diagnostic must compare against degree initial values", firstStep, err)
	}
}

func TestReflectionsRejectInvalidAndDoNotFallbackToDegree(t *testing.T) {
	m := Matrix{Countries: []string{"A", "B", "C", "D"}, Disciplines: []string{"x", "y", "z"}, Values: [][]float64{{1, 1, 0}, {1, 0, 1}, {0, 1, 1}, {0, 1, 1}}}
	base, err := Compute(m)
	if err != nil || base.Status != "ok" {
		t.Fatal(base.Status, err)
	}
	r, _, err := ReflectionsFromPrepared(base, DefaultReflectionConfig())
	if err != nil || r.Status != "degenerate" || len(r.ECI) != 0 || len(r.PCI) != 0 {
		t.Fatal("constant initial degree fabricated complexity", r, err)
	}
	for _, cfg := range []ReflectionConfig{{0, 1}, {15, 16}, {16, 16}, {102, 103}} {
		if _, _, err := ReflectionsFromPrepared(base, cfg); err == nil {
			t.Fatal("accepted invalid config", cfg)
		}
	}
	disconnected := Matrix{Countries: []string{"A", "B"}, Disciplines: []string{"x", "y"}, Values: [][]float64{{1, 0}, {0, 1}}}
	r, _, err = ComputeReflections(disconnected, DefaultReflectionConfig())
	if err != nil || r.Status != "disconnected" || len(r.ECI) != 0 {
		t.Fatal(r.Status, err)
	}
	base.ActiveCountries = append(base.ActiveCountries, 0)
	if _, _, err := ReflectionsFromPrepared(base, DefaultReflectionConfig()); err == nil {
		t.Fatal("accepted duplicate active country")
	}
}
