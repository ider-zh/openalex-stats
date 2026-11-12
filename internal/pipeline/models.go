package pipeline

import (
	"context"
	"sort"
	"sync"

	"openalexstats/internal/aggregate"
	"openalexstats/internal/eci"
	"openalexstats/internal/results"
)

type ModelResult struct {
	Year   int        `json:"year"`
	Scheme string     `json:"scheme"`
	Level  string     `json:"level"`
	Metric string     `json:"metric"`
	Result eci.Result `json:"result"`
}
type layer struct{ scheme, level string }
type groupKey struct {
	year  int
	layer layer
}

var layers = []layer{{"topics", "field"}, {"topics", "subfield"}, {"concepts", "0"}, {"concepts", "1"}}

func computeModels(ctx context.Context, rows []aggregate.Row, workers, startYear, endYear int) ([]ModelResult, error) {
	return ComputeModels(ctx, rows, workers, startYear, endYear, nil)
}

// ComputeModels optionally uses the snapshot's fixed, complete taxonomy and
// country dictionary rather than inferring a different label universe by year.
func ComputeModels(ctx context.Context, rows []aggregate.Row, workers, startYear, endYear int, dictionary *results.Dictionary) ([]ModelResult, error) {
	countrySet := map[string]bool{}
	disciplineSets := map[layer]map[string]bool{}
	groups := map[groupKey][]aggregate.Row{}
	for _, l := range layers {
		disciplineSets[l] = map[string]bool{}
	}
	for _, r := range rows {
		if r.Country != aggregate.Unknown {
			countrySet[r.Country] = true
		}
		l := layer{r.Scheme, r.Level}
		if r.DisciplineID != aggregate.Unknown {
			disciplineSets[l][r.DisciplineID] = true
		}
		if r.Country == aggregate.Unknown || r.DisciplineID == aggregate.Unknown {
			continue
		}
		k := groupKey{r.Year, l}
		groups[k] = append(groups[k], r)
	}
	countries := sortedKeys(countrySet)
	countryIndex := map[string]int{}
	for i, c := range countries {
		countryIndex[c] = i
	}
	disciplines := map[layer][]string{}
	disciplineIndex := map[layer]map[string]int{}
	for _, l := range layers {
		disciplines[l] = sortedKeys(disciplineSets[l])
		disciplineIndex[l] = map[string]int{}
		for j, d := range disciplines[l] {
			disciplineIndex[l][d] = j
		}
	}
	var results []ModelResult
	for y := startYear; y <= endYear; y++ {
		for _, l := range layers {
			for _, metric := range []string{"publications", "citations"} {
				results = append(results, ModelResult{Year: y, Scheme: l.scheme, Level: l.level, Metric: metric})
			}
		}
	}
	jobs := make(chan int)
	errs := make([]error, len(results))
	var wg sync.WaitGroup
	for w := 0; w < min(workers, max(1, len(results))); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					errs[i] = ctx.Err()
					continue
				}
				r := &results[i]
				l := layer{r.Scheme, r.Level}
				if dictionary != nil {
					m, e := dictionary.Matrix(groups[groupKey{r.Year, l}], r.Scheme, r.Level, r.Year, r.Metric)
					if e != nil {
						errs[i] = e
						continue
					}
					r.Result, errs[i] = eci.Compute(m)
					continue
				}
				m := eci.Matrix{Countries: countries, Disciplines: disciplines[l], Values: make([][]float64, len(countries))}
				for c := range m.Values {
					m.Values[c] = make([]float64, len(m.Disciplines))
				}
				for _, row := range groups[groupKey{r.Year, l}] {
					value := row.Publications
					if r.Metric == "citations" {
						value = row.Citations
					}
					m.Values[countryIndex[row.Country]][disciplineIndex[l][row.DisciplineID]] += value
				}
				r.Result, errs[i] = eci.Compute(m)
			}
		}()
	}
	for i := range results {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return nil, e
		}
	}
	previous := map[string]int{}
	for i := range results {
		r := &results[i]
		if r.Result.Status != "ok" {
			continue
		}
		key := r.Scheme + "/" + r.Level + "/" + r.Metric
		if prev, ok := previous[key]; ok {
			align(&r.Result, &results[prev].Result)
		}
		previous[key] = i
	}
	return results, nil
}
func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for v := range values {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
func align(current, previous *eci.Result) {
	prior := map[string]float64{}
	for i, c := range previous.ActiveCountries {
		prior[previous.Countries[c]] = previous.ECI[i]
	}
	type pair struct{ x, y float64 }
	var pairs []pair
	mx, my := 0.0, 0.0
	for i, c := range current.ActiveCountries {
		if old, ok := prior[current.Countries[c]]; ok {
			pairs = append(pairs, pair{current.ECI[i], old})
			mx += current.ECI[i]
			my += old
		}
	}
	if len(pairs) < 2 {
		return
	}
	mx /= float64(len(pairs))
	my /= float64(len(pairs))
	cov := 0.0
	for _, p := range pairs {
		cov += (p.x - mx) * (p.y - my)
	}
	if cov < 0 {
		for i := range current.ECI {
			current.ECI[i] *= -1
		}
		for i := range current.PCI {
			current.PCI[i] *= -1
		}
	}
	current.SignConvention = "first valid year: positive diversity correlation; subsequent years: align on common countries"
}
