package eci

import (
	"fmt"
	"math"
)

const ReflectionsMethodID = "java-reflections-v1"

type ReflectionConfig struct {
	CountryIterations    int `json:"country_iterations"`
	DisciplineIterations int `json:"discipline_iterations"`
}

func DefaultReflectionConfig() ReflectionConfig { return ReflectionConfig{16, 17} }

type ReflectionDiagnostics struct {
	Config                  ReflectionConfig `json:"config"`
	CountryRawSD            float64          `json:"country_raw_sd"`
	DisciplineRawSD         float64          `json:"discipline_raw_sd"`
	CountrySignalULPs       float64          `json:"country_signal_ulps"`
	DisciplineSignalULPs    float64          `json:"discipline_signal_ulps"`
	CountryTwoStepMaxChange float64          `json:"country_two_step_max_change"`
	ResidualKind            string           `json:"residual_kind"`
}

// ComputeReflections keeps the finite Method of Reflections, initialized with
// binary-network degrees. Spectral scores are never used as reflection input.
func ComputeReflections(input Matrix, cfg ReflectionConfig) (Result, ReflectionDiagnostics, error) {
	prepared, err := Compute(input)
	if err != nil {
		return Result{}, ReflectionDiagnostics{}, err
	}
	return ReflectionsFromPrepared(prepared, cfg)
}

// ReflectionsFromPrepared uses the exact RCA/network/active universe of an
// already audited model. Shared spectral diagnostics assess global network
// identifiability; the returned scores are finite raw reflection iterates.
func ReflectionsFromPrepared(base Result, cfg ReflectionConfig) (Result, ReflectionDiagnostics, error) {
	out := base
	out.ECI, out.PCI = nil, nil
	out.Residual = 0
	diag := ReflectionDiagnostics{Config: cfg, ResidualKind: "relative centered(T*z)-lambda2*z; finite-iteration diagnostic, not a convergence requirement"}
	if cfg.CountryIterations < 2 || cfg.CountryIterations > 100 || cfg.CountryIterations%2 != 0 || cfg.DisciplineIterations != cfg.CountryIterations+1 {
		return out, diag, fmt.Errorf("reflections require even country iterations in [2,100] and discipline iterations=country+1")
	}
	if err := validateLabels(base.Countries); err != nil {
		return out, diag, err
	}
	if err := validateLabels(base.Disciplines); err != nil {
		return out, diag, err
	}
	n, d := len(base.Countries), len(base.Disciplines)
	if len(base.Advantage) != n {
		return out, diag, fmt.Errorf("reflection network country dimension mismatch")
	}
	kc, kp := make([]float64, n), make([]float64, d)
	for i, row := range base.Advantage {
		if len(row) != d {
			return out, diag, fmt.Errorf("reflection network discipline dimension mismatch")
		}
		for j, edge := range row {
			if edge {
				kc[i]++
				kp[j]++
			}
		}
	}
	seenC, seenP := make(map[int]bool), make(map[int]bool)
	for _, i := range base.ActiveCountries {
		if i < 0 || i >= n || seenC[i] || kc[i] == 0 {
			return out, diag, fmt.Errorf("invalid active reflection country")
		}
		seenC[i] = true
	}
	for _, j := range base.ActiveDisciplines {
		if j < 0 || j >= d || seenP[j] || kp[j] == 0 {
			return out, diag, fmt.Errorf("invalid active reflection discipline")
		}
		seenP[j] = true
	}
	for i, degree := range kc {
		if degree > 0 && !seenC[i] {
			return out, diag, fmt.Errorf("reflection country omitted from active universe")
		}
	}
	for j, degree := range kp {
		if degree > 0 && !seenP[j] {
			return out, diag, fmt.Errorf("reflection discipline omitted from active universe")
		}
	}
	if base.Status != "ok" {
		return out, diag, nil
	}
	if len(base.ActiveCountries) < 2 || len(base.ActiveDisciplines) < 2 || len(components(base.Advantage, base.ActiveCountries, base.ActiveDisciplines)) != 1 {
		return out, diag, fmt.Errorf("ok reference has an invalid global reflection network")
	}
	c, p := append([]float64(nil), kc...), append([]float64(nil), kp...)
	var targetC, priorC []float64
	if cfg.CountryIterations == 2 {
		priorC = append([]float64(nil), c...)
	}
	for iteration := 1; iteration <= cfg.DisciplineIterations; iteration++ {
		nc, np := make([]float64, n), make([]float64, d)
		for _, i := range base.ActiveCountries {
			for _, j := range base.ActiveDisciplines {
				if base.Advantage[i][j] {
					nc[i] += p[j]
					np[j] += c[i]
				}
			}
			nc[i] /= kc[i]
		}
		for _, j := range base.ActiveDisciplines {
			np[j] /= kp[j]
		}
		c, p = nc, np
		if iteration == cfg.CountryIterations-2 {
			priorC = append([]float64(nil), c...)
		}
		if iteration == cfg.CountryIterations {
			targetC = append([]float64(nil), c...)
		}
	}
	e, q := make([]float64, len(base.ActiveCountries)), make([]float64, len(base.ActiveDisciplines))
	for x, i := range base.ActiveCountries {
		e[x] = targetC[i]
		if priorC != nil {
			diag.CountryTwoStepMaxChange = math.Max(diag.CountryTwoStepMaxChange, math.Abs(targetC[i]-priorC[i]))
		}
	}
	for y, j := range base.ActiveDisciplines {
		q[y] = p[j]
	}
	diag.CountryRawSD, diag.CountrySignalULPs = reflectionSignal(e)
	diag.DisciplineRawSD, diag.DisciplineSignalULPs = reflectionSignal(q)
	// Do not turn floating-point noise near a constant into an artificial rank.
	if diag.CountrySignalULPs < 32 || diag.DisciplineSignalULPs < 32 || !standardizeReflections(e) || !standardizeReflections(q) {
		out.Status = "degenerate"
		out.Diagnostic = "finite reflection iterate has zero variance or insufficient floating-point signal; no initial-degree fallback"
		return out, diag, nil
	}
	meanE, meanK := 0.0, 0.0
	for x, i := range base.ActiveCountries {
		meanE += e[x]
		meanK += kc[i]
	}
	meanE /= float64(len(e))
	meanK /= float64(len(e))
	cov := 0.0
	for x, i := range base.ActiveCountries {
		cov += (e[x] - meanE) * (kc[i] - meanK)
	}
	sign := 1.0
	if cov < 0 {
		sign = -1
	}
	if math.Abs(cov) < 1e-14 {
		for _, v := range e {
			if math.Abs(v) > 1e-14 {
				if v < 0 {
					sign = -1
				}
				break
			}
		}
	}
	for i := range e {
		e[i] *= sign
	}
	for j := range q {
		q[j] *= sign
	}
	out.ECI, out.PCI = e, q
	out.SignConvention = "finite reflections: positive diversity correlation; centered input-order tie-break; align across years separately"
	out.Residual = reflectionResidual(out, kc, kp)
	return out, diag, nil
}

func reflectionSignal(v []float64) (sd, ulps float64) {
	if len(v) == 0 {
		return 0, 0
	}
	scale := 0.0
	for _, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0, 0
		}
		scale = math.Max(scale, math.Abs(x))
	}
	_, _, sd = reflectionMoments(v)
	resolution := math.Nextafter(scale, math.Inf(1)) - scale
	if resolution <= 0 || math.IsInf(resolution, 0) {
		return sd, 0
	}
	return sd, sd / resolution
}

// Finite reflection iterates approach a nonzero constant. Compute their
// population moments after subtracting an observed origin, so rounding a large
// mean cannot leave a nonzero mean in the standardized small residual signal.
// This is the same population z-score, not a different reflection model.
func reflectionMoments(v []float64) (origin, meanOffset, sd float64) {
	if len(v) == 0 {
		return 0, 0, 0
	}
	origin = v[0]
	sum, correction := 0.0, 0.0
	for _, x := range v {
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return origin, 0, math.NaN()
		}
		y := (x - origin) - correction
		t := sum + y
		correction = (t - sum) - y
		sum = t
	}
	meanOffset = sum / float64(len(v))
	variance := 0.0
	for _, x := range v {
		delta := (x - origin) - meanOffset
		variance += delta * delta
	}
	return origin, meanOffset, math.Sqrt(variance / float64(len(v)))
}

func standardizeReflections(v []float64) bool {
	origin, meanOffset, sd := reflectionMoments(v)
	if sd < 1e-14 || math.IsNaN(sd) || math.IsInf(sd, 0) {
		return false
	}
	for i, x := range v {
		v[i] = ((x - origin) - meanOffset) / sd
	}
	return true
}

func reflectionResidual(r Result, kc, kp []float64) float64 {
	p := make([]float64, len(r.Disciplines))
	for x, i := range r.ActiveCountries {
		for _, j := range r.ActiveDisciplines {
			if r.Advantage[i][j] {
				p[j] += r.ECI[x] / kp[j]
			}
		}
	}
	c := make([]float64, len(r.ECI))
	mean := 0.0
	for x, i := range r.ActiveCountries {
		for _, j := range r.ActiveDisciplines {
			if r.Advantage[i][j] {
				c[x] += p[j] / kc[i]
			}
		}
		mean += c[x]
	}
	mean /= float64(len(c))
	residual, norm := 0.0, 0.0
	for x, v := range c {
		delta := v - mean - r.SecondEigenvalue*r.ECI[x]
		residual += delta * delta
		norm += r.ECI[x] * r.ECI[x]
	}
	return math.Sqrt(residual / norm)
}
