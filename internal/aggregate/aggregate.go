// Package aggregate computes country-year-discipline cubes. An Aggregator is not
// safe for concurrent mutation; workers should own independent instances.
package aggregate

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"openalexstats/internal/model"
)

const Unknown = "UNKNOWN"

type Config struct {
	CountryMode      string  `json:"country_mode"`
	ConceptMode      string  `json:"concept_mode"`
	ConceptThreshold float64 `json:"concept_threshold"`
}

type Row struct {
	Country      string  `json:"country" parquet:"country"`
	Year         int     `json:"year" parquet:"year"`
	Scheme       string  `json:"scheme" parquet:"scheme"`
	Level        string  `json:"level" parquet:"level"`
	DisciplineID string  `json:"discipline_id" parquet:"discipline_id"`
	Publications float64 `json:"publications" parquet:"publications"`
	Citations    float64 `json:"citations" parquet:"citations"`
}

type Quality struct {
	PublicationWorks              uint64            `json:"publication_works"`
	CitationCalls                 uint64            `json:"citation_calls"`
	CitationEvents                uint64            `json:"citation_events"`
	MissingCountryPublications    uint64            `json:"missing_country_publications"`
	MissingCountryCitations       uint64            `json:"missing_country_citations"`
	MissingDisciplinePublications map[string]uint64 `json:"missing_discipline_publications"`
	MissingDisciplineCitations    map[string]uint64 `json:"missing_discipline_citations"`
	PublicationAssignments        map[string]uint64 `json:"publication_assignments"`
	CitationAssignments           map[string]uint64 `json:"citation_assignments"`
}

type key struct {
	country           string
	year              int
	scheme, level, id string
}
type totals struct{ publications, citations float64 }
type Aggregator struct {
	cfg     Config
	rows    map[key]totals
	quality Quality
}

func New(cfg Config) (*Aggregator, error) {
	if cfg.CountryMode == "" {
		cfg.CountryMode = "fractional"
	}
	if cfg.ConceptMode == "" {
		cfg.ConceptMode = "whole"
	}
	for _, m := range []string{cfg.CountryMode, cfg.ConceptMode} {
		if m != "fractional" && m != "whole" {
			return nil, fmt.Errorf("invalid weighting mode %q", m)
		}
	}
	if math.IsNaN(cfg.ConceptThreshold) || math.IsInf(cfg.ConceptThreshold, 0) || cfg.ConceptThreshold < 0 || cfg.ConceptThreshold > 1 {
		return nil, fmt.Errorf("concept threshold must be finite in [0,1]")
	}
	return &Aggregator{cfg: cfg, rows: make(map[key]totals), quality: Quality{MissingDisciplinePublications: map[string]uint64{}, MissingDisciplineCitations: map[string]uint64{}, PublicationAssignments: map[string]uint64{}, CitationAssignments: map[string]uint64{}}}, nil
}

func (a *Aggregator) AddPublication(w model.Work) error { return a.add(w, w.Year, 1, false) }
func (a *Aggregator) AddCitation(w model.Work, year int, count uint64) error {
	return a.add(w, year, count, true)
}
func unique(values []string) []string {
	m := map[string]struct{}{}
	for _, s := range values {
		if s = strings.TrimSpace(s); s != "" {
			m[s] = struct{}{}
		}
	}
	r := make([]string, 0, len(m))
	for s := range m {
		r = append(r, s)
	}
	sort.Strings(r)
	return r
}

type assignment struct {
	scheme, level string
	ids           []string
}
type prepared struct {
	countries      []string
	missingCountry bool
	assignments    []assignment
	countryWeight  float64
}

func (a *Aggregator) prepare(w model.Work) prepared {
	p := prepared{countries: unique(w.Countries), assignments: []assignment{{"topics", "field", unique([]string{w.FieldID})}, {"topics", "subfield", unique([]string{w.SubfieldID})}, {"concepts", "0", nil}, {"concepts", "1", nil}}, countryWeight: 1}
	p.missingCountry = len(p.countries) == 0
	if p.missingCountry {
		p.countries = []string{Unknown}
	}
	for _, c := range w.Concepts {
		if c.Level >= 0 && c.Level <= 1 && !math.IsNaN(c.Score) && !math.IsInf(c.Score, 0) && c.Score >= a.cfg.ConceptThreshold {
			i := c.Level + 2
			p.assignments[i].ids = append(p.assignments[i].ids, c.ID)
		}
	}
	for i := range p.assignments {
		p.assignments[i].ids = unique(p.assignments[i].ids)
	}
	if a.cfg.CountryMode == "fractional" {
		p.countryWeight /= float64(len(p.countries))
	}
	return p
}
func (a *Aggregator) add(w model.Work, year int, count uint64, citation bool) error {
	if year <= 0 {
		return fmt.Errorf("invalid year %d", year)
	}
	if count == 0 {
		return nil
	}
	p := a.prepare(w)
	publication, calls, events := uint64(1), uint64(0), uint64(0)
	if citation {
		publication, calls, events = 0, 1, count
	}
	if err := a.recordQuality(p, publication, calls, events); err != nil {
		return err
	}
	a.recordRows(p, year, count, citation)
	return nil
}

// AddSeries prepares countries and four label sets once, then reuses them for
// a complete target-work citation series. includePublication must be set only
// when w.Year is in the requested publication output window.
func (a *Aggregator) AddSeries(w model.Work, yearStart int, counts []uint64, includePublication bool) error {
	if includePublication && w.Year <= 0 {
		return fmt.Errorf("invalid publication year %d", w.Year)
	}
	if len(counts) > 0 && (yearStart <= 0 || yearStart > int(^uint(0)>>1)-len(counts)+1) {
		return fmt.Errorf("invalid citation year range")
	}
	var calls, events uint64
	for _, c := range counts {
		if c > 0 {
			if math.MaxUint64-events < c {
				return fmt.Errorf("aggregate citation events overflow")
			}
			events += c
			calls++
		}
	}
	if !includePublication && events == 0 {
		return nil
	}
	p := a.prepare(w)
	publication := uint64(0)
	if includePublication {
		publication = 1
	}
	if err := a.recordQuality(p, publication, calls, events); err != nil {
		return err
	}
	if includePublication {
		a.recordRows(p, w.Year, 1, false)
	}
	for i, c := range counts {
		if c > 0 {
			a.recordRows(p, yearStart+i, c, true)
		}
	}
	return nil
}

// recordQuality validates the complete contribution before mutating any counters
// or cube cells. Assignments count raw, deduplicated label hits (including one
// UNKNOWN when empty), independently of either weighting mode.
func (a *Aggregator) recordQuality(p prepared, publication, calls, events uint64) error {
	var publicationAssignments, citationAssignments [4]uint64
	var keys [4]string
	for i, assignment := range p.assignments {
		k := uint64(max(1, len(assignment.ids)))
		if events > math.MaxUint64/k {
			return fmt.Errorf("aggregate citation assignments overflow")
		}
		publicationAssignments[i] = publication * k
		citationAssignments[i] = events * k
		keys[i] = assignment.scheme + "." + assignment.level
	}
	q := &a.quality
	if math.MaxUint64-q.PublicationWorks < publication || math.MaxUint64-q.CitationCalls < calls || math.MaxUint64-q.CitationEvents < events ||
		p.missingCountry && (math.MaxUint64-q.MissingCountryPublications < publication || math.MaxUint64-q.MissingCountryCitations < events) {
		return fmt.Errorf("aggregate quality counter overflow")
	}
	for i, key := range keys {
		if math.MaxUint64-q.PublicationAssignments[key] < publicationAssignments[i] || math.MaxUint64-q.CitationAssignments[key] < citationAssignments[i] ||
			len(p.assignments[i].ids) == 0 && (math.MaxUint64-q.MissingDisciplinePublications[key] < publication || math.MaxUint64-q.MissingDisciplineCitations[key] < events) {
			return fmt.Errorf("aggregate quality counter overflow")
		}
	}
	q.PublicationWorks += publication
	q.CitationCalls += calls
	q.CitationEvents += events
	if p.missingCountry {
		q.MissingCountryPublications += publication
		q.MissingCountryCitations += events
	}
	for i, key := range keys {
		if publication > 0 {
			q.PublicationAssignments[key] += publicationAssignments[i]
		}
		if events > 0 {
			q.CitationAssignments[key] += citationAssignments[i]
		}
		if len(p.assignments[i].ids) == 0 {
			if publication > 0 {
				q.MissingDisciplinePublications[key] += publication
			}
			if events > 0 {
				q.MissingDisciplineCitations[key] += events
			}
		}
	}
	return nil
}

func (a *Aggregator) recordRows(p prepared, year int, count uint64, citation bool) {
	for _, assignment := range p.assignments {
		ids := assignment.ids
		if len(ids) == 0 {
			ids = []string{Unknown}
		}
		dw := 1.0
		if assignment.scheme == "concepts" && a.cfg.ConceptMode == "fractional" {
			dw /= float64(len(ids))
		}
		for _, country := range p.countries {
			for _, id := range ids {
				k := key{country, year, assignment.scheme, assignment.level, id}
				v := a.rows[k]
				weight := float64(count) * p.countryWeight * dw
				if citation {
					v.citations += weight
				} else {
					v.publications += weight
				}
				a.rows[k] = v
			}
		}
	}
}
func (a *Aggregator) Rows() []Row {
	r := make([]Row, 0, len(a.rows))
	for k, v := range a.rows {
		r = append(r, Row{k.country, k.year, k.scheme, k.level, k.id, v.publications, v.citations})
	}
	sort.Slice(r, func(i, j int) bool {
		x, y := r[i], r[j]
		if x.Scheme != y.Scheme {
			return x.Scheme < y.Scheme
		}
		if x.Level != y.Level {
			return x.Level < y.Level
		}
		if x.Year != y.Year {
			return x.Year < y.Year
		}
		if x.Country != y.Country {
			return x.Country < y.Country
		}
		return x.DisciplineID < y.DisciplineID
	})
	return r
}
func (a *Aggregator) Quality() Quality {
	q := a.quality
	q.MissingDisciplinePublications = cloneMap(q.MissingDisciplinePublications)
	q.MissingDisciplineCitations = cloneMap(q.MissingDisciplineCitations)
	q.PublicationAssignments = cloneMap(q.PublicationAssignments)
	q.CitationAssignments = cloneMap(q.CitationAssignments)
	return q
}
func cloneMap(m map[string]uint64) map[string]uint64 {
	r := make(map[string]uint64, len(m))
	for k, v := range m {
		r[k] = v
	}
	return r
}

// Merge combines disjoint worker contributions. The caller must merge in a
// stable order for reproducible floating-point results and must not submit the
// same work contribution to more than one worker.
func (a *Aggregator) Merge(other *Aggregator) error {
	if other == nil || other == a {
		return fmt.Errorf("merge requires a distinct non-nil aggregator")
	}
	if a.cfg != other.cfg {
		return fmt.Errorf("cannot merge aggregators with different weighting rules")
	}
	if math.MaxUint64-a.quality.PublicationWorks < other.quality.PublicationWorks || math.MaxUint64-a.quality.CitationCalls < other.quality.CitationCalls ||
		math.MaxUint64-a.quality.CitationEvents < other.quality.CitationEvents || math.MaxUint64-a.quality.MissingCountryPublications < other.quality.MissingCountryPublications ||
		math.MaxUint64-a.quality.MissingCountryCitations < other.quality.MissingCountryCitations {
		return fmt.Errorf("aggregate merge quality overflow")
	}
	for _, pair := range [][2]map[string]uint64{{a.quality.MissingDisciplinePublications, other.quality.MissingDisciplinePublications}, {a.quality.MissingDisciplineCitations, other.quality.MissingDisciplineCitations}, {a.quality.PublicationAssignments, other.quality.PublicationAssignments}, {a.quality.CitationAssignments, other.quality.CitationAssignments}} {
		for k, v := range pair[1] {
			if math.MaxUint64-pair[0][k] < v {
				return fmt.Errorf("aggregate merge quality overflow")
			}
		}
	}
	for k, v := range other.rows {
		t := a.rows[k]
		t.publications += v.publications
		t.citations += v.citations
		a.rows[k] = t
	}
	a.quality.PublicationWorks += other.quality.PublicationWorks
	a.quality.CitationCalls += other.quality.CitationCalls
	a.quality.CitationEvents += other.quality.CitationEvents
	a.quality.MissingCountryPublications += other.quality.MissingCountryPublications
	a.quality.MissingCountryCitations += other.quality.MissingCountryCitations
	for k, v := range other.quality.MissingDisciplinePublications {
		a.quality.MissingDisciplinePublications[k] += v
	}
	for k, v := range other.quality.MissingDisciplineCitations {
		a.quality.MissingDisciplineCitations[k] += v
	}
	for k, v := range other.quality.PublicationAssignments {
		a.quality.PublicationAssignments[k] += v
	}
	for k, v := range other.quality.CitationAssignments {
		a.quality.CitationAssignments[k] += v
	}
	return nil
}
