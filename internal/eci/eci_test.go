package eci

import (
	"math"
	"testing"
)

func TestConnectedECIProperties(t *testing.T) {
	m := Matrix{Countries: []string{"A", "B", "C", "zero"}, Disciplines: []string{"x", "y", "z", "empty"}, Values: [][]float64{{10, 1, 1, 0}, {5, 5, 1, 0}, {1, 5, 10, 0}, {0, 0, 0, 0}}}
	r, err := Compute(m)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "ok" {
		t.Fatalf("status %+v", r)
	}
	if len(r.ECI) != 3 || len(r.PCI) != 3 || len(r.ActiveCountries) != 3 {
		t.Fatal("inactive rows must have no score")
	}
	if r.Residual > 1e-12 {
		t.Fatal("spectral residual", r.Residual)
	}
	for _, v := range [][]float64{r.ECI, r.PCI} {
		mean, sq := 0.0, 0.0
		for _, x := range v {
			mean += x
			sq += x * x
		}
		if math.Abs(mean) > 1e-12 || math.Abs(sq/float64(len(v))-1) > 1e-12 {
			t.Fatal("not population-standardized", v)
		}
	}
	if !r.Advantage[0][0] || r.Advantage[0][1] || !r.Advantage[1][0] || !r.Advantage[1][1] || !r.Advantage[2][1] || !r.Advantage[2][2] {
		t.Fatalf("RCA pattern %+v", r.Advantage)
	}
	// Scaling all publications cannot change RCA or complexity.
	for i := range m.Values {
		for j := range m.Values[i] {
			m.Values[i][j] *= 13
		}
	}
	scaled, _ := Compute(m)
	for i, x := range r.ECI {
		if math.Abs(x-scaled.ECI[i]) > 1e-12 {
			t.Fatal("scale-sensitive ECI")
		}
	}
	// The symmetric transform must yield an eigenvector of the row-normalized
	// country operator, up to standardization's additive/multiplicative changes.
	operatorECI := make([]float64, 3)
	for i := 0; i < 3; i++ {
		div := 0.0
		for k := 0; k < 3; k++ {
			if r.Advantage[i][k] {
				div++
			}
		}
		for j := 0; j < 3; j++ {
			a := 0.0
			for k := 0; k < 3; k++ {
				ub := 0.0
				for c := 0; c < 3; c++ {
					if r.Advantage[c][k] {
						ub++
					}
				}
				if r.Advantage[i][k] && r.Advantage[j][k] {
					a += 1 / (div * ub)
				}
			}
			operatorECI[i] += a * r.ECI[j]
		}
	}
	if !standardize(operatorECI) {
		t.Fatal("operator collapsed")
	}
	for i, x := range r.ECI {
		if math.Abs(x-operatorECI[i]) > 1e-10 {
			t.Fatal("incorrect non-symmetric transform")
		}
	}
}

func TestUndefinedScoresAreExplicit(t *testing.T) {
	cases := []struct {
		m      Matrix
		status string
	}{{Matrix{Countries: []string{"A", "B"}, Disciplines: []string{"x", "y"}, Values: [][]float64{{10, 0}, {0, 10}}}, "disconnected"}, {Matrix{Countries: []string{"A", "B"}, Disciplines: []string{"x", "y"}, Values: [][]float64{{1, 1}, {1, 1}}}, "degenerate"}, {Matrix{Countries: []string{"A"}, Disciplines: []string{"x"}, Values: [][]float64{{0}}}, "empty"}}
	for _, tc := range cases {
		r, err := Compute(tc.m)
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != tc.status || len(r.ECI) > 0 || len(r.PCI) > 0 {
			t.Fatalf("expected %s without invented scores: %+v", tc.status, r)
		}
		if tc.status == "disconnected" && (r.ComponentCount != 2 || len(r.Components) != 2 || r.EigenGap > 1e-10 || !r.Unstable) {
			t.Fatalf("missing disconnected-network diagnostics: %+v", r)
		}
	}
}

func TestInvalidInput(t *testing.T) {
	for _, m := range []Matrix{{Countries: []string{"A"}, Disciplines: []string{"x"}, Values: [][]float64{{-1}}}, {Countries: []string{"A"}, Disciplines: []string{"x"}, Values: [][]float64{{math.NaN()}}}, {Countries: []string{"A", "A"}, Values: [][]float64{{}, {}}}, {Countries: []string{"UNKNOWN"}, Disciplines: []string{"x"}, Values: [][]float64{{1}}}} {
		if _, err := Compute(m); err == nil {
			t.Fatal("accepted invalid matrix", m)
		}
	}
}

func TestProportionalPortfoliosRemainDegenerate(t *testing.T) {
	for _, scale := range []float64{.1, .7, 1, 17.3} {
		m := Matrix{Countries: []string{"A", "B", "C"}, Disciplines: []string{"x", "y", "z"}}
		for _, country := range []float64{.1, .7, 1.3} {
			row := []float64{}
			for _, discipline := range []float64{.2, .9, 4.7} {
				row = append(row, country*discipline*scale)
			}
			m.Values = append(m.Values, row)
		}
		r, err := Compute(m)
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != "degenerate" {
			t.Fatalf("identical portfolios have invented complexity: scale=%g status=%s RCA=%v", scale, r.Status, r.RCA)
		}
	}
}
