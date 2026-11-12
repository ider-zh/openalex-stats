package audit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/parquet-go/parquet-go"
	"gonum.org/v1/gonum/mat"
	"openalexstats/internal/aggregate"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/eci"
)

// The independent audit deliberately uses a general nonsymmetric eigensolver
// on T=Dc^-1 M Dd^-1 M^T. It never calls the production Compute/Matrix helpers.
const modelBoundaryTolerance = 1e-12
const modelNumericalTolerance = 1e-8

type ModelCheck struct {
	Year                int       `json:"year"`
	Scheme              string    `json:"scheme"`
	Level               string    `json:"level"`
	Metric              string    `json:"metric"`
	ReportedStatus      string    `json:"reported_status"`
	IndependentStatus   string    `json:"independent_status"`
	Passed              bool      `json:"passed"`
	Checks              []Check   `json:"checks"`
	MaxRCAError         float64   `json:"max_rca_error"`
	AdvantageMismatches int       `json:"advantage_mismatches"`
	OperatorResidual    float64   `json:"centered_nonsymmetric_operator_residual"`
	PCIMaxError         float64   `json:"pci_projection_max_error"`
	ECIMean             float64   `json:"eci_mean"`
	ECIVariance         float64   `json:"eci_population_variance"`
	PCIMean             float64   `json:"pci_mean"`
	PCIVariance         float64   `json:"pci_population_variance"`
	SecondEigenvalue    float64   `json:"independent_second_eigenvalue"`
	EigenGap            float64   `json:"independent_eigen_gap"`
	Eigenvalues         []float64 `json:"independent_nonsymmetric_eigenvalues_descending,omitempty"`
	ComponentCount      int       `json:"independent_component_count"`
	ScoreChecksApplied  bool      `json:"score_checks_applied"`
	OrientationRule     string    `json:"orientation_rule"`
	OrientationFallback string    `json:"orientation_fallback,omitempty"`
	CommonCountries     int       `json:"common_countries"`
	TemporalCovariance  float64   `json:"temporal_covariance"`
	DiversityCovariance float64   `json:"recovered_raw_diversity_covariance"`
	diversity           []float64
}
type ModelAuditReport struct {
	ScenarioID          string        `json:"scenario_id"`
	Version             string        `json:"version"`
	Status              string        `json:"status"`
	Passed              bool          `json:"passed"`
	StartedAt           time.Time     `json:"started_at"`
	CompletedAt         time.Time     `json:"completed_at"`
	ExpectedModels      int           `json:"expected_models"`
	Models              []ModelCheck  `json:"models"`
	FailedModels        int           `json:"failed_models"`
	Exports             []ExportCheck `json:"export_checks"`
	FailedExports       int           `json:"failed_exports"`
	UnknownRowsExcluded uint64        `json:"unknown_rows_excluded"`
	Sources             []FileHash    `json:"sources"`
	Failure             string        `json:"failure,omitempty"`
	Notes               []string      `json:"notes"`
}
type auditLabel struct {
	ID string `json:"id"`
}
type auditDictionary struct {
	Countries []auditLabel `json:"countries"`
	Layers    []struct {
		Scheme string       `json:"scheme"`
		Level  string       `json:"level"`
		Labels []auditLabel `json:"labels"`
	} `json:"layers"`
}
type publishedModel struct {
	Year       int        `json:"year"`
	Scheme     string     `json:"scheme"`
	Level      string     `json:"level"`
	Metric     string     `json:"metric"`
	ScenarioID string     `json:"scenario_id,omitempty"`
	Result     eci.Result `json:"result"`
}
type matrixKey struct {
	year          int
	scheme, level string
}
type auditMatrix struct {
	countries, disciplines []string
	values                 []float64
}
type twoMatrices struct{ publications, citations auditMatrix }
type independentState struct {
	rca                                [][]float64
	advantage                          [][]bool
	activeCountries, activeDisciplines []int
	diversity, ubiquity                []float64
	components                         []eci.Component
	operator                           *mat.Dense
	eigenvalues                        []float64
	lambda, gap                        float64
	status                             string
}

// AuditModels is a standalone read-only numerical audit of all 216 published
// main-scenario models. Every matrix is rebuilt from cube.parquet plus the fixed
// dictionary; UNKNOWN is excluded. A score disagreement returns a populated
// failed report and error, and always attempts to persist outputFile.
func AuditModels(ctx context.Context, resultDir, outputFile string) (report ModelAuditReport, err error) {
	report = ModelAuditReport{Version: "independent-model-audit-v2", Status: "running", StartedAt: time.Now().UTC(), ExpectedModels: 216, Models: []ModelCheck{}, Notes: []string{
		"X reconstructed from the exported actual main scenario cube; known country/discipline margins only, UNKNOWN excluded.",
		"Independent RCA boundary rule snaps |RCA-1|<=1e-12 to 1; active indices and M are compared explicitly.",
		"General mat.Eigen solves the nonsymmetric row-stochastic country operator T; EigenSym is not used here.",
		"Valid scores must have zero mean/unit population variance and satisfy center(T*ECI)=lambda2*ECI and PCI=standardize(Dd^-1*M^T*ECI). The eigen equations permit a joint sign flip, but independent first-year/temporal orientation checks constrain the published series.",
		"First valid year uses positive diversity covariance or its raw-coordinate label tie; later valid years require nonnegative common-country covariance, with explicitly audited diversity/tie fallback if common-country count or variance is insufficient.",
		"country_eci.parquet and discipline_pci.parquet are independently matched to every JSON model label/active/valid/status/score; missing, extra, duplicate and mismatched exported records fail.",
		"Empty, disconnected and degenerate models must not publish invented scores; all 216 annual/layer/metric slots are mandatory.",
	}}
	defer func() {
		report.CompletedAt = time.Now().UTC()
		if err != nil {
			report.Status, report.Passed, report.Failure = "failed", false, err.Error()
		}
		if outputFile != "" {
			if e := checkpoint.JSON(outputFile, report); e != nil && err == nil {
				err = e
				report.Status, report.Passed, report.Failure = "failed", false, e.Error()
			}
		}
	}()
	if ctx == nil {
		return report, errors.New("model audit: nil context")
	}
	mainScenario, hasScenarios, e := readMainAuditScenario(resultDir)
	if e != nil {
		return report, e
	}
	report.ScenarioID = mainScenario.ID
	var dictionary auditDictionary
	if err = readJSON(filepath.Join(resultDir, "dictionary.json"), &dictionary); err != nil {
		return report, err
	}
	var models []publishedModel
	if err = readJSON(filepath.Join(resultDir, "eci.json"), &models); err != nil {
		return report, err
	}
	for _, name := range []string{"cube.parquet", "eci.json", "dictionary.json", "country_eci.parquet", "discipline_pci.parquet"} {
		h, e := hashFile(ctx, filepath.Join(resultDir, name))
		if e != nil {
			return report, e
		}
		report.Sources = append(report.Sources, h)
	}
	if hasScenarios {
		h, e := hashFile(ctx, filepath.Join(resultDir, "scenarios.json"))
		if e != nil {
			return report, e
		}
		report.Sources = append(report.Sources, h)
	}
	countries := make([]string, len(dictionary.Countries))
	ci := map[string]int{}
	for i, c := range dictionary.Countries {
		if c.ID == "" || c.ID == aggregate.Unknown {
			return report, errors.New("model audit: invalid dictionary country")
		}
		if _, ok := ci[c.ID]; ok {
			return report, errors.New("model audit: duplicate dictionary country")
		}
		ci[c.ID] = i
		countries[i] = c.ID
	}
	layerLabels := map[string][]string{}
	di := map[string]map[string]int{}
	for _, layer := range dictionary.Layers {
		key := layer.Scheme + "/" + layer.Level
		if !isAuditLayer(layer.Scheme, layer.Level) {
			return report, errors.New("model audit: unexpected dictionary layer")
		}
		if _, ok := layerLabels[key]; ok {
			return report, errors.New("model audit: duplicate dictionary layer")
		}
		labels := make([]string, len(layer.Labels))
		index := map[string]int{}
		for i, label := range layer.Labels {
			if label.ID == "" || label.ID == aggregate.Unknown {
				return report, errors.New("model audit: invalid dictionary discipline")
			}
			if _, ok := index[label.ID]; ok {
				return report, errors.New("model audit: duplicate dictionary discipline")
			}
			labels[i] = label.ID
			index[label.ID] = i
		}
		layerLabels[key], di[key] = labels, index
	}
	if len(layerLabels) != 4 {
		return report, errors.New("model audit: dictionary must have four fixed taxonomy layers")
	}
	matrices := map[matrixKey]*twoMatrices{}
	for year := 2000; year <= 2026; year++ {
		for _, layer := range auditLayers {
			labels := layerLabels[layer[0]+"/"+layer[1]]
			n := len(countries) * len(labels)
			matrices[matrixKey{year, layer[0], layer[1]}] = &twoMatrices{auditMatrix{countries, labels, make([]float64, n)}, auditMatrix{countries, labels, make([]float64, n)}}
		}
	}
	err = readModelCube(ctx, filepath.Join(resultDir, "cube.parquet"), func(row aggregate.Row) error {
		if row.Year < 2000 || row.Year > 2026 || !isAuditLayer(row.Scheme, row.Level) || !finiteModelValue(row.Publications) || !finiteModelValue(row.Citations) {
			return errors.New("model audit: invalid main cube row")
		}
		if row.Country == aggregate.Unknown || row.DisciplineID == aggregate.Unknown {
			report.UnknownRowsExcluded++
			return nil
		}
		i, ok := ci[row.Country]
		if !ok {
			return errors.New("model audit: cube country outside fixed dictionary")
		}
		j, ok := di[row.Scheme+"/"+row.Level][row.DisciplineID]
		if !ok {
			return errors.New("model audit: cube discipline outside fixed dictionary")
		}
		m := matrices[matrixKey{row.Year, row.Scheme, row.Level}]
		p := i*len(m.publications.disciplines) + j
		m.publications.values[p] += row.Publications
		m.citations.values[p] += row.Citations
		return nil
	})
	if err != nil {
		return report, err
	}
	byKey := map[string]publishedModel{}
	for _, model := range models {
		if model.ScenarioID == "" && mainScenario.ID == legacyMainScenario {
			model.ScenarioID = mainScenario.ID
		}
		if model.ScenarioID != mainScenario.ID {
			return report, errors.New("model audit: unsupported model scenario")
		}
		key := modelID(model.Year, model.Scheme, model.Level, model.Metric)
		if _, ok := byKey[key]; ok {
			return report, errors.New("model audit: duplicate model slot")
		}
		byKey[key] = model
	}
	ordered := []publishedModel{}
	for year := 2000; year <= 2026; year++ {
		for _, layer := range auditLayers {
			for _, metric := range []string{"publications", "citations"} {
				key := modelID(year, layer[0], layer[1], metric)
				model, ok := byKey[key]
				if !ok {
					return report, fmt.Errorf("model audit: missing model %s", key)
				}
				ordered = append(ordered, model)
				delete(byKey, key)
			}
		}
	}
	if len(byKey) != 0 {
		return report, errors.New("model audit: unexpected model slots")
	}
	report.Models = make([]ModelCheck, len(ordered))
	errs := make([]error, len(ordered))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(ordered)) {
		wg.Go(func() {
			for i := range jobs {
				if ctx.Err() != nil {
					errs[i] = ctx.Err()
					continue
				}
				model := ordered[i]
				pair := matrices[matrixKey{model.Year, model.Scheme, model.Level}]
				matrix := pair.publications
				if model.Metric == "citations" {
					matrix = pair.citations
				}
				report.Models[i], errs[i] = checkPublishedModel(model, matrix)
			}
		})
	}
	for i := range ordered {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return report, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			return report, fmt.Errorf("model audit %s: %w", modelID(ordered[i].Year, ordered[i].Scheme, ordered[i].Level, ordered[i].Metric), e)
		}
	}
	auditOrientations(ordered, report.Models)
	report.Exports, err = auditScoreExports(ctx, resultDir, ordered, report.Models)
	if err != nil {
		return report, err
	}
	for _, model := range report.Models {
		if !model.Passed {
			report.FailedModels++
		}
	}
	for _, file := range report.Exports {
		if !file.Passed {
			report.FailedExports++
		}
	}
	if report.FailedModels > 0 || report.FailedExports > 0 {
		return report, fmt.Errorf("model audit: %d of 216 models and %d exported score files failed independent checks", report.FailedModels, report.FailedExports)
	}
	report.Status, report.Passed = "passed", true
	return report, nil
}

var auditLayers = [][2]string{{"topics", "field"}, {"topics", "subfield"}, {"concepts", "0"}, {"concepts", "1"}}

func isAuditLayer(s, l string) bool {
	return s == "topics" && (l == "field" || l == "subfield") || s == "concepts" && (l == "0" || l == "1")
}
func modelID(y int, s, l, m string) string { return fmt.Sprintf("%d/%s/%s/%s", y, s, l, m) }
func finiteModelValue(x float64) bool      { return !math.IsNaN(x) && !math.IsInf(x, 0) && x >= 0 }

func deriveIndependent(matrix auditMatrix) (state independentState, err error) {
	n, d := len(matrix.countries), len(matrix.disciplines)
	if len(matrix.values) != n*d {
		return state, errors.New("invalid independent matrix dimensions")
	}
	rows, cols := make([]float64, n), make([]float64, d)
	total := 0.0
	for i := 0; i < n; i++ {
		for j := 0; j < d; j++ {
			v := matrix.values[i*d+j]
			if !finiteModelValue(v) {
				return state, errors.New("invalid independent matrix cell")
			}
			rows[i] += v
			cols[j] += v
			total += v
		}
	}
	if !finiteModelValue(total) {
		return state, errors.New("independent matrix total overflow")
	}
	state.rca = make([][]float64, n)
	state.advantage = make([][]bool, n)
	state.diversity = make([]float64, n)
	state.ubiquity = make([]float64, d)
	for i := 0; i < n; i++ {
		state.rca[i] = make([]float64, d)
		state.advantage[i] = make([]bool, d)
		if rows[i] > 0 {
			state.activeCountries = append(state.activeCountries, i)
		}
		for j := 0; j < d; j++ {
			if rows[i] > 0 && cols[j] > 0 {
				r := (matrix.values[i*d+j] / rows[i]) / (cols[j] / total)
				if math.Abs(r-1) <= modelBoundaryTolerance {
					r = 1
				}
				state.rca[i][j] = r
				if r >= 1 {
					state.advantage[i][j] = true
					state.diversity[i]++
					state.ubiquity[j]++
				}
			}
		}
	}
	for j, v := range cols {
		if v > 0 {
			state.activeDisciplines = append(state.activeDisciplines, j)
		}
	}
	state.components = independentComponents(state.advantage, state.activeCountries, state.activeDisciplines)
	if len(state.activeCountries) < 2 || len(state.activeDisciplines) < 2 {
		state.status = "empty"
		return state, nil
	}
	a := len(state.activeCountries)
	t := mat.NewDense(a, a, nil)
	for x, i := range state.activeCountries {
		if state.diversity[i] <= 0 {
			return state, errors.New("positive country has no independent advantage")
		}
		for y, j := range state.activeCountries {
			value := 0.0
			for _, k := range state.activeDisciplines {
				if state.advantage[i][k] && state.advantage[j][k] {
					if state.ubiquity[k] <= 0 {
						return state, errors.New("positive discipline has no independent advantage")
					}
					value += 1 / (state.diversity[i] * state.ubiquity[k])
				}
			}
			t.Set(x, y, value)
		}
	}
	state.operator = t
	var eigen mat.Eigen
	if !eigen.Factorize(t, mat.EigenNone) {
		return state, errors.New("independent nonsymmetric eigendecomposition failed")
	}
	for _, v := range eigen.Values(nil) {
		if math.Abs(imag(v)) > 1e-8 {
			return state, errors.New("country operator has unexpectedly complex spectrum")
		}
		state.eigenvalues = append(state.eigenvalues, real(v))
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(state.eigenvalues)))
	state.lambda = state.eigenvalues[1]
	state.gap = math.Abs(state.eigenvalues[0] - state.lambda)
	if a > 2 {
		state.gap = math.Min(state.gap, math.Abs(state.lambda-state.eigenvalues[2]))
	}
	state.status = "ok"
	if len(state.components) > 1 {
		state.status = "disconnected"
	} else if math.Abs(1-state.lambda) < 1e-10 || (a > 2 && math.Abs(state.lambda-state.eigenvalues[2]) < 1e-10) || identicalRows(state.advantage, state.activeCountries, state.activeDisciplines) {
		state.status = "degenerate"
	}
	return state, nil
}

func checkPublishedModel(model publishedModel, matrix auditMatrix) (out ModelCheck, err error) {
	out = ModelCheck{Year: model.Year, Scheme: model.Scheme, Level: model.Level, Metric: model.Metric, ReportedStatus: model.Result.Status, Checks: []Check{}}
	state, err := deriveIndependent(matrix)
	if err != nil {
		return out, err
	}
	out.IndependentStatus = state.status
	out.SecondEigenvalue = state.lambda
	out.EigenGap = state.gap
	out.Eigenvalues = state.eigenvalues
	out.ComponentCount = len(state.components)
	for _, i := range state.activeCountries {
		out.diversity = append(out.diversity, state.diversity[i])
	}
	r := model.Result
	out.truth("country_dictionary", slices.Equal(r.Countries, matrix.countries))
	out.truth("discipline_dictionary", slices.Equal(r.Disciplines, matrix.disciplines))
	out.truth("active_country_indices", slices.Equal(r.ActiveCountries, state.activeCountries))
	out.truth("active_discipline_indices", slices.Equal(r.ActiveDisciplines, state.activeDisciplines))
	out.truth("status", r.Status == state.status)
	out.numeric("rca_boundary_tolerance", modelBoundaryTolerance, r.RCARoundingTolerance, 1e-20)
	shape := len(r.RCA) == len(state.rca) && len(r.Advantage) == len(state.advantage)
	if shape {
		for i := range state.rca {
			if len(r.RCA[i]) != len(state.rca[i]) || len(r.Advantage[i]) != len(state.advantage[i]) {
				shape = false
				break
			}
			for j, want := range state.rca[i] {
				diff := math.Abs(r.RCA[i][j] - want)
				if math.IsNaN(diff) || math.IsInf(diff, 0) {
					out.MaxRCAError = math.MaxFloat64
				} else {
					out.MaxRCAError = math.Max(out.MaxRCAError, diff)
				}
				if r.Advantage[i][j] != state.advantage[i][j] {
					out.AdvantageMismatches++
				}
			}
		}
	}
	out.truth("matrix_shapes", shape)
	out.numeric("max_rca_error", 0, out.MaxRCAError, 1e-10)
	out.numeric("advantage_mismatches", 0, float64(out.AdvantageMismatches), 0)
	out.numeric("component_count", float64(len(state.components)), float64(r.ComponentCount), 0)
	out.truth("component_membership", componentSignature(state.components) == componentSignature(r.Components))
	largest := 0
	for _, c := range state.components {
		largest = max(largest, len(c.Countries))
	}
	out.numeric("largest_component_countries", float64(largest), float64(r.LargestComponentCountries), 0)
	if len(state.eigenvalues) > 0 {
		out.numeric("second_eigenvalue", state.lambda, r.SecondEigenvalue, 1e-8)
		out.numeric("eigen_gap", state.gap, r.EigenGap, 1e-8)
		eigenvalues := append([]float64(nil), r.Eigenvalues...)
		sort.Sort(sort.Reverse(sort.Float64Slice(eigenvalues)))
		same := len(eigenvalues) == len(state.eigenvalues)
		maxDiff := 0.0
		if same {
			for i, want := range state.eigenvalues {
				maxDiff = math.Max(maxDiff, math.Abs(eigenvalues[i]-want))
			}
		}
		out.truth("spectrum_shape", same)
		out.numeric("max_spectrum_error", 0, maxDiff, 1e-8)
		if math.Abs(state.gap-1e-6) > 1e-10 {
			out.truth("unstable_flag", r.Unstable == (state.gap < 1e-6))
		}
	}
	if state.status != "ok" {
		out.truth("undefined_scores_absent", len(r.ECI) == 0 && len(r.PCI) == 0)
		out.finish()
		return out, nil
	}
	if len(r.ECI) != len(state.activeCountries) || len(r.PCI) != len(state.activeDisciplines) {
		out.truth("score_shapes", false)
		out.finish()
		return out, nil
	}
	out.ScoreChecksApplied = true
	out.ECIMean, out.ECIVariance = populationMoments(r.ECI)
	out.PCIMean, out.PCIVariance = populationMoments(r.PCI)
	out.numeric("eci_mean", 0, out.ECIMean, 1e-8)
	out.numeric("eci_population_variance", 1, out.ECIVariance, 1e-8)
	out.numeric("pci_mean", 0, out.PCIMean, 1e-8)
	out.numeric("pci_population_variance", 1, out.PCIVariance, 1e-8)
	transformed := make([]float64, len(r.ECI))
	for i := range transformed {
		for j, value := range r.ECI {
			transformed[i] += state.operator.At(i, j) * value
		}
	}
	mean, _ := populationMoments(transformed)
	for i, value := range transformed {
		delta := value - mean - state.lambda*r.ECI[i]
		out.OperatorResidual += delta * delta
	}
	out.OperatorResidual = math.Sqrt(out.OperatorResidual)
	out.numeric("centered_T_ECI_residual", 0, out.OperatorResidual, modelNumericalTolerance)
	projection := make([]float64, len(state.activeDisciplines))
	for y, j := range state.activeDisciplines {
		for x, i := range state.activeCountries {
			if state.advantage[i][j] {
				projection[y] += r.ECI[x] / state.ubiquity[j]
			}
		}
	}
	if !normalizeIndependent(projection) {
		out.truth("pci_projection_nonconstant", false)
	} else {
		for i, want := range projection {
			out.PCIMaxError = math.Max(out.PCIMaxError, math.Abs(r.PCI[i]-want))
		}
		out.numeric("pci_projection_max_error", 0, out.PCIMaxError, modelNumericalTolerance)
	}
	out.numeric("reported_symmetric_residual_bound", 0, r.Residual, 1e-7)
	out.truth("reported_residual_nonnegative", r.Residual >= 0)
	out.finish()
	return out, nil
}

func (m *ModelCheck) numeric(name string, expected, observed, tolerance float64) {
	delta := observed - expected
	pass := !math.IsNaN(observed) && !math.IsInf(observed, 0) && math.Abs(delta) <= tolerance
	if math.IsNaN(observed) || math.IsInf(observed, 0) {
		observed = math.MaxFloat64
	}
	if math.IsNaN(delta) || math.IsInf(delta, 0) {
		delta = math.MaxFloat64
	}
	m.Checks = append(m.Checks, Check{name, expected, observed, delta, tolerance, pass})
}
func (m *ModelCheck) truth(name string, value bool) {
	observed := 0.0
	if value {
		observed = 1
	}
	m.numeric(name, 1, observed, 0)
}
func (m *ModelCheck) finish() {
	m.Passed = true
	for _, c := range m.Checks {
		if !c.Passed {
			m.Passed = false
			break
		}
	}
	for _, value := range []*float64{&m.MaxRCAError, &m.OperatorResidual, &m.PCIMaxError, &m.ECIMean, &m.ECIVariance, &m.PCIMean, &m.PCIVariance, &m.TemporalCovariance, &m.DiversityCovariance} {
		if math.IsNaN(*value) || math.IsInf(*value, 0) {
			*value = math.MaxFloat64
		}
	}
}
func populationMoments(values []float64) (mean, variance float64) {
	if len(values) == 0 {
		return math.NaN(), math.NaN()
	}
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))
	for _, v := range values {
		variance += (v - mean) * (v - mean)
	}
	variance /= float64(len(values))
	return
}
func normalizeIndependent(values []float64) bool {
	mean, variance := populationMoments(values)
	sd := math.Sqrt(variance)
	if math.IsNaN(sd) || math.IsInf(sd, 0) || sd < 1e-14 {
		return false
	}
	for i := range values {
		values[i] = (values[i] - mean) / sd
	}
	return true
}
func identicalRows(m [][]bool, countries, disciplines []int) bool {
	for _, i := range countries[1:] {
		for _, j := range disciplines {
			if m[i][j] != m[countries[0]][j] {
				return false
			}
		}
	}
	return true
}
func independentComponents(m [][]bool, countries, disciplines []int) []eci.Component {
	n := len(m)
	parent := make([]int, n+len(disciplines))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for _, i := range countries {
		for k, j := range disciplines {
			if m[i][j] {
				a, b := find(i), find(n+k)
				parent[a] = b
			}
		}
	}
	groups := map[int]*eci.Component{}
	order := []int{}
	for _, i := range countries {
		root := find(i)
		if groups[root] == nil {
			groups[root] = &eci.Component{}
			order = append(order, root)
		}
		groups[root].Countries = append(groups[root].Countries, i)
	}
	for k, j := range disciplines {
		if group := groups[find(n+k)]; group != nil {
			group.Disciplines = append(group.Disciplines, j)
		}
	}
	out := []eci.Component{}
	for _, root := range order {
		out = append(out, *groups[root])
	}
	return out
}
func componentSignature(parts []eci.Component) string {
	out := []string{}
	for _, part := range parts {
		c, d := append([]int(nil), part.Countries...), append([]int(nil), part.Disciplines...)
		slices.Sort(c)
		slices.Sort(d)
		out = append(out, fmt.Sprintf("%v|%v", c, d))
	}
	slices.Sort(out)
	return strings.Join(out, ";")
}
func readModelCube(ctx context.Context, path string, yield func(aggregate.Row) error) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	r := parquet.NewGenericReader[aggregate.Row](f)
	defer r.Close()
	batch := make([]aggregate.Row, 2048)
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		n, e := r.Read(batch)
		for _, row := range batch[:n] {
			if err := yield(row); err != nil {
				return err
			}
		}
		clear(batch)
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
}
