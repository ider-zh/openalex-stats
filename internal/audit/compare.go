package audit

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"openalexstats/internal/production"
	"openalexstats/internal/results"
)

type annual struct {
	Count             uint64
	Samples           map[string]float64
	Assignments       map[string]uint64
	SelfLoops, Future uint64
}

func readAnnual(path, countColumn string) (map[int]annual, error) {
	var rows []map[string]json.RawMessage
	if err := readJSON(path, &rows); err != nil {
		return nil, err
	}
	out := map[int]annual{}
	for _, row := range rows {
		var year int
		var value annual
		value.Samples = map[string]float64{}
		value.Assignments = map[string]uint64{}
		for key, raw := range row {
			if key == "country_assignments" || strings.HasSuffix(key, "_assignments") {
				var n uint64
				if err := json.Unmarshal(raw, &n); err != nil {
					return nil, err
				}
				value.Assignments[key] = n
			}
		}
		if err := json.Unmarshal(row["year"], &year); err != nil {
			return nil, err
		}
		if year < 2000 || year > 2026 {
			return nil, fmt.Errorf("audit: oracle returned out-of-range year")
		}
		if _, exists := out[year]; exists {
			return nil, fmt.Errorf("audit: duplicate oracle year")
		}
		if err := json.Unmarshal(row[countColumn], &value.Count); err != nil {
			return nil, err
		}
		for _, cell := range sampleCells {
			var n float64
			if err := json.Unmarshal(row[cell.Alias], &n); err != nil {
				return nil, err
			}
			value.Samples[cell.Alias] = n
		}
		for _, key := range assignmentColumns() {
			if _, ok := value.Assignments[key]; !ok {
				return nil, fmt.Errorf("audit: oracle annual row missing assignment count %s", key)
			}
		}
		if countColumn == "edges" {
			if err := json.Unmarshal(row["self_loops"], &value.SelfLoops); err != nil {
				return nil, err
			}
			if err := json.Unmarshal(row["future_edges"], &value.Future); err != nil {
				return nil, err
			}
		}
		out[year] = value
	}
	return out, nil
}
func compareEvidence(directory string, run production.Report, report *Report, inventories ...[]results.Scenario) error {
	scenarios := auditScenarios("fractional")
	if len(inventories) > 0 {
		scenarios = inventories[0]
	}
	main, err := mainAuditScenario(scenarios)
	if err != nil {
		return err
	}
	var inventory []struct {
		Type     string `json:"type"`
		Group    string `json:"corpus_group"`
		Works    uint64 `json:"works"`
		Selected uint64 `json:"selected"`
	}
	if err := readJSON(filepath.Join(directory, "inventory.json"), &inventory); err != nil {
		return err
	}
	var raw, selected, core, xpac, unknown uint64
	selectedGroups := map[string]uint64{}
	selectedTypes := map[string][3]uint64{}
	blocked := map[string]bool{}
	for _, typ := range run.Config.Scope.Blacklist {
		blocked[typ] = true
	}
	var selectedBlacklisted uint64
	types := map[string][3]uint64{}
	for _, row := range inventory {
		if row.Selected > row.Works {
			return fmt.Errorf("audit: selected inventory exceeds raw type/corpus count")
		}
		if blocked[row.Type] {
			selectedBlacklisted += row.Selected
		}
		raw += row.Works
		selected += row.Selected
		selectedGroups[row.Group] += row.Selected
		group := 0
		switch row.Group {
		case "core":
			core += row.Works
		case "xpac":
			xpac += row.Works
			group = 1
		case "unknown":
			unknown += row.Works
			group = 2
		default:
			return fmt.Errorf("audit: unknown oracle corpus group")
		}
		vSelected := selectedTypes[row.Type]
		vSelected[group] += row.Selected
		selectedTypes[row.Type] = vSelected
		v := types[row.Type]
		v[group] += row.Works
		types[row.Type] = v
	}
	report.SelectedByCorpus = selectedGroups
	report.SelectedTypeCounts = selectedTypes
	report.check("Q/no_blacklisted_type_selected", 0, float64(selectedBlacklisted), false)
	report.check("raw/source_rows", float64(run.SourceRows), float64(raw), false)
	report.check("raw/inventory_rows", float64(run.Inventory.Rows), float64(raw), false)
	report.check("raw/core", float64(run.Inventory.Core.Works), float64(core), false)
	report.check("raw/xpac", float64(run.Inventory.XPAC.Works), float64(xpac), false)
	report.check("raw/unknown_xpac", float64(run.Inventory.NullXPAC.Works), float64(unknown), false)
	report.check("Q/type_blacklist_and_"+run.Config.Scope.Corpus, float64(run.SelectedWorks), float64(selected), false)
	if run.Config.Scope.Corpus == "core" {
		report.check("Q/core_no_selected_xpac", 0, float64(selectedGroups["xpac"]), false)
		report.check("Q/core_no_selected_unknown_xpac", 0, float64(selectedGroups["unknown"]), false)
		report.check("Q/core_selected_core", float64(run.SelectedWorks), float64(selectedGroups["core"]), false)
	}
	for typ, want := range run.Inventory.TypeCounts {
		got := types[typ]
		for group, label := range []string{"core", "xpac", "unknown"} {
			report.check("type/"+typ+"/"+label, float64(want[group]), float64(got[group]), false)
		}
		delete(types, typ)
	}
	for typ, got := range types {
		for group, label := range []string{"core", "xpac", "unknown"} {
			report.check("unexpected_type/"+typ+"/"+label, 0, float64(got[group]), false)
		}
	}
	var q []struct {
		Selected uint64 `json:"selected"`
		Unique   uint64 `json:"unique_ids"`
	}
	if err := readJSON(filepath.Join(directory, "q.json"), &q); err != nil {
		return err
	}
	if len(q) != 1 {
		return fmt.Errorf("audit: invalid Q evidence")
	}
	report.check("Q/materialized_selected", float64(run.SelectedWorks), float64(q[0].Selected), false)
	report.check("Q/unique_work_ids", float64(q[0].Selected), float64(q[0].Unique), false)
	publications, err := readAnnual(filepath.Join(directory, "publication_year.json"), "works")
	if err != nil {
		return err
	}
	citations, err := readAnnual(filepath.Join(directory, "citation_year.json"), "edges")
	if err != nil {
		return err
	}
	var pub, edges, loops, future uint64
	for _, y := range publications {
		pub += y.Count
	}
	for _, y := range citations {
		edges += y.Count
		loops += y.SelfLoops
		future += y.Future
	}
	report.check("publications/quality_publication_works", float64(run.Quality.PublicationWorks), float64(pub), false)
	report.check("publications/valid_citing_year_sources", float64(run.Graph.ValidYearSourceRecords), float64(pub), false)
	report.check("citations/retained_edges", float64(run.Graph.RetainedEdges), float64(edges), false)
	report.check("citations/quality_citation_events", float64(run.Quality.CitationEvents), float64(edges), false)
	report.check("citations/excluded_same_work_loops", float64(run.Graph.SelfLoops), float64(loops), false)
	report.check("citations/future_target_edges", float64(run.Graph.FutureTargetEdges), float64(future), false)
	var counts []struct {
		Cube       uint64 `json:"cube_rows"`
		Statistics uint64 `json:"statistics_rows"`
	}
	if err := readJSON(filepath.Join(directory, "output_counts.json"), &counts); err != nil {
		return err
	}
	if len(counts) != 1 {
		return fmt.Errorf("audit: invalid output count evidence")
	}
	report.check("output/cube_rows", float64(run.CubeRows), float64(counts[0].Cube), false)
	report.check("output/statistics_rows", float64(run.StatisticsRows), float64(counts[0].Statistics), false)
	type yearKey struct {
		source, scenario, scheme, level string
		year                            int
	}
	var outputYears []struct {
		Source       string  `json:"source"`
		Scenario     string  `json:"scenario_id"`
		Scheme       string  `json:"scheme"`
		Level        string  `json:"level"`
		Year         int     `json:"year"`
		Publications float64 `json:"publications"`
		Citations    float64 `json:"citations"`
	}
	if err := readJSON(filepath.Join(directory, "output_year.json"), &outputYears); err != nil {
		return err
	}
	yearRows := map[yearKey][2]float64{}
	for _, r := range outputYears {
		k := yearKey{r.Source, r.Scenario, r.Scheme, r.Level, r.Year}
		if _, exists := yearRows[k]; exists {
			return fmt.Errorf("audit: duplicate output annual group")
		}
		yearRows[k] = [2]float64{r.Publications, r.Citations}
	}
	for _, source := range []string{"cube", "statistics"} {
		selected := scenarios
		if source == "cube" {
			selected = []results.Scenario{main}
		}
		for _, scenario := range selected {
			for _, layer := range auditLayers {
				for year := 2000; year <= 2026; year++ {
					k := yearKey{source, scenario.ID, layer[0], layer[1], year}
					got := yearRows[k]
					delete(yearRows, k)
					prefix := fmt.Sprintf("annual/%s/%s/%s/%s/%d/", source, scenario.ID, layer[0], layer[1], year)
					expected := func(a annual) float64 {
						if scenario.CountryMode == "fractional" && (scenario.ConceptMode == "fractional" || layer[0] == "topics") {
							return float64(a.Count)
						}
						column := "country_assignments"
						if layer[0] == "concepts" && scenario.ConceptMode == "whole" {
							level := 0
							if layer[1] == "1" {
								level = 1
							}
							column = conceptColumn(level, scenario.ConceptThreshold) + "_assignments"
							if scenario.CountryMode == "whole" {
								column = "country_" + column
							}
						}
						return float64(a.Assignments[column])
					}
					// Whole-country/whole-Concept cells and sums are integers. Fractional
					// assignments use declared float tolerance even when their ideal sum is integer.
					fractional := scenario.CountryMode != "whole" || scenario.ConceptMode != "whole" && layer[0] == "concepts"
					report.check(prefix+"publications", expected(publications[year]), got[0], fractional)
					report.check(prefix+"citations", expected(citations[year]), got[1], fractional)
				}
			}
		}
	}

	if len(yearRows) != 0 {
		return fmt.Errorf("audit: %d unsupported output annual groups", len(yearRows))
	}
	var integrity []struct {
		InvalidWhole      uint64 `json:"invalid_whole_integers"`
		InvalidCounts     uint64 `json:"invalid_counts"`
		InvalidDimensions uint64 `json:"invalid_dimensions"`
		InvalidMetadata   uint64 `json:"invalid_scenario_metadata"`
		InvalidDictionary uint64 `json:"invalid_dictionary_labels"`
	}
	if err := readJSON(filepath.Join(directory, "output_integrity.json"), &integrity); err != nil {
		return err
	}
	if len(integrity) != 1 {
		return fmt.Errorf("audit: invalid output integrity evidence")
	}
	report.check("output/all_countrywhole_conceptwhole_cells_exact_nonnegative_integers", 0, float64(integrity[0].InvalidWhole), false)
	report.check("output/all_counts_finite_nonnegative", 0, float64(integrity[0].InvalidCounts), false)
	report.check("output/all_four_layer_dimensions_valid", 0, float64(integrity[0].InvalidDimensions), false)
	report.check("output/all_scenario_metadata_matches", 0, float64(integrity[0].InvalidMetadata), false)
	report.check("output/all_four_layers_labels_match_fixed_dictionary", 0, float64(integrity[0].InvalidDictionary), false)
	type cellKey struct {
		source, country, scheme, level, discipline string
		year                                       int
	}
	var outputCells []struct {
		Source       string  `json:"source"`
		Country      string  `json:"country"`
		Scheme       string  `json:"scheme"`
		Level        string  `json:"level"`
		Discipline   string  `json:"discipline_id"`
		Year         int     `json:"year"`
		Publications float64 `json:"publications"`
		Citations    float64 `json:"citations"`
	}
	if err := readJSON(filepath.Join(directory, "output_cells.json"), &outputCells); err != nil {
		return err
	}
	cellRows := map[cellKey][2]float64{}
	for _, r := range outputCells {
		k := cellKey{r.Source, r.Country, r.Scheme, r.Level, r.Discipline, r.Year}
		if _, exists := cellRows[k]; exists {
			return fmt.Errorf("audit: duplicate output representative cell")
		}
		cellRows[k] = [2]float64{r.Publications, r.Citations}
	}
	for _, source := range []string{"cube", "statistics"} {
		for _, cell := range sampleCells {
			for year := 2000; year <= 2026; year++ {
				k := cellKey{source, cell.Country, cell.Scheme, cell.Level, cell.Discipline, year}
				got := cellRows[k]
				delete(cellRows, k)
				prefix := fmt.Sprintf("cell/%s/%s/%s/%d/", source, cell.Country, cell.Alias, year)
				report.check(prefix+"publications", publications[year].Samples[cell.Alias], got[0], true)
				report.check(prefix+"citations", citations[year].Samples[cell.Alias], got[1], true)
			}
		}
	}
	if len(cellRows) != 0 {
		return fmt.Errorf("audit: %d unsupported representative output cells", len(cellRows))
	}
	return nil
}

func assignmentColumns() []string {
	out := []string{"country_assignments"}
	for _, threshold := range []float64{0, .3, .5} {
		for _, level := range []int{0, 1} {
			c := conceptColumn(level, threshold) + "_assignments"
			out = append(out, c, "country_"+c)
		}
	}
	return out
}
