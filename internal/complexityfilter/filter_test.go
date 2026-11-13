package complexityfilter

import (
	"math"
	"reflect"
	"testing"

	"openalexstats/internal/eci"
)

func fixture(values [][]float64) Input {
	countries := []string{"A", "B", "C", "D"}[:len(values)]
	disciplines := []string{"x", "y", "z", "w"}[:len(values[0])]
	p, c := make([]uint64, len(values)), make([]uint64, len(values))
	for i := range p {
		p[i], c[i] = 1000, 10000
	}
	return Input{Matrix: eci.Matrix{Countries: countries, Disciplines: disciplines, Values: values}, Metric: "publications", WholePublications: p, WholeCitations: c}
}

func TestZeroProfileExactlyReusesProduction(t *testing.T) {
	in := fixture([][]float64{{10, 1, 1}, {5, 5, 1}, {1, 5, 10}})
	got, err := Compute(in, Profile{ID: "unfiltered"})
	if err != nil {
		t.Fatal(err)
	}
	want, err := eci.Compute(in.Matrix)
	if err != nil {
		t.Fatal(err)
	}
	wantJava, wantDiag, err := eci.ReflectionsFromPrepared(want, eci.DefaultReflectionConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Matrix, in.Matrix) || !reflect.DeepEqual(got.Spectral, want) || !reflect.DeepEqual(got.Reflections, wantJava) || !reflect.DeepEqual(got.ReflectionDiagnostics, wantDiag) {
		t.Fatal("zero thresholds changed production matrices, models or diagnostics")
	}
	if got.Diagnostics.RetainedMassFraction == nil || *got.Diagnostics.RetainedMassFraction != 1 {
		t.Fatal("baseline mass not conserved")
	}
	got.Matrix.Values[0][0] = 42
	if in.Matrix.Values[0][0] != 10 {
		t.Fatal("filter aliases immutable input")
	}
}

func TestCountryRemovalRecomputesRCAMargins(t *testing.T) {
	in := fixture([][]float64{{10, 1}, {1, 10}, {100, 1}})
	in.WholePublications[2] = 9
	got, err := Compute(in, Profile{ID: "support", CountryMinPublications: 10})
	if err != nil {
		t.Fatal(err)
	}
	if got.Diagnostics.FinalMass != 22 || got.Diagnostics.CountryScaleRemoved != 1 {
		t.Fatal("removed national mass remains in matrix")
	}
	want := (10.0 / 11.0) / (11.0 / 22.0)
	if got.Spectral.RCA[0][0] != want {
		t.Fatalf("RCA retained an excluded country's denominator: %g want%g", got.Spectral.RCA[0][0], want)
	}
	if in.Matrix.Values[2][0] != 100 {
		t.Fatal("source matrix mutated")
	}
}

func TestCellSupportZeroesXBeforeRCA(t *testing.T) {
	in := fixture([][]float64{{10, 1, 1}, {5, 5, 1}, {1, 5, 10}})
	got, err := Compute(in, Profile{ID: "cell", CellMinSupport: 2})
	if err != nil {
		t.Fatal(err)
	}
	wantX := eci.Matrix{Countries: in.Matrix.Countries, Disciplines: in.Matrix.Disciplines, Values: [][]float64{{10, 0, 0}, {5, 5, 0}, {0, 5, 10}}}
	want, err := eci.Compute(wantX)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Spectral, want) || got.Diagnostics.CellSupportRemovedCells != 4 {
		t.Fatal("cell gate did not recompute complete RCA from zeroed cells")
	}
	if got.Diagnostics.FinalMass != 35 {
		t.Fatalf("removed cell mass was reassigned: %g", got.Diagnostics.FinalMass)
	}
	if !reflect.DeepEqual(got.Spectral.Advantage, got.Reflections.Advantage) || !reflect.DeepEqual(got.Spectral.RCA, got.Reflections.RCA) {
		t.Fatal("methods did not share final RCA/M")
	}
	wantJava, _, err := eci.ReflectionsFromPrepared(want, eci.DefaultReflectionConfig())
	if err != nil || !reflect.DeepEqual(got.Reflections, wantJava) {
		t.Fatal("PCI/finite reflections used another network")
	}
}

func TestDiversityPruningReachesMonotoneFixedPoint(t *testing.T) {
	in := fixture([][]float64{{11, 8, 5}, {8, 11, 7}, {7, 2, 5}, {4, 6, 6}})
	got, err := Compute(in, Profile{ID: "diversity", MinDiversity: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Diagnostics.Rounds) != 3 || !reflect.DeepEqual(got.Diagnostics.Rounds[0].RemovedCountries, []string{"A", "B"}) || !reflect.DeepEqual(got.Diagnostics.Rounds[1].RemovedCountries, []string{"C"}) || len(got.Diagnostics.Rounds[2].RemovedCountries) != 0 {
		t.Fatalf("unexpected fixed-point sequence: %+v", got.Diagnostics.Rounds)
	}
	if got.Diagnostics.DiversityRemoved != 3 || got.Diagnostics.Countries[3].FinalDiversity != 3 {
		t.Fatal("final RCA was not recomputed after simultaneous row removal")
	}
	if got.Spectral.Status != "empty" || got.Reflections.Status != "empty" || len(got.Spectral.ECI) != 0 || len(got.Reflections.PCI) != 0 {
		t.Fatal("one surviving country was given fabricated comparable scores")
	}
}

func TestDisconnectedNetworkHasNoGlobalScores(t *testing.T) {
	got, err := Compute(fixture([][]float64{{10, 1}, {1, 10}}), Profile{ID: "disconnected", CellMinSupport: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []eci.Result{got.Spectral, got.Reflections} {
		if r.Status != "disconnected" || len(r.ECI) != 0 || len(r.PCI) != 0 {
			t.Fatal("disconnected components got global scores")
		}
	}
}

func TestInclusiveMinimumsAndRCABoundary(t *testing.T) {
	in := fixture([][]float64{{2, 2}, {2, 2}})
	in.WholePublications = []uint64{10, 10}
	in.WholeCitations = []uint64{20, 20}
	in.Metric = "citations"
	got, err := Compute(in, Profile{ID: "boundary", CountryMinPublications: 10, CountryMinCitationEvents: 20, CellMinSupport: 2, MinDiversity: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range got.Spectral.RCA {
		for _, r := range row {
			if r != 1 {
				t.Fatal("RCA=1 boundary changed")
			}
		}
	}
	if got.Diagnostics.FinalMass != 8 || got.Diagnostics.CountryScaleRemoved != 0 || got.Diagnostics.DiversityRemoved != 0 {
		t.Fatal("exactly equal minimum was excluded")
	}
}

func TestCitationEventGateUsesCitationEvents(t *testing.T) {
	in := fixture([][]float64{{10, 1}, {1, 10}})
	in.WholePublications = []uint64{0, 5}
	in.WholeCitations = []uint64{100, 200}
	in.Metric = "citations"
	got, err := Compute(in, Profile{ID: "events", CountryMinCitationEvents: 100})
	if err != nil || got.Diagnostics.CountryScaleRemoved != 0 {
		t.Fatal("citation gate silently used publication sample instead")
	}
	got, err = Compute(in, Profile{ID: "both", CountryMinCitationEvents: 100, CountryMinPublications: 1})
	if err != nil || got.Diagnostics.CountryScaleRemoved != 1 {
		t.Fatal("distinct publication and event gates not respected")
	}
	in.Metric = "publications"
	got, err = Compute(in, Profile{ID: "publication-basis", CountryMinCitationEvents: 1000000})
	if err != nil || got.Diagnostics.CountryScaleRemoved != 0 {
		t.Fatal("publication input was incorrectly subject to citation-event gate")
	}
}

func TestLightweightDegreesMatchProductionRCABoundary(t *testing.T) {
	for _, values := range [][][]float64{
		{{1.0 / 3, 1.0 / 3}, {2.0 / 3, 2.0 / 3}},
		{{1, 1 + 1e-13}, {1 + 1e-13, 1}},
		{{0, 0, 0}, {1e-6, 30, .5}, {3, 20, .75}},
	} {
		in := fixture(values)
		r, err := eci.Compute(in.Matrix)
		if err != nil {
			t.Fatal(err)
		}
		for i, want := range r.Advantage {
			if rcaDegrees(in.Matrix)[i] != degree(want) {
				t.Fatal("lightweight boundary does not match production")
			}
		}
	}
}

func TestInvalidRawDataCannotHideBehindFilters(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), -1} {
		in := fixture([][]float64{{value, 1}, {2, 2}})
		in.WholePublications[0] = 0
		if _, err := Compute(in, Profile{ID: "bad", CountryMinPublications: 1}); err == nil {
			t.Fatal("invalid excluded source data accepted")
		}
	}
	in := fixture([][]float64{{1, 2}, {2, 1}})
	if _, err := Compute(in, Profile{ID: "bad", CellMinSupport: math.NaN()}); err == nil {
		t.Fatal("NaN threshold accepted")
	}
	in.WholeCitations = nil
	if _, err := Compute(in, Profile{ID: "bad"}); err == nil {
		t.Fatal("missing national source counts accepted")
	}
}
