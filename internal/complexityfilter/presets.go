package complexityfilter

import "fmt"

const BaselineProfileID = "baseline-v1"
const DefaultProfileID = "p1000-c1000-x5-d3-v1"

// Presets are disclosed engineering starting points, not scientifically
// validated universal capability thresholds. Counts remain unchanged; these
// profiles affect only the model's internally recomputed RCA/ECI/PCI.
func Presets() []Profile {
	out := []Profile{{ID: BaselineProfileID, DisplayName: "未筛选对照"}}
	for _, p := range []uint64{100, 1000, 5000} {
		for _, x := range []float64{0, 5, 20} {
			for _, d := range []int{1, 3} {
				out = append(out, Profile{ID: fmt.Sprintf("p%d-c1000-x%g-d%d-v1", p, x, d), DisplayName: fmt.Sprintf("P%d · C1000 · X%g · D%d", p, x, d), CountryMinPublications: p, CountryMinCitationEvents: 1000, CellMinSupport: x, MinDiversity: d})
			}
		}
	}
	return out
}
