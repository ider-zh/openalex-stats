package main

import (
	"fmt"
	"math"
	"sort"

	"openalexstats/internal/pipeline"
)

const directionPolicy = "baseline-pooled-common-year-sign-v1"

type directionRecord struct {
	Method                 string   `json:"method"`
	Scheme                 string   `json:"scheme"`
	Level                  string   `json:"level"`
	Metric                 string   `json:"metric"`
	YearRange              []int    `json:"year_range"`
	AppliedAllYears        bool     `json:"applied_all_years"`
	CommonPairN            int      `json:"common_pair_n"`
	CommonYears            []int    `json:"common_years"`
	RawPooledRho           *float64 `json:"raw_pooled_rho"`
	AlignedPooledRho       *float64 `json:"aligned_pooled_rho"`
	Factor                 int      `json:"factor"`
	WeakReference          bool     `json:"weak_reference"`
	Unavailable            bool     `json:"unavailable"`
	UnavailableReason      string   `json:"unavailable_reason,omitempty"`
	PooledCovarianceSum    float64  `json:"pooled_covariance_sum"`
	PooledCurrentVariance  float64  `json:"pooled_current_variance_sum"`
	PooledBaselineVariance float64  `json:"pooled_baseline_variance_sum"`
	BaselineModelsSHA256   string   `json:"baseline_models_sha256"`
}

type directionAudit struct {
	Policy               string            `json:"direction_policy"`
	ModelSource          string            `json:"model_source"`
	BaselineModelsSHA256 map[string]string `json:"baseline_models_sha256"`
	Chains               []directionRecord `json:"chains"`
}

type compensatedSum struct{ sum, correction float64 }

func (s *compensatedSum) add(x float64) {
	y := x - s.correction
	t := s.sum + y
	s.correction = (t - s.sum) - y
	s.sum = t
}

// alignToBaseline pools within-year centered common-country moments across
// every complete year2000..2025. Sorted years/IDs and compensated sums keep weak
// reference signs deterministic. One factor changes a whole27-year chain and
// both ECI/PCI together; no per-country, per-year or rank-based fitting occurs.
func alignToBaseline(models []pipeline.ModelResult, baseline []pipeline.ModelResult, method, baselineSHA string) []directionRecord {
	prior := map[string]pipeline.ModelResult{}
	for _, m := range baseline {
		prior[modelKey(m.Year, m.Scheme, m.Level, m.Metric)] = m
	}
	groups := map[string][]int{}
	for i, m := range models {
		key := m.Scheme + "/" + m.Level + "/" + m.Metric
		groups[key] = append(groups[key], i)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var audit []directionRecord
	for _, key := range keys {
		indices := groups[key]
		sort.SliceStable(indices, func(i, j int) bool { return models[indices[i]].Year < models[indices[j]].Year })
		first := models[indices[0]]
		r := directionRecord{Method: method, Scheme: first.Scheme, Level: first.Level, Metric: first.Metric, YearRange: []int{2000, 2025}, AppliedAllYears: true, Factor: 1, CommonYears: []int{}, BaselineModelsSHA256: baselineSHA}
		var covariance, currentVariance, baselineVariance compensatedSum
		for _, index := range indices {
			m := models[index]
			if m.Year < 2000 || m.Year > 2025 || m.Result.Status != "ok" {
				continue
			}
			b, ok := prior[modelKey(m.Year, m.Scheme, m.Level, m.Metric)]
			if !ok || b.Result.Status != "ok" {
				continue
			}
			old := map[string]float64{}
			for position, country := range b.Result.ActiveCountries {
				old[b.Result.Countries[country]] = b.Result.ECI[position]
			}
			current := map[string]float64{}
			for position, country := range m.Result.ActiveCountries {
				id := m.Result.Countries[country]
				if _, ok := old[id]; ok {
					current[id] = m.Result.ECI[position]
				}
			}
			ids := make([]string, 0, len(current))
			for id := range current {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			if len(ids) < 2 {
				continue
			}
			var sx, sy compensatedSum
			for _, id := range ids {
				sx.add(current[id])
				sy.add(old[id])
			}
			mx, my := sx.sum/float64(len(ids)), sy.sum/float64(len(ids))
			for _, id := range ids {
				dx, dy := current[id]-mx, old[id]-my
				covariance.add(dx * dy)
				currentVariance.add(dx * dx)
				baselineVariance.add(dy * dy)
			}
			r.CommonPairN += len(ids)
			r.CommonYears = append(r.CommonYears, m.Year)
		}
		r.PooledCovarianceSum, r.PooledCurrentVariance, r.PooledBaselineVariance = covariance.sum, currentVariance.sum, baselineVariance.sum
		if len(r.CommonYears) == 0 || currentVariance.sum <= 0 || baselineVariance.sum <= 0 {
			r.Unavailable = true
			r.UnavailableReason = "no informative common valid complete-year country pairs or zero pooled variance"
		} else {
			rho := covariance.sum / math.Sqrt(currentVariance.sum*baselineVariance.sum)
			if rho > 1 && rho < 1+1e-12 {
				rho = 1
			}
			if rho < -1 && rho > -1-1e-12 {
				rho = -1
			}
			r.RawPooledRho = &rho
			r.WeakReference = math.Abs(rho) < .2
			if covariance.sum < 0 {
				r.Factor = -1
			}
			aligned := rho * float64(r.Factor)
			r.AlignedPooledRho = &aligned
		}
		for _, index := range indices {
			m := &models[index]
			for i := range m.Result.ECI {
				m.Result.ECI[i] *= float64(r.Factor)
			}
			for i := range m.Result.PCI {
				m.Result.PCI[i] *= float64(r.Factor)
			}
			m.Result.SignConvention += fmt.Sprintf("; %s: complete-year2000-2025 pooled common-country factor%d applied to entire chain ECI/PCI", directionPolicy, r.Factor)
		}
		audit = append(audit, r)
	}
	return audit
}
