package results

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strings"
	"sync"

	"openalexstats/internal/aggregate"
	"openalexstats/internal/model"
)

type denseLayer struct {
	scheme, level string
	ids           []string
	codes         map[string]int
	unknown       int
	exportOrder   []int
}
type denseLayout struct {
	layers                           []denseLayer
	countries                        []string
	countryCodes                     map[string]int
	countryOrder                     []int
	unknownCountry                   int
	labels                           []string
	labelLayer                       []int
	scenarios                        []Scenario
	thresholds                       []float64
	scenarioThreshold                []int
	yearStart, yearEnd, years, cells int
	estimatedBytes                   uint64
	signature                        [32]byte
}
type denseQuality struct {
	publications, citationCalls, citationEvents, missingCountryPublications, missingCountryCitations uint64
	missingPublications, missingCitations                                                            [4]uint64
	publicationAssignments, citationAssignments                                                      [4]uint64
}

// Dense is a worker-owned country/taxonomy/year/scenario accumulator. AddSeries
// is not safe for concurrent mutation. Its successful hot path allocates no
// objects; work strings/slices are borrowed only for the duration of the call.
type Dense struct {
	layout                         *denseLayout
	publications, citations        []float64
	quality                        []denseQuality
	countryMarks, conceptMarks     []uint64
	countryScratch, conceptScratch []int
	maxConceptScores               []float64
	selectedConcepts               [][]int
	nonzeroYears                   []int
	nonzeroValues                  []float64
}

// EstimateDenseBytes includes the two float64 cubes and bounded per-worker
// scratch. Small read-only dictionary/map/runtime overhead is additional.
func EstimateDenseBytes(dictionary Dictionary, scenarios []Scenario, start, end int) (uint64, error) {
	l, err := makeDenseLayout(dictionary, scenarios, start, end)
	if err != nil {
		return 0, err
	}
	return l.estimatedBytes, nil
}
func NewDense(dictionary Dictionary, scenarios []Scenario, start, end int) (*Dense, error) {
	l, err := makeDenseLayout(dictionary, scenarios, start, end)
	if err != nil {
		return nil, err
	}
	d := &Dense{layout: l, publications: make([]float64, l.cells), citations: make([]float64, l.cells), quality: make([]denseQuality, len(l.scenarios)), countryMarks: make([]uint64, (len(l.countries)+63)/64), conceptMarks: make([]uint64, (len(l.labels)+63)/64), countryScratch: make([]int, 0, len(l.countries)), conceptScratch: make([]int, 0, len(l.labels)), maxConceptScores: make([]float64, len(l.labels)), selectedConcepts: make([][]int, len(l.thresholds)*2), nonzeroYears: make([]int, 0, l.years), nonzeroValues: make([]float64, 0, l.years)}
	for t := range l.thresholds {
		for level := 0; level < 2; level++ {
			d.selectedConcepts[t*2+level] = make([]int, 0, len(l.layers[2+level].ids))
		}
	}
	return d, nil
}
func makeDenseLayout(dictionary Dictionary, scenarios []Scenario, start, end int) (*denseLayout, error) {
	if start < 1 || end < start || end-start > 10000 {
		return nil, fmt.Errorf("invalid dense year range")
	}
	if len(scenarios) == 0 {
		return nil, fmt.Errorf("dense requires scenarios")
	}
	l := &denseLayout{countryCodes: map[string]int{}, yearStart: start, yearEnd: end, years: end - start + 1, scenarios: append([]Scenario(nil), scenarios...)}
	countries := append([]Label(nil), dictionary.Countries...)
	sort.Slice(countries, func(i, j int) bool { return countries[i].ID < countries[j].ID })
	for _, c := range countries {
		if c.ID == aggregate.Unknown {
			continue
		}
		if c.ID == "" || strings.TrimSpace(c.ID) != c.ID {
			return nil, fmt.Errorf("invalid dense country dictionary ID")
		}
		if _, ok := l.countryCodes[c.ID]; ok {
			return nil, fmt.Errorf("duplicate dense country %s", c.ID)
		}
		l.countryCodes[c.ID] = len(l.countries)
		l.countries = append(l.countries, c.ID)
	}
	l.unknownCountry = len(l.countries)
	l.countryCodes[aggregate.Unknown] = l.unknownCountry
	l.countries = append(l.countries, aggregate.Unknown)
	l.countryOrder = make([]int, len(l.countries))
	for i := range l.countryOrder {
		l.countryOrder[i] = i
	}
	sort.Slice(l.countryOrder, func(i, j int) bool { return l.countries[l.countryOrder[i]] < l.countries[l.countryOrder[j]] })
	for layerIndex, key := range [][2]string{{"topics", "field"}, {"topics", "subfield"}, {"concepts", "0"}, {"concepts", "1"}} {
		var labels []Label
		found := false
		for _, layer := range dictionary.Layers {
			if layer.Scheme == key[0] && layer.Level == key[1] {
				if found {
					return nil, fmt.Errorf("duplicate dense layer")
				}
				found = true
				labels = append([]Label(nil), layer.Labels...)
			}
		}
		if !found {
			return nil, fmt.Errorf("dense dictionary missing %s/%s", key[0], key[1])
		}
		sort.Slice(labels, func(i, j int) bool { return labels[i].ID < labels[j].ID })
		layer := denseLayer{scheme: key[0], level: key[1], codes: map[string]int{}}
		for _, label := range labels {
			if label.ID == aggregate.Unknown {
				continue
			}
			if label.ID == "" || strings.TrimSpace(label.ID) != label.ID {
				return nil, fmt.Errorf("invalid dense discipline ID")
			}
			if _, ok := layer.codes[label.ID]; ok {
				return nil, fmt.Errorf("duplicate dense discipline %s", label.ID)
			}
			index := len(l.labels)
			layer.codes[label.ID] = index
			layer.ids = append(layer.ids, label.ID)
			l.labels = append(l.labels, label.ID)
			l.labelLayer = append(l.labelLayer, layerIndex)
		}
		layer.unknown = len(l.labels)
		layer.codes[aggregate.Unknown] = layer.unknown
		layer.ids = append(layer.ids, aggregate.Unknown)
		l.labels = append(l.labels, aggregate.Unknown)
		l.labelLayer = append(l.labelLayer, layerIndex)
		layer.exportOrder = make([]int, 0, len(layer.ids))
		for _, id := range layer.ids {
			layer.exportOrder = append(layer.exportOrder, layer.codes[id])
		}
		sort.Slice(layer.exportOrder, func(i, j int) bool { return l.labels[layer.exportOrder[i]] < l.labels[layer.exportOrder[j]] })
		l.layers = append(l.layers, layer)
	}
	seen := map[string]bool{}
	for _, s := range l.scenarios {
		if s.ID == "" || seen[s.ID] {
			return nil, fmt.Errorf("dense scenario IDs must be unique")
		}
		seen[s.ID] = true
		if s.CountryMode != "whole" && s.CountryMode != "fractional" || s.ConceptMode != "whole" && s.ConceptMode != "fractional" {
			return nil, fmt.Errorf("dense weights must be explicit")
		}
		if _, err := aggregate.New(s.Config()); err != nil {
			return nil, err
		}
		found := false
		for _, t := range l.thresholds {
			if t == s.ConceptThreshold {
				found = true
			}
		}
		if !found {
			l.thresholds = append(l.thresholds, s.ConceptThreshold)
		}
	}
	sort.Float64s(l.thresholds)
	for _, s := range l.scenarios {
		for i, t := range l.thresholds {
			if t == s.ConceptThreshold {
				l.scenarioThreshold = append(l.scenarioThreshold, i)
				break
			}
		}
	}
	cells := uint64(1)
	for _, n := range []int{len(l.scenarios), len(l.labels), len(l.countries), l.years} {
		if n < 1 || cells > uint64(math.MaxInt)/uint64(n)/16 {
			return nil, fmt.Errorf("dense dimensions exceed addressable storage")
		}
		cells *= uint64(n)
	}
	l.cells = int(cells)
	l.estimatedBytes = cells*16 + uint64(len(l.labels))*uint64(32+16*len(l.thresholds)) + uint64(len(l.countries))*24 + uint64(l.years)*16 + uint64(len(l.scenarios))*uint64(184)
	metadata, _ := json.Marshal(struct {
		Countries, Labels []string
		Scenarios         []Scenario
		Start, End        int
	}{l.countries, l.labels, l.scenarios, start, end})
	l.signature = sha256.Sum256(metadata)
	return l, nil
}
func (d *Dense) EstimatedBytes() uint64 { return d.layout.estimatedBytes }

// AddSeries counts publication only within this Dense's output window. counts
// is the target's complete citation-year vector (or nil for all zeros). Older
// target works remain eligible for citation counts. Unknown actual dictionary
// IDs return an error, never silently disappear from the fixed universe.
func (d *Dense) AddSeries(w model.Work, counts []uint64) error {
	l := d.layout
	if len(counts) != 0 && len(counts) != l.years {
		return fmt.Errorf("dense citation vector has %d years, expected %d", len(counts), l.years)
	}
	d.nonzeroYears = d.nonzeroYears[:0]
	d.nonzeroValues = d.nonzeroValues[:0]
	var citationEvents uint64
	for year, count := range counts {
		if count == 0 {
			continue
		}
		if math.MaxUint64-citationEvents < count {
			return fmt.Errorf("dense citation events overflow")
		}
		citationEvents += count
		d.nonzeroYears = append(d.nonzeroYears, year)
		d.nonzeroValues = append(d.nonzeroValues, float64(count))
	}
	includePublication := w.Year >= l.yearStart && w.Year <= l.yearEnd
	clear(d.countryMarks)
	d.countryScratch = d.countryScratch[:0]
	for _, country := range w.Countries {
		country = strings.TrimSpace(country)
		if country == "" {
			continue
		}
		index, ok := l.countryCodes[country]
		if !ok {
			return fmt.Errorf("dense country %q not in fixed dictionary; reconcile first", country)
		}
		bit := uint64(1) << (index % 64)
		if d.countryMarks[index/64]&bit == 0 {
			d.countryMarks[index/64] |= bit
			d.countryScratch = append(d.countryScratch, index)
		}
	}
	missingCountry := len(d.countryScratch) == 0
	if missingCountry {
		d.countryScratch = append(d.countryScratch, l.unknownCountry)
	}
	var primary [2]int
	var missingPrimary [2]bool
	for layer, id := range [2]string{w.FieldID, w.SubfieldID} {
		id = strings.TrimSpace(id)
		if id == "" {
			primary[layer] = l.layers[layer].unknown
			missingPrimary[layer] = true
			continue
		}
		index, ok := l.layers[layer].codes[id]
		if !ok {
			return fmt.Errorf("dense %s discipline %q not in fixed dictionary; reconcile first", l.layers[layer].level, id)
		}
		primary[layer] = index
	}
	clear(d.conceptMarks)
	d.conceptScratch = d.conceptScratch[:0]
	for i := range d.selectedConcepts {
		d.selectedConcepts[i] = d.selectedConcepts[i][:0]
	}
	for _, concept := range w.Concepts {
		if concept.Level < 0 || concept.Level > 1 || math.IsNaN(concept.Score) || math.IsInf(concept.Score, 0) {
			continue
		}
		id := strings.TrimSpace(concept.ID)
		if id == "" {
			continue
		}
		index, ok := l.layers[2+concept.Level].codes[id]
		if !ok {
			return fmt.Errorf("dense concept level %d ID %q not in fixed dictionary; reconcile first", concept.Level, id)
		}
		bit := uint64(1) << (index % 64)
		if d.conceptMarks[index/64]&bit == 0 {
			d.conceptMarks[index/64] |= bit
			d.conceptScratch = append(d.conceptScratch, index)
			d.maxConceptScores[index] = concept.Score
		} else if concept.Score > d.maxConceptScores[index] {
			d.maxConceptScores[index] = concept.Score
		}
	}
	for _, index := range d.conceptScratch {
		level := l.labelLayer[index] - 2
		score := d.maxConceptScores[index]
		for t, threshold := range l.thresholds {
			if score >= threshold {
				d.selectedConcepts[t*2+level] = append(d.selectedConcepts[t*2+level], index)
			}
		}
	}
	if !includePublication && citationEvents == 0 {
		return nil
	}
	publicationYear := w.Year - l.yearStart
	fractionalCountryWeight := 1 / float64(len(d.countryScratch))
	// Preflight every scenario before changing either quality or cube cells.
	for scenario := range l.scenarios {
		if _, ok := d.nextQuality(scenario, includePublication, citationEvents, missingCountry, missingPrimary); !ok {
			return fmt.Errorf("dense quality or assignment counter overflow")
		}
	}
	for scenario, s := range l.scenarios {
		d.quality[scenario], _ = d.nextQuality(scenario, includePublication, citationEvents, missingCountry, missingPrimary)
		countryWeight := 1.0
		if s.CountryMode == "fractional" {
			countryWeight = fractionalCountryWeight
		}
		for layer := 0; layer < 2; layer++ {
			d.addLabel(scenario, primary[layer], publicationYear, includePublication, countryWeight, 1)
		}
		threshold := l.scenarioThreshold[scenario]
		for level := 0; level < 2; level++ {
			selected := d.selectedConcepts[threshold*2+level]
			if len(selected) == 0 {
				d.addLabel(scenario, l.layers[2+level].unknown, publicationYear, includePublication, countryWeight, 1)
				continue
			}
			disciplineWeight := 1.0
			if s.ConceptMode == "fractional" {
				disciplineWeight = 1 / float64(len(selected))
			}
			for _, label := range selected {
				d.addLabel(scenario, label, publicationYear, includePublication, countryWeight, disciplineWeight)
			}
		}
	}
	return nil
}

// nextQuality computes exact raw label assignments, independently of country
// and concept weights. Empty layers receive one UNKNOWN assignment. All arrays
// are fixed-size values so successful AddSeries calls remain allocation-free.
func (d *Dense) nextQuality(scenario int, publication bool, events uint64, missingCountry bool, missingPrimary [2]bool) (denseQuality, bool) {
	var delta denseQuality
	if publication {
		delta.publications = 1
		if missingCountry {
			delta.missingCountryPublications = 1
		}
	}
	delta.citationCalls = uint64(len(d.nonzeroYears))
	delta.citationEvents = events
	if missingCountry {
		delta.missingCountryCitations = events
	}
	threshold := d.layout.scenarioThreshold[scenario]
	for layer := 0; layer < 4; layer++ {
		k, missing := uint64(1), false
		if layer < 2 {
			missing = missingPrimary[layer]
		} else {
			count := len(d.selectedConcepts[threshold*2+layer-2])
			k, missing = uint64(max(1, count)), count == 0
		}
		if events > math.MaxUint64/k {
			return denseQuality{}, false
		}
		if publication {
			delta.publicationAssignments[layer] = k
		}
		delta.citationAssignments[layer] = events * k
		if missing {
			delta.missingPublications[layer] = delta.publications
			delta.missingCitations[layer] = events
		}
	}
	return sumDenseQuality(d.quality[scenario], delta)
}

func sumDenseQuality(a, b denseQuality) (denseQuality, bool) {
	if math.MaxUint64-a.publications < b.publications || math.MaxUint64-a.citationCalls < b.citationCalls || math.MaxUint64-a.citationEvents < b.citationEvents ||
		math.MaxUint64-a.missingCountryPublications < b.missingCountryPublications || math.MaxUint64-a.missingCountryCitations < b.missingCountryCitations {
		return denseQuality{}, false
	}
	for layer := range a.missingPublications {
		if math.MaxUint64-a.missingPublications[layer] < b.missingPublications[layer] || math.MaxUint64-a.missingCitations[layer] < b.missingCitations[layer] ||
			math.MaxUint64-a.publicationAssignments[layer] < b.publicationAssignments[layer] || math.MaxUint64-a.citationAssignments[layer] < b.citationAssignments[layer] {
			return denseQuality{}, false
		}
	}
	a.publications += b.publications
	a.citationCalls += b.citationCalls
	a.citationEvents += b.citationEvents
	a.missingCountryPublications += b.missingCountryPublications
	a.missingCountryCitations += b.missingCountryCitations
	for layer := range a.missingPublications {
		a.missingPublications[layer] += b.missingPublications[layer]
		a.missingCitations[layer] += b.missingCitations[layer]
		a.publicationAssignments[layer] += b.publicationAssignments[layer]
		a.citationAssignments[layer] += b.citationAssignments[layer]
	}
	return a, true
}

func (d *Dense) addLabel(scenario, label, publicationYear int, includePublication bool, countryWeight, disciplineWeight float64) {
	l := d.layout
	for _, country := range d.countryScratch {
		base := ((scenario*len(l.labels)+label)*len(l.countries) + country) * l.years
		if includePublication {
			d.publications[base+publicationYear] += countryWeight * disciplineWeight
		}
		for i, year := range d.nonzeroYears {
			d.citations[base+year] += d.nonzeroValues[i] * countryWeight * disciplineWeight
		}
	}
}

// Merge requires matching fixed universes and disjoint work contributions.
// Call in a fixed worker order for reproducible floating-point addition. The
// per-call cube merge is parallel, but concurrent Merge calls are not safe.
func (d *Dense) Merge(other *Dense) error {
	if other == nil || d == other {
		return fmt.Errorf("dense merge requires distinct instances")
	}
	if d.layout.signature != other.layout.signature {
		return fmt.Errorf("dense layouts/scenarios differ")
	}
	for i, v := range other.quality {
		if _, ok := sumDenseQuality(d.quality[i], v); !ok {
			return fmt.Errorf("dense merge quality counter overflow")
		}
	}
	workers := 1
	if len(d.publications) > 1<<20 {
		workers = min(runtime.GOMAXPROCS(0), 16)
	}
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		start, end := len(d.publications)*worker/workers, len(d.publications)*(worker+1)/workers
		wg.Go(func() {
			for i := start; i < end; i++ {
				d.publications[i] += other.publications[i]
				d.citations[i] += other.citations[i]
			}
		})
	}
	wg.Wait()
	for i, v := range other.quality {
		d.quality[i], _ = sumDenseQuality(d.quality[i], v)
	}
	return nil
}
func (d *Dense) Inputs() []Input {
	l := d.layout
	out := make([]Input, len(l.scenarios))
	scenarioCells := len(l.labels) * len(l.countries) * l.years
	for scenario, s := range l.scenarios {
		nonzero := 0
		for i := scenario * scenarioCells; i < (scenario+1)*scenarioCells; i++ {
			if d.publications[i] != 0 || d.citations[i] != 0 {
				nonzero++
			}
		}
		input := Input{Scenario: s, Rows: make([]aggregate.Row, 0, nonzero)}
		for _, layerIndex := range []int{2, 3, 0, 1} {
			layer := l.layers[layerIndex]
			for year := 0; year < l.years; year++ {
				for _, country := range l.countryOrder {
					for _, label := range layer.exportOrder {
						cell := ((scenario*len(l.labels)+label)*len(l.countries)+country)*l.years + year
						p, c := d.publications[cell], d.citations[cell]
						if p != 0 || c != 0 {
							input.Rows = append(input.Rows, aggregate.Row{Country: l.countries[country], Year: l.yearStart + year, Scheme: layer.scheme, Level: layer.level, DisciplineID: l.labels[label], Publications: p, Citations: c})
						}
					}
				}
			}
		}
		q := d.quality[scenario]
		input.Quality = aggregate.Quality{PublicationWorks: q.publications, CitationCalls: q.citationCalls, CitationEvents: q.citationEvents, MissingCountryPublications: q.missingCountryPublications, MissingCountryCitations: q.missingCountryCitations, MissingDisciplinePublications: map[string]uint64{}, MissingDisciplineCitations: map[string]uint64{}, PublicationAssignments: map[string]uint64{}, CitationAssignments: map[string]uint64{}}
		for layerIndex, layer := range l.layers {
			key := layer.scheme + "." + layer.level
			if q.missingPublications[layerIndex] > 0 {
				input.Quality.MissingDisciplinePublications[key] = q.missingPublications[layerIndex]
			}
			if q.missingCitations[layerIndex] > 0 {
				input.Quality.MissingDisciplineCitations[key] = q.missingCitations[layerIndex]
			}
			if q.publicationAssignments[layerIndex] > 0 {
				input.Quality.PublicationAssignments[key] = q.publicationAssignments[layerIndex]
			}
			if q.citationAssignments[layerIndex] > 0 {
				input.Quality.CitationAssignments[key] = q.citationAssignments[layerIndex]
			}
		}
		out[scenario] = input
	}
	return out
}
