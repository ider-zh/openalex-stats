// Experimental fork of internal/eci/eci.go. Main data and production code are untouched.
// Package eci computes ECI/PCI from one country-discipline matrix. It does not
// mix taxonomy layers or infer field-specific country ECI.
package diagnostic

import (
	"fmt"
	"math"

	"gonum.org/v1/gonum/mat"
	prod "openalexstats/internal/eci"
)

// RCARoundingTolerance snaps only roundoff-sized deviations from the RCA=1
// boundary. Without this, identical proportional portfolios can acquire false
// advantages solely because fractional sums round to 0.9999999999999998.
const RCARoundingTolerance = 1e-12

func NormalizeRCA(value float64) float64 {
	if math.Abs(value-1) <= RCARoundingTolerance {
		return 1
	}
	return value
}

// Research-only configurable binary network, preserving production eigensolver and guards.
type Matrix = prod.Matrix
type Result = prod.Result
type Component = prod.Component
type Config struct {
	RCAThreshold       float64
	MinimumCellSupport float64
	MinimumDiversity   int
}

func Compute(input Matrix, cfg Config) (Result, error) {
	if cfg.RCAThreshold <= 0 {
		cfg.RCAThreshold = 1
	}
	if cfg.MinimumDiversity < 1 {
		cfg.MinimumDiversity = 1
	}
	n, d := len(input.Countries), len(input.Disciplines)
	out := Result{Status: "empty", Countries: append([]string(nil), input.Countries...), Disciplines: append([]string(nil), input.Disciplines...), RCARoundingTolerance: RCARoundingTolerance}
	if len(input.Values) != n {
		return out, fmt.Errorf("country dimension mismatch")
	}
	if err := validateLabels(input.Countries); err != nil {
		return out, err
	}
	if err := validateLabels(input.Disciplines); err != nil {
		return out, err
	}
	row := make([]float64, n)
	col := make([]float64, d)
	total := 0.0
	for i, v := range input.Values {
		if len(v) != d {
			return out, fmt.Errorf("discipline dimension mismatch at row %d", i)
		}
		for j, x := range v {
			if math.IsNaN(x) || math.IsInf(x, 0) || x < 0 {
				return out, fmt.Errorf("matrix must be finite and nonnegative")
			}
			row[i] += x
			col[j] += x
			total += x
		}
	}
	if math.IsInf(total, 0) {
		return out, fmt.Errorf("matrix sum overflow")
	}
	out.RCA = make([][]float64, n)
	out.Advantage = make([][]bool, n)
	kc := make([]float64, n)
	kd := make([]float64, d)
	for i := 0; i < n; i++ {
		out.RCA[i] = make([]float64, d)
		out.Advantage[i] = make([]bool, d)
		for j := 0; j < d; j++ {
			if row[i] > 0 && col[j] > 0 {
				r := NormalizeRCA((input.Values[i][j] / row[i]) / (col[j] / total))
				out.RCA[i][j] = r
				if r >= cfg.RCAThreshold && input.Values[i][j] >= cfg.MinimumCellSupport {
					out.Advantage[i][j] = true
					kc[i]++
					kd[j]++
				}
			}
		}
	}
	for i := range kc {
		if kc[i] > 0 && kc[i] < float64(cfg.MinimumDiversity) {
			for j, edge := range out.Advantage[i] {
				if edge {
					out.Advantage[i][j] = false
					kd[j]--
				}
			}
			kc[i] = 0
		}
	}
	for i := range kc {
		if kc[i] > 0 {
			out.ActiveCountries = append(out.ActiveCountries, i)
		}
	}
	for j := 0; j < d; j++ {
		if kd[j] > 0 {
			out.ActiveDisciplines = append(out.ActiveDisciplines, j)
		}
	}
	if len(out.ActiveCountries) < 2 || len(out.ActiveDisciplines) < 2 {
		out.Components = components(out.Advantage, out.ActiveCountries, out.ActiveDisciplines)
		out.ComponentCount = len(out.Components)
		for _, c := range out.Components {
			out.LargestComponentCountries = max(out.LargestComponentCountries, len(c.Countries))
		}
		out.Diagnostic = "fewer than two nonzero countries or disciplines"
		return out, nil
	}
	// Every positive row/column must have at least one RCA>=1; test this invariant.
	for _, i := range out.ActiveCountries {
		if kc[i] == 0 {
			return out, fmt.Errorf("nonzero country has no advantage")
		}
	}
	for _, j := range out.ActiveDisciplines {
		if kd[j] == 0 {
			return out, fmt.Errorf("nonzero discipline has no advantage")
		}
	}
	out.Components = components(out.Advantage, out.ActiveCountries, out.ActiveDisciplines)
	out.ComponentCount = len(out.Components)
	for _, c := range out.Components {
		out.LargestComponentCountries = max(out.LargestComponentCountries, len(c.Countries))
	}
	a := len(out.ActiveCountries)
	s := mat.NewSymDense(a, nil)
	for x, i := range out.ActiveCountries {
		for y := 0; y <= x; y++ {
			j := out.ActiveCountries[y]
			v := 0.0
			for _, k := range out.ActiveDisciplines {
				if out.Advantage[i][k] && out.Advantage[j][k] {
					v += 1 / kd[k]
				}
			}
			s.SetSym(x, y, v/math.Sqrt(kc[i]*kc[j]))
		}
	}
	var eig mat.EigenSym
	if !eig.Factorize(s, true) {
		return out, fmt.Errorf("eigendecomposition failed")
	}
	out.Eigenvalues = eig.Values(nil)
	lambda := out.Eigenvalues[a-2]
	out.SecondEigenvalue = lambda
	out.EigenGap = math.Abs(out.Eigenvalues[a-1] - lambda)
	if a > 2 {
		out.EigenGap = math.Min(out.EigenGap, math.Abs(lambda-out.Eigenvalues[a-3]))
	}
	out.Unstable = out.EigenGap < 1e-6
	if out.ComponentCount > 1 {
		out.Status = "disconnected"
		out.Diagnostic = "multiple advantage-network components: no comparable global ECI/PCI scores"
		return out, nil
	}
	if math.Abs(1-lambda) < 1e-10 || (a > 2 && math.Abs(lambda-out.Eigenvalues[a-3]) < 1e-10) {
		out.Status = "degenerate"
		out.Diagnostic = "second eigenvalue is repeated or indistinguishable from a neighbor"
		return out, nil
	}
	var vectors mat.Dense
	eig.VectorsTo(&vectors)
	u := make([]float64, a)
	e := make([]float64, a)
	for x, i := range out.ActiveCountries {
		u[x] = vectors.At(x, a-2)
		e[x] = u[x] / math.Sqrt(kc[i])
	}
	for x := 0; x < a; x++ {
		v := 0.0
		for y := 0; y < a; y++ {
			v += s.At(x, y) * u[y]
		}
		delta := v - lambda*u[x]
		out.Residual += delta * delta
	}
	out.Residual = math.Sqrt(out.Residual)
	// Positive correlation with diversity; deterministic label-order tie-break.
	em, km := 0.0, 0.0
	for x, i := range out.ActiveCountries {
		em += e[x]
		km += kc[i]
	}
	em /= float64(a)
	km /= float64(a)
	corr := 0.0
	for x, i := range out.ActiveCountries {
		corr += (e[x] - em) * (kc[i] - km)
	}
	sign := 1.0
	if corr < 0 {
		sign = -1
	}
	if math.Abs(corr) < 1e-14 {
		for _, v := range e {
			if math.Abs(v) > 1e-14 {
				if v < 0 {
					sign = -1
				}
				break
			}
		}
	}
	for x := range e {
		e[x] *= sign
	}
	p := make([]float64, len(out.ActiveDisciplines))
	for y, j := range out.ActiveDisciplines {
		for x, i := range out.ActiveCountries {
			if out.Advantage[i][j] {
				p[y] += e[x] / kd[j]
			}
		}
	}
	if !standardize(e) || !standardize(p) {
		out.Status = "degenerate"
		out.Diagnostic = "country or discipline score has zero variance"
		return out, nil
	}
	// Scores align with ActiveCountries/ActiveDisciplines; inactive entries have
	// no score, rather than a misleading zero ECI.
	out.ECI = e
	out.PCI = p
	out.Status = "ok"
	out.SignConvention = "positive diversity correlation; input-order tie-break; align across years separately"
	if out.Unstable {
		out.Diagnostic = "small eigengap: complexity ordering may be unstable"
	}
	return out, nil
}

func validateLabels(v []string) error {
	seen := map[string]bool{}
	for _, s := range v {
		if s == "" || s == "UNKNOWN" || seen[s] {
			return fmt.Errorf("labels must be unique, known and nonempty: %q", s)
		}
		seen[s] = true
	}
	return nil
}
func standardize(v []float64) bool {
	m := 0.0
	for _, x := range v {
		m += x
	}
	m /= float64(len(v))
	variance := 0.0
	for _, x := range v {
		variance += (x - m) * (x - m)
	}
	sd := math.Sqrt(variance / float64(len(v)))
	if sd < 1e-14 {
		return false
	}
	for i := range v {
		v[i] = (v[i] - m) / sd
	}
	return true
}
func components(m [][]bool, countries, disciplines []int) []Component {
	seenCountry := map[int]bool{}
	var result []Component
	for _, start := range countries {
		if seenCountry[start] {
			continue
		}
		seenCountry[start] = true
		c := Component{}
		seenDiscipline := map[int]bool{}
		queue := []int{start}
		for len(queue) > 0 {
			i := queue[0]
			queue = queue[1:]
			c.Countries = append(c.Countries, i)
			for _, j := range disciplines {
				if !m[i][j] || seenDiscipline[j] {
					continue
				}
				seenDiscipline[j] = true
				c.Disciplines = append(c.Disciplines, j)
				for _, k := range countries {
					if m[k][j] && !seenCountry[k] {
						seenCountry[k] = true
						queue = append(queue, k)
					}
				}
			}
		}
		result = append(result, c)
	}
	return result
}
