package pipeline

import "sort"

// AlignModelYears applies the existing common-country temporal sign convention
// to an additional method, without changing any original spectral model.
func AlignModelYears(models []ModelResult) []bool {
	order := make([]int, len(models))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return models[order[i]].Year < models[order[j]].Year })
	previous := map[string]int{}
	flips := make([]bool, len(models))
	for _, i := range order {
		r := &models[i]
		if r.Result.Status != "ok" {
			continue
		}
		key := r.Scheme + "/" + r.Level + "/" + r.Metric
		if prev, ok := previous[key]; ok {
			probe := 0
			for probe < len(r.Result.ECI) && r.Result.ECI[probe] == 0 {
				probe++
			}
			old := 0.0
			if probe < len(r.Result.ECI) {
				old = r.Result.ECI[probe]
			}
			align(&r.Result, &models[prev].Result)
			flips[i] = probe < len(r.Result.ECI) && old*r.Result.ECI[probe] < 0
		}
		previous[key] = i
	}
	return flips
}
