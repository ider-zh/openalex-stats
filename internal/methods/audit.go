package methods

import (
	"fmt"
	"math"
)

// Independent verification uses the two-step country operator T^k, not the
// production alternating country/discipline loop. It compares scores up to the
// separately audited paired sign convention.
func auditModels(base, derived []Model) (Audit, error) {
	a := Audit{Status: "passed", Models: len(derived), Statuses: map[string]int{}}
	prior := map[string]Model{}
	for index, m := range derived {
		a.Statuses[m.Result.Status]++
		key, e := networkKey(m.Result)
		if e != nil || key != base[index].NetworkSHA256 {
			return a, fmt.Errorf("source network changed")
		}
		a.NetworkChecks++
		if m.Result.Status != "ok" {
			continue
		}
		a.ValidModels++
		r := m.Result
		nc, np := len(r.ActiveCountries), len(r.ActiveDisciplines)
		kc, kp := make([]float64, nc), make([]float64, np)
		for x, i := range r.ActiveCountries {
			for y, j := range r.ActiveDisciplines {
				if r.Advantage[i][j] {
					kc[x]++
					kp[y]++
				}
			}
		}
		t := make([][]float64, nc)
		for x, i := range r.ActiveCountries {
			t[x] = make([]float64, nc)
			for z, k := range r.ActiveCountries {
				for y, j := range r.ActiveDisciplines {
					if r.Advantage[i][j] && r.Advantage[k][j] {
						t[x][z] += 1 / (kc[x] * kp[y])
					}
				}
			}
		}
		c := append([]float64(nil), kc...)
		for step := 0; step < m.ReflectionDiagnostics.Config.CountryIterations/2; step++ {
			next := make([]float64, nc)
			for x := range next {
				for z := range c {
					next[x] += t[x][z] * c[z]
				}
			}
			c = next
		}
		p := make([]float64, np)
		for y, j := range r.ActiveDisciplines {
			for x, i := range r.ActiveCountries {
				if r.Advantage[i][j] {
					p[y] += c[x] / kp[y]
				}
			}
		}
		if !independentZ(c) || !independentZ(p) {
			return a, fmt.Errorf("independent reference lost variance for valid model")
		}
		dot := 0.0
		for x := range c {
			dot += c[x] * r.ECI[x]
		}
		sign := 1.0
		if dot < 0 {
			sign = -1
		}
		precision := math.Min(m.ReflectionDiagnostics.CountrySignalULPs, m.ReflectionDiagnostics.DisciplineSignalULPs)
		tolerance := math.Max(2e-8, 512/precision)
		if tolerance > 1e-4 {
			a.LowPrecisionModels++
		}
		for _, pair := range []struct{ got, want []float64 }{{r.ECI, c}, {r.PCI, p}} {
			if len(pair.got) != len(pair.want) {
				return a, fmt.Errorf("reflection score dimensions changed")
			}
			mean, sq := 0.0, 0.0
			for i, v := range pair.got {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return a, fmt.Errorf("nonfinite reflection score")
				}
				delta := math.Abs(v - sign*pair.want[i])
				a.MaxIndependentScoreDifference = math.Max(a.MaxIndependentScoreDifference, delta)
				if delta > tolerance {
					return a, fmt.Errorf("independent reflection mismatch %g > %g", delta, tolerance)
				}
				mean += v
				sq += v * v
			}
			if math.Abs(mean/float64(len(pair.got))) > 1e-6 || math.Abs(sq/float64(len(pair.got))-1) > 1e-8 {
				return a, fmt.Errorf("invalid population standardization")
			}
		}
		a.IndependentIterationChecks++
		group := m.Scheme + "/" + m.Level + "/" + m.Metric
		if last, ok := prior[group]; ok {
			pairsX, pairsY := []float64{}, []float64{}
			old := map[string]float64{}
			for x, i := range last.Result.ActiveCountries {
				old[last.Result.Countries[i]] = last.Result.ECI[x]
			}
			for x, i := range r.ActiveCountries {
				if v, exists := old[r.Countries[i]]; exists {
					pairsX = append(pairsX, r.ECI[x])
					pairsY = append(pairsY, v)
				}
			}
			if covariance(pairsX, pairsY) < -1e-8 {
				return a, fmt.Errorf("temporal sign not aligned")
			}
		} else {
			if covariance(r.ECI, kc) < -1e-8 {
				return a, fmt.Errorf("first valid year sign not diversity aligned")
			}
		}
		prior[group] = m
		a.SignChecks++
	}
	return a, nil
}

func independentZ(v []float64) bool {
	sum := 0.0
	for _, x := range v {
		sum += x
	}
	mean := sum / float64(len(v))
	norm := 0.0
	for _, x := range v {
		delta := x - mean
		norm += delta * delta
	}
	scale := math.Sqrt(norm / float64(len(v)))
	if scale == 0 || math.IsNaN(scale) {
		return false
	}
	for i := range v {
		v[i] = (v[i] - mean) / scale
	}
	return true
}
func covariance(a, b []float64) float64 {
	if len(a) < 2 || len(a) != len(b) {
		return 0
	}
	am, bm := 0.0, 0.0
	for i := range a {
		am += a[i]
		bm += b[i]
	}
	am /= float64(len(a))
	bm /= float64(len(a))
	v := 0.0
	for i := range a {
		v += (a[i] - am) * (b[i] - bm)
	}
	return v
}
