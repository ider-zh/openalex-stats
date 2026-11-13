// Package complexityfilter builds explicitly filtered country-discipline inputs
// before computing complexity. Original statistics and unfiltered ranks are not
// rewritten. Both methods use the same final RCA matrix and binary network.
package complexityfilter

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"

	"openalexstats/internal/eci"
)

const Algorithm = "country-scale-cell-zero-monotone-diversity-v1"

// Profile thresholds are inclusive minimums. CellMinSupport is applied to the
// original fractional-country X cell before recalculating all RCA margins.
// MinDiversity=0 disables the diversity pruning pass.
type Profile struct {
	ID                       string  `json:"id"`
	DisplayName              string  `json:"display_name"`
	CountryMinPublications   uint64  `json:"country_min_publications"`
	CountryMinCitationEvents uint64  `json:"country_min_citation_events"`
	CellMinSupport           float64 `json:"cell_min_support"`
	MinDiversity             int     `json:"min_diversity"`
}

// Input national scales are whole-country counts from primary Topics Field,
// including UNKNOWN subject, not sums over multi-label Concepts. Citation
// events are not independent target-paper sample sizes.
type Input struct {
	Matrix            eci.Matrix `json:"matrix"`
	Metric            string     `json:"metric"`
	WholePublications []uint64   `json:"whole_publications"`
	WholeCitations    []uint64   `json:"whole_citations"`
}

type CountryDecision struct {
	Country           string   `json:"country"`
	WholePublications uint64   `json:"whole_publications"`
	WholeCitations    uint64   `json:"whole_citations"`
	WholeBasisEvents  uint64   `json:"whole_basis_events"`
	OriginalMass      float64  `json:"original_matrix_mass"`
	RetainedMass      float64  `json:"retained_matrix_mass"`
	FinalDiversity    int      `json:"final_diversity"`
	Excluded          bool     `json:"excluded"`
	ExclusionRound    int      `json:"exclusion_round,omitempty"`
	Reasons           []string `json:"reasons,omitempty"`
}

type Round struct {
	Round             int      `json:"round"`
	MatrixMass        float64  `json:"matrix_mass"`
	PositiveCountries int      `json:"positive_countries"`
	RemovedCountries  []string `json:"removed_countries"`
}

type Diagnostics struct {
	Algorithm               string            `json:"algorithm"`
	Profile                 Profile           `json:"profile"`
	Metric                  string            `json:"metric"`
	SourceMatrixSHA256      string            `json:"source_matrix_sha256"`
	FinalMatrixSHA256       string            `json:"final_matrix_sha256"`
	FinalNetworkSHA256      string            `json:"final_network_sha256"`
	OriginalMass            float64           `json:"original_known_classified_assignment_mass"`
	AfterCountryScaleMass   float64           `json:"after_country_scale_mass"`
	AfterCellSupportMass    float64           `json:"after_cell_support_mass"`
	FinalMass               float64           `json:"final_mass"`
	RetainedMassFraction    *float64          `json:"retained_mass_fraction"`
	CountryScaleRemoved     int               `json:"country_scale_removed"`
	CellSupportRemovedCells int               `json:"cell_support_removed_cells"`
	DiversityRemoved        int               `json:"diversity_removed"`
	Rounds                  []Round           `json:"rounds"`
	Countries               []CountryDecision `json:"countries"`
}

type Output struct {
	Matrix                eci.Matrix                `json:"filtered_matrix"`
	Spectral              eci.Result                `json:"spectral"`
	Reflections           eci.Result                `json:"reflections"`
	ReflectionDiagnostics eci.ReflectionDiagnostics `json:"reflection_diagnostics"`
	Diagnostics           Diagnostics               `json:"diagnostics"`
}

func (p Profile) Validate() error {
	if p.ID == "" || p.CellMinSupport < 0 || math.IsNaN(p.CellMinSupport) || math.IsInf(p.CellMinSupport, 0) || p.MinDiversity < 0 {
		return fmt.Errorf("invalid complexity filter profile")
	}
	return nil
}

// Compute applies scale exclusions and cell zeroing once, then repeatedly
// removes every currently positive row whose newly recomputed RCA degree is
// below MinDiversity. Rows never reenter. At least one row is removed on each
// nonfinal pass, so termination is bounded by n+1 RCA passes and one production spectral solve.
// Removed-country coauthor weights are never reassigned to retained countries.
func Compute(input Input, profile Profile) (Output, error) {
	var out Output
	if err := profile.Validate(); err != nil {
		return out, err
	}
	if input.Metric != "publications" && input.Metric != "citations" {
		return out, fmt.Errorf("invalid complexity basis")
	}
	n, d := len(input.Matrix.Countries), len(input.Matrix.Disciplines)
	if len(input.Matrix.Values) != n || len(input.WholePublications) != n || len(input.WholeCitations) != n {
		return out, fmt.Errorf("complexity filter country dimension mismatch")
	}
	matrix := eci.Matrix{Countries: append([]string(nil), input.Matrix.Countries...), Disciplines: append([]string(nil), input.Matrix.Disciplines...), Values: make([][]float64, n)}
	diag := Diagnostics{Algorithm: Algorithm, Profile: profile, Metric: input.Metric, Countries: make([]CountryDecision, n)}
	for i, values := range input.Matrix.Values {
		if len(values) != d {
			return out, fmt.Errorf("complexity filter discipline dimension mismatch")
		}
		decision := CountryDecision{Country: matrix.Countries[i], WholePublications: input.WholePublications[i], WholeCitations: input.WholeCitations[i], WholeBasisEvents: input.WholePublications[i]}
		if input.Metric == "citations" {
			decision.WholeBasisEvents = input.WholeCitations[i]
		}
		if decision.WholePublications < profile.CountryMinPublications {
			decision.Reasons = append(decision.Reasons, "country_min_publications")
		}
		if input.Metric == "citations" && input.WholeCitations[i] < profile.CountryMinCitationEvents {
			decision.Reasons = append(decision.Reasons, "country_min_citation_events")
		}
		decision.Excluded = len(decision.Reasons) > 0
		if decision.Excluded {
			diag.CountryScaleRemoved++
		}
		matrix.Values[i] = make([]float64, d)
		for j, x := range values {
			// Invalid raw values must not disappear behind an exclusion gate.
			if x < 0 || math.IsNaN(x) || math.IsInf(x, 0) {
				return out, fmt.Errorf("raw matrix must be finite and nonnegative")
			}
			decision.OriginalMass += x
			if decision.Excluded {
				continue
			}
			if x < profile.CellMinSupport {
				if x > 0 {
					diag.CellSupportRemovedCells++
				}
				continue
			}
			matrix.Values[i][j] = x
		}
		diag.OriginalMass += decision.OriginalMass
		if !decision.Excluded {
			diag.AfterCountryScaleMass += decision.OriginalMass
		}
		diag.AfterCellSupportMass += sum(matrix.Values[i])
		diag.Countries[i] = decision
	}
	if math.IsInf(diag.OriginalMass, 0) {
		return out, fmt.Errorf("raw matrix sum overflow")
	}
	for iteration := 1; iteration <= n+1; iteration++ {
		degrees := rcaDegrees(matrix)
		round := Round{Round: iteration, RemovedCountries: []string{}}
		remove := []int{}
		for i, row := range matrix.Values {
			mass := sum(row)
			round.MatrixMass += mass
			if mass == 0 {
				continue
			}
			round.PositiveCountries++
			degree := degrees[i]
			if degree < profile.MinDiversity {
				remove = append(remove, i)
				round.RemovedCountries = append(round.RemovedCountries, matrix.Countries[i])
			}
		}
		diag.Rounds = append(diag.Rounds, round)
		if len(remove) == 0 {
			prepared, err := eci.Compute(matrix)
			if err != nil {
				return out, err
			}
			for i, row := range prepared.Advantage {
				if degree(row) != degrees[i] {
					return out, fmt.Errorf("lightweight RCA degree differs from production")
				}
			}
			out.Spectral = prepared
			break
		}
		for _, i := range remove {
			clear(matrix.Values[i])
			diag.Countries[i].Excluded = true
			diag.Countries[i].ExclusionRound = iteration
			diag.Countries[i].Reasons = append(diag.Countries[i].Reasons, "min_diversity")
			diag.DiversityRemoved++
		}
	}
	if out.Spectral.Status == "" {
		return out, fmt.Errorf("complexity filter did not reach monotone fixed point")
	}
	for i, row := range matrix.Values {
		diag.Countries[i].RetainedMass = sum(row)
		diag.Countries[i].FinalDiversity = degree(out.Spectral.Advantage[i])
		diag.FinalMass += diag.Countries[i].RetainedMass
		if !diag.Countries[i].Excluded && diag.Countries[i].RetainedMass == 0 {
			diag.Countries[i].Excluded = true
			diag.Countries[i].Reasons = append(diag.Countries[i].Reasons, "zero_matrix_support")
		}
	}
	if diag.OriginalMass > 0 {
		v := diag.FinalMass / diag.OriginalMass
		diag.RetainedMassFraction = &v
	}
	var err error
	out.Reflections, out.ReflectionDiagnostics, err = eci.ReflectionsFromPrepared(out.Spectral, eci.DefaultReflectionConfig())
	if err != nil {
		return Output{}, err
	}
	out.Matrix, out.Diagnostics = matrix, diag
	out.Diagnostics.SourceMatrixSHA256 = MatrixSHA256(input.Matrix)
	out.Diagnostics.FinalMatrixSHA256 = MatrixSHA256(matrix)
	out.Diagnostics.FinalNetworkSHA256 = NetworkSHA256(out.Spectral)
	return out, nil
}

// MatrixSHA256 hashes header OA-COMPLEXITY-MATRIX-1\n, uint64-LE n/d,
// length-prefixed UTF-8 country then discipline labels, then row-major float64
// bits. NetworkSHA256 uses NETWORK instead of MATRIX and one 0/1 byte per edge.
// These cover excluded labels too, so no renumbered subset can share an identity.
func MatrixSHA256(m eci.Matrix) string {
	b := hashHeader("MATRIX", m.Countries, m.Disciplines)
	for _, row := range m.Values {
		for _, x := range row {
			b = binary.LittleEndian.AppendUint64(b, math.Float64bits(x))
		}
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func NetworkSHA256(r eci.Result) string {
	b := hashHeader("NETWORK", r.Countries, r.Disciplines)
	for _, row := range r.Advantage {
		for _, edge := range row {
			v := byte(0)
			if edge {
				v = 1
			}
			b = append(b, v)
		}
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func hashHeader(kind string, countries, disciplines []string) []byte {
	b := []byte("OA-COMPLEXITY-" + kind + "-1\n")
	b = binary.LittleEndian.AppendUint64(b, uint64(len(countries)))
	b = binary.LittleEndian.AppendUint64(b, uint64(len(disciplines)))
	for _, labels := range [][]string{countries, disciplines} {
		for _, label := range labels {
			b = binary.LittleEndian.AppendUint64(b, uint64(len(label)))
			b = append(b, label...)
		}
	}
	return b
}

func sum(values []float64) float64 {
	v := 0.0
	for _, x := range values {
		v += x
	}
	return v
}

func degree(values []bool) int {
	n := 0
	for _, edge := range values {
		if edge {
			n++
		}
	}
	return n
}

// rcaDegrees deliberately matches production Compute's row/column/total loop
// and division order, including its 1e-12 RCA=1 boundary normalization. This
// avoids an eigendecomposition for every transient pruning round. Final degrees
// are cross-checked against the production solver before returning any score.
func rcaDegrees(m eci.Matrix) []int {
	row, col := make([]float64, len(m.Countries)), make([]float64, len(m.Disciplines))
	total := 0.0
	for i, values := range m.Values {
		for j, x := range values {
			row[i] += x
			col[j] += x
			total += x
		}
	}
	degrees := make([]int, len(row))
	for i, values := range m.Values {
		for j, x := range values {
			if row[i] > 0 && col[j] > 0 && eci.NormalizeRCA((x/row[i])/(col[j]/total)) >= 1 {
				degrees[i]++
			}
		}
	}
	return degrees
}
