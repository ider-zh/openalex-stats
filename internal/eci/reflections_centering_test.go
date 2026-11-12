package eci

import (
	"math"
	"testing"
)

func TestReflectionsStandardizationNearNonzeroConstant(t *testing.T) {
	indices := []float64{0, 1, 2, 2, 3, 5, 8, 13, 21, 21, 34, 55, 89}
	x := make([]float64, len(indices))
	step := math.Ldexp(1, -36)
	mean := 0.0
	for i, v := range indices {
		x[i] = 128 + step*v
		mean += v
	}
	mean /= float64(len(indices))
	variance := 0.0
	for _, v := range indices {
		variance += (v - mean) * (v - mean)
	}
	sd := math.Sqrt(variance / float64(len(indices)))
	if !standardizeReflections(x) {
		t.Fatal("lost representable nonconstant signal")
	}
	m, sq := 0.0, 0.0
	for i, v := range x {
		want := (indices[i] - mean) / sd
		if math.Abs(v-want) > 1e-13 {
			t.Fatalf("z[%d]=%.16g want %.16g", i, v, want)
		}
		m += v
		sq += v * v
	}
	if math.Abs(m/float64(len(x))) > 1e-14 || math.Abs(sq/float64(len(x))-1) > 1e-13 {
		t.Fatal("population moments drift", m, sq)
	}
	if standardizeReflections([]float64{128, 128, 128}) {
		t.Fatal("constant signal fabricated")
	}
}
