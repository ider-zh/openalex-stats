package audit

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"openalexstats/internal/results"
)

const legacyMainScenario = "fractional-concept-0.3"

// The oracle pins its own supported inventories, independently of production
// defaults, so changing a default never silently changes historical validation.
func auditScenarios(conceptMode string) []results.Scenario {
	var out []results.Scenario
	for _, country := range []string{"fractional", "whole"} {
		for _, threshold := range []float64{0, .3, .5} {
			infix := "-concept-"
			if conceptMode == "whole" {
				infix = "-concept-whole-"
			}
			out = append(out, results.Scenario{ID: country + infix + strconv.FormatFloat(threshold, 'f', 1, 64), CountryMode: country, ConceptMode: conceptMode, ConceptThreshold: threshold, Main: threshold == .3})
		}
	}
	return out
}

func validateAuditScenarios(scenarios []results.Scenario) error {
	for _, mode := range []string{"fractional", "whole"} {
		expected := map[string]results.Scenario{}
		for _, s := range auditScenarios(mode) {
			expected[s.ID] = s
		}
		if len(scenarios) != len(expected) {
			continue
		}
		valid := true
		for _, s := range scenarios {
			if want, ok := expected[s.ID]; !ok || want != s {
				valid = false
				break
			}
			delete(expected, s.ID)
		}
		if valid && len(expected) == 0 {
			return nil
		}
	}
	return fmt.Errorf("audit: unsupported/duplicate scenario inventory; requires six standard country/threshold scenarios with consistently fractional or whole Concepts")
}

func mainAuditScenario(scenarios []results.Scenario) (results.Scenario, error) {
	var main results.Scenario
	count := 0
	seen := map[string]bool{}
	for _, s := range scenarios {
		if s.ID == "" || strings.TrimSpace(s.ID) != s.ID || seen[s.ID] || (s.CountryMode != "fractional" && s.CountryMode != "whole") || (s.ConceptMode != "fractional" && s.ConceptMode != "whole") || math.IsNaN(s.ConceptThreshold) || math.IsInf(s.ConceptThreshold, 0) || s.ConceptThreshold < 0 || s.ConceptThreshold > 1 {
			return main, fmt.Errorf("model audit: invalid scenario metadata")
		}
		seen[s.ID] = true
		if s.Main && s.CountryMode == "fractional" {
			main = s
			count++
		}
	}
	if count != 1 {
		return main, fmt.Errorf("model audit: expected one fractional-country main scenario, got %d", count)
	}
	return main, nil
}

func readMainAuditScenario(directory string) (results.Scenario, bool, error) {
	var scenarios []results.Scenario
	if err := readJSON(filepath.Join(directory, "scenarios.json"), &scenarios); err != nil {
		if os.IsNotExist(err) {
			return auditScenarios("fractional")[1], false, nil
		}
		return results.Scenario{}, false, err
	}
	main, err := mainAuditScenario(scenarios)
	return main, true, err
}
