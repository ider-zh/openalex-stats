// Package results derives query-ready tables without changing corpus membership.
package results

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"openalexstats/internal/aggregate"
	"openalexstats/internal/eci"
)

type Scenario struct {
	ID               string  `json:"id"`
	CountryMode      string  `json:"country_mode"`
	ConceptMode      string  `json:"concept_mode"`
	ConceptThreshold float64 `json:"concept_threshold"`
	Main             bool    `json:"main"`
}

func DefaultScenarios() []Scenario {
	var out []Scenario
	for _, mode := range []string{"fractional", "whole"} {
		for _, threshold := range []float64{0, .3, .5} {
			out = append(out, Scenario{mode + "-concept-whole-" + strconv.FormatFloat(threshold, 'f', 1, 64), mode, "whole", threshold, threshold == .3})
		}
	}
	return out
}
func (s Scenario) Config() aggregate.Config {
	return aggregate.Config{CountryMode: s.CountryMode, ConceptMode: s.ConceptMode, ConceptThreshold: s.ConceptThreshold}
}
func (s Scenario) ForECI() bool {
	return s.Main && s.CountryMode == "fractional" && (s.ConceptMode == "whole" || s.ConceptMode == "fractional")
}

// ResolveScenarios validates externally supplied statistical scenarios. Empty
// input selects the hit-count defaults. Exactly one main fractional-country
// scenario owns the models; explicit historical concept fractions remain valid.
func ResolveScenarios(input []Scenario) ([]Scenario, error) {
	if len(input) == 0 {
		input = DefaultScenarios()
	}
	out := append([]Scenario(nil), input...)
	seen := make(map[string]bool, len(out))
	main := 0
	for _, s := range out {
		if s.ID == "" || strings.TrimSpace(s.ID) != s.ID || seen[s.ID] {
			return nil, fmt.Errorf("scenario IDs must be unique, nonempty and unpadded")
		}
		seen[s.ID] = true
		if (s.CountryMode != "fractional" && s.CountryMode != "whole") || (s.ConceptMode != "fractional" && s.ConceptMode != "whole") {
			return nil, fmt.Errorf("scenario %q must declare valid country and concept modes", s.ID)
		}
		if _, err := aggregate.New(s.Config()); err != nil {
			return nil, fmt.Errorf("scenario %q: %w", s.ID, err)
		}
		if s.ForECI() {
			main++
		}
	}
	if main != 1 {
		return nil, fmt.Errorf("exactly one main fractional-country ECI scenario required, got %d", main)
	}
	return out, nil
}

type Input struct {
	Scenario Scenario
	Rows     []aggregate.Row
	Quality  aggregate.Quality
}
type Row struct {
	ScenarioID             string  `json:"scenario_id" parquet:"scenario_id"`
	CountryMode            string  `json:"country_mode" parquet:"country_mode"`
	ConceptMode            string  `json:"concept_mode" parquet:"concept_mode"`
	ConceptThreshold       float64 `json:"concept_threshold" parquet:"concept_threshold"`
	Main                   bool    `json:"main" parquet:"main"`
	Country                string  `json:"country" parquet:"country"`
	Year                   int     `json:"year" parquet:"year"`
	Scheme                 string  `json:"scheme" parquet:"scheme"`
	Level                  string  `json:"level" parquet:"level"`
	DisciplineID           string  `json:"discipline_id" parquet:"discipline_id"`
	Publications           float64 `json:"publications" parquet:"publications"`
	Citations              float64 `json:"citations" parquet:"citations"`
	PublicationShare       float64 `json:"publication_share" parquet:"publication_share"`
	CitationShare          float64 `json:"citation_share" parquet:"citation_share"`
	PublicationRCA         float64 `json:"publication_rca" parquet:"publication_rca"`
	CitationRCA            float64 `json:"citation_rca" parquet:"citation_rca"`
	PublicationShareValid  bool    `json:"publication_share_valid" parquet:"publication_share_valid"`
	CitationShareValid     bool    `json:"citation_share_valid" parquet:"citation_share_valid"`
	PublicationRCAValid    bool    `json:"publication_rca_valid" parquet:"publication_rca_valid"`
	CitationRCAValid       bool    `json:"citation_rca_valid" parquet:"citation_rca_valid"`
	PublicationMatrixTotal float64 `json:"publication_matrix_total" parquet:"publication_matrix_total"`
	CitationMatrixTotal    float64 `json:"citation_matrix_total" parquet:"citation_matrix_total"`
}
type Coverage struct {
	ScenarioID                        string  `json:"scenario_id"`
	Scheme                            string  `json:"scheme"`
	Level                             string  `json:"level"`
	PublicationWorks                  uint64  `json:"publication_works"`
	CitationEvents                    uint64  `json:"citation_events"`
	MissingCountryPublications        uint64  `json:"missing_country_publications"`
	MissingCountryCitations           uint64  `json:"missing_country_citations"`
	UnclassifiedPublications          uint64  `json:"unclassified_publications"`
	UnclassifiedCitations             uint64  `json:"unclassified_citations"`
	PublicationClassificationCoverage float64 `json:"publication_classification_coverage"`
	CitationClassificationCoverage    float64 `json:"citation_classification_coverage"`
	PublicationCoverageValid          bool    `json:"publication_coverage_valid"`
	CitationCoverageValid             bool    `json:"citation_coverage_valid"`
}
type layerYear struct {
	scheme, level string
	year          int
}
type rowKey struct {
	layerYear
	country, id string
}
type pair struct{ p, c float64 }
type denominators struct {
	total       pair
	countries   map[string]pair
	disciplines map[string]pair
}

// Derive uses only known-country/known-discipline cells for both RCA margins.
// Shares are a country's share within one discipline, not country portfolio
// shares. Whole-count denominators are assignment totals, not unique works.
// UNKNOWN cells retain raw counts but never receive defined shares or RCA.
func Derive(inputs []Input) ([]Row, []Coverage, error) {
	var out []Row
	var coverage []Coverage
	seen := map[string]bool{}
	for _, input := range inputs {
		s := input.Scenario
		if (s.CountryMode != "fractional" && s.CountryMode != "whole") || (s.ConceptMode != "fractional" && s.ConceptMode != "whole") {
			return nil, nil, fmt.Errorf("scenario weighting labels must be explicit")
		}
		if s.ID == "" || seen[s.ID] {
			return nil, nil, fmt.Errorf("scenario IDs must be nonempty and unique")
		}
		seen[s.ID] = true
		if _, err := aggregate.New(s.Config()); err != nil {
			return nil, nil, err
		}
		cells := map[rowKey]pair{}
		denoms := map[layerYear]*denominators{}
		for _, r := range input.Rows {
			if r.Year <= 0 || !validLayer(r.Scheme, r.Level) || r.Country == "" || r.DisciplineID == "" || !finiteNonnegative(r.Publications) || !finiteNonnegative(r.Citations) {
				return nil, nil, fmt.Errorf("invalid cube row in %s", s.ID)
			}
			k := rowKey{layerYear{r.Scheme, r.Level, r.Year}, r.Country, r.DisciplineID}
			v := cells[k]
			v.p += r.Publications
			v.c += r.Citations
			if !finiteNonnegative(v.p) || !finiteNonnegative(v.c) {
				return nil, nil, fmt.Errorf("cube sum overflow")
			}
			cells[k] = v
		}
		for k, v := range cells {
			if k.country == aggregate.Unknown || k.id == aggregate.Unknown {
				continue
			}
			d := denoms[k.layerYear]
			if d == nil {
				d = &denominators{countries: map[string]pair{}, disciplines: map[string]pair{}}
				denoms[k.layerYear] = d
			}
			d.total = plus(d.total, v)
			d.countries[k.country] = plus(d.countries[k.country], v)
			d.disciplines[k.id] = plus(d.disciplines[k.id], v)
		}
		for _, d := range denoms {
			if !finiteNonnegative(d.total.p) || !finiteNonnegative(d.total.c) {
				return nil, nil, fmt.Errorf("matrix total overflow")
			}
		}
		for k, v := range cells {
			r := Row{ScenarioID: s.ID, CountryMode: s.CountryMode, ConceptMode: s.ConceptMode, ConceptThreshold: s.ConceptThreshold, Main: s.Main, Country: k.country, Year: k.year, Scheme: k.scheme, Level: k.level, DisciplineID: k.id, Publications: v.p, Citations: v.c}
			if d := denoms[k.layerYear]; d != nil {
				r.PublicationMatrixTotal = d.total.p
				r.CitationMatrixTotal = d.total.c
				if k.country != aggregate.Unknown && k.id != aggregate.Unknown {
					cr, dr := d.countries[k.country], d.disciplines[k.id]
					if dr.p > 0 {
						r.PublicationShare = v.p / dr.p
						r.PublicationShareValid = true
					}
					if dr.c > 0 {
						r.CitationShare = v.c / dr.c
						r.CitationShareValid = true
					}
					if cr.p > 0 && dr.p > 0 && d.total.p > 0 {
						r.PublicationRCA = eci.NormalizeRCA((v.p / cr.p) / (dr.p / d.total.p))
						r.PublicationRCAValid = true
					}
					if cr.c > 0 && dr.c > 0 && d.total.c > 0 {
						r.CitationRCA = eci.NormalizeRCA((v.c / cr.c) / (dr.c / d.total.c))
						r.CitationRCAValid = true
					}
				}
			}
			out = append(out, r)
		}
		for _, l := range [][2]string{{"topics", "field"}, {"topics", "subfield"}, {"concepts", "0"}, {"concepts", "1"}} {
			q := input.Quality
			k := l[0] + "." + l[1]
			c := Coverage{ScenarioID: s.ID, Scheme: l[0], Level: l[1], PublicationWorks: q.PublicationWorks, CitationEvents: q.CitationEvents, MissingCountryPublications: q.MissingCountryPublications, MissingCountryCitations: q.MissingCountryCitations, UnclassifiedPublications: q.MissingDisciplinePublications[k], UnclassifiedCitations: q.MissingDisciplineCitations[k]}
			if c.UnclassifiedPublications > c.PublicationWorks || c.UnclassifiedCitations > c.CitationEvents {
				return nil, nil, fmt.Errorf("quality counts exceed work/event totals in %s", s.ID)
			}
			if c.PublicationWorks > 0 {
				c.PublicationClassificationCoverage = 1 - float64(c.UnclassifiedPublications)/float64(c.PublicationWorks)
				c.PublicationCoverageValid = true
			}
			if c.CitationEvents > 0 {
				c.CitationClassificationCoverage = 1 - float64(c.UnclassifiedCitations)/float64(c.CitationEvents)
				c.CitationCoverageValid = true
			}
			coverage = append(coverage, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.ScenarioID != b.ScenarioID {
			return a.ScenarioID < b.ScenarioID
		}
		if a.Scheme != b.Scheme {
			return a.Scheme < b.Scheme
		}
		if a.Level != b.Level {
			return a.Level < b.Level
		}
		if a.Year != b.Year {
			return a.Year < b.Year
		}
		if a.Country != b.Country {
			return a.Country < b.Country
		}
		return a.DisciplineID < b.DisciplineID
	})
	return out, coverage, nil
}
func plus(a, b pair) pair              { return pair{a.p + b.p, a.c + b.c} }
func finiteNonnegative(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) && x >= 0 }
func validLayer(s, l string) bool {
	return s == "topics" && (l == "field" || l == "subfield") || s == "concepts" && (l == "0" || l == "1")
}
