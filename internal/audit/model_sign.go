package audit

import (
	"fmt"
	"math"
)

// auditOrientations uses independent covariance and stationary-weight recovery,
// not the production sign/align functions. Jointly flipping both axes preserves
// a model's eigen equations but must not bypass the series' orientation rule.
func auditOrientations(models []publishedModel, checks []ModelCheck) {
	previous := map[string]int{}
	for i, model := range models {
		if model.Result.Status != "ok" || len(model.Result.ECI) == 0 {
			checks[i].OrientationRule = "not_applicable"
			continue
		}
		series := model.Scheme + "/" + model.Level + "/" + model.Metric
		prior, exists := previous[series]
		if !exists {
			checkFirstOrientation(model, &checks[i], "first_valid_diversity_or_label_tie")
		} else {
			old := models[prior].Result
			priorScores := map[string]float64{}
			for k, ordinal := range old.ActiveCountries {
				if ordinal >= 0 && ordinal < len(old.Countries) && k < len(old.ECI) {
					priorScores[old.Countries[ordinal]] = old.ECI[k]
				}
			}
			var current, priorValues []float64
			for k, ordinal := range model.Result.ActiveCountries {
				if ordinal < 0 || ordinal >= len(model.Result.Countries) || k >= len(model.Result.ECI) {
					continue
				}
				if value, ok := priorScores[model.Result.Countries[ordinal]]; ok {
					current = append(current, model.Result.ECI[k])
					priorValues = append(priorValues, value)
				}
			}
			checks[i].CommonCountries = len(current)
			if len(current) < 2 {
				checks[i].OrientationFallback = "fewer than two common active countries"
				checkFirstOrientation(model, &checks[i], "fallback_diversity_or_label_tie")
			} else {
				mx, vx := populationMoments(current)
				my, vy := populationMoments(priorValues)
				if vx <= 1e-24 || vy <= 1e-24 {
					checks[i].OrientationFallback = "zero variance among common active country scores"
					checkFirstOrientation(model, &checks[i], "fallback_diversity_or_label_tie")
				} else {
					cov := 0.0
					for k, v := range current {
						cov += (v - mx) * (priorValues[k] - my)
					}
					checks[i].OrientationRule = "common_country_temporal_covariance"
					checks[i].TemporalCovariance = cov
					tol := 1e-12 + 1e-12*float64(len(current))*math.Sqrt(vx*vy)
					checks[i].truth("temporal_sign_covariance_nonnegative", cov >= -tol)
				}
			}
		}
		checks[i].finish()
		previous[series] = i
	}
}

func checkFirstOrientation(model publishedModel, check *ModelCheck, rule string) {
	check.OrientationRule = rule
	score := model.Result.ECI
	degrees := check.diversity
	if len(score) != len(degrees) || len(score) == 0 {
		check.truth("orientation_degree_shape", false)
		return
	}
	degreeMean := 0.0
	degreeTotal := 0.0
	weightedScore := 0.0
	for i, k := range degrees {
		degreeMean += k
		degreeTotal += k
		weightedScore += k * score[i]
	}
	degreeMean /= float64(len(degrees))
	if degreeTotal <= 0 {
		check.truth("orientation_positive_degree", false)
		return
	}
	weightedMean := weightedScore / degreeTotal
	// The unstandardized right eigenvector is stationary-mean-zero and obeys
	// sum(kc*e^2)=1. Recover its positive normalization scale from standardized
	// ECI so the documented raw-eigenvector zero-covariance tie is testable.
	weightedNorm := 0.0
	covariance := 0.0
	for i, value := range score {
		weightedNorm += degrees[i] * (value - weightedMean) * (value - weightedMean)
		covariance += value * (degrees[i] - degreeMean)
	}
	if weightedNorm <= 0 || math.IsInf(weightedNorm, 0) || math.IsNaN(weightedNorm) {
		check.truth("orientation_stationary_scale", false)
		return
	}
	scale := 1 / math.Sqrt(weightedNorm)
	rawCovariance := scale * covariance
	check.DiversityCovariance = rawCovariance
	if math.Abs(rawCovariance) >= 1e-14 {
		check.truth("initial_diversity_covariance_positive", rawCovariance > 0)
		return
	}
	check.OrientationFallback = joinFallback(check.OrientationFallback, "diversity covariance is numerically zero; use first nonzero raw-coordinate label tie")
	for _, value := range score {
		raw := scale * (value - weightedMean)
		if math.Abs(raw) > 1e-14 {
			check.truth("initial_zero_covariance_label_tie", raw > 0)
			return
		}
	}
	check.truth("initial_nonzero_coordinate_exists", false)
}
func joinFallback(a, b string) string {
	if a == "" {
		return b
	}
	return fmt.Sprintf("%s; %s", a, b)
}
