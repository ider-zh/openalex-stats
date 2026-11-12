package methods

import (
	"math"
	"testing"
)

func TestIndependentZTranslationAndScaleInvarianceNearConstant(t *testing.T) {
	base := []float64{0, 1, 2, 2, 3, 5, 8, 13, 21, 21, 34, 55, 89}
	shifted := make([]float64, len(base))
	for i, x := range base {
		shifted[i] = 128 + math.Ldexp(x, -36)
	}
	if !independentZ(base) || !independentZ(shifted) {
		t.Fatal("nonconstant representable signal lost")
	}
	for i := range base {
		if math.Abs(base[i]-shifted[i]) > 1e-13 {
			t.Fatal("independent reference is not affine invariant", i, base[i], shifted[i])
		}
	}
}
