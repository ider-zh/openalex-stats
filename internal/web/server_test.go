package web

import (
	"encoding/csv"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/aggregate"
	"openalexstats/internal/eci"
	"openalexstats/internal/results"
)

func jsonFile(t *testing.T, root, name string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, name), b, 0600); err != nil {
		t.Fatal(err)
	}
}
func parquetFixture[T any](t *testing.T, path string, rows []T) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := parquet.NewGenericWriter[T](f)
	if _, err := w.Write(rows); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T, derived bool) string {
	t.Helper()
	root := t.TempDir()
	jsonFile(t, root, "run.json", map[string]any{
		"status": "full_snapshot_complete", "selected_works": 20,
		"config": map[string]any{"year_start": 2000, "year_end": 2026,
			"input":     map[string]any{"snapshot_date": "2026-06-26"},
			"scope":     map[string]any{"corpus": "core", "blacklist": []string{"dataset"}},
			"weighting": map[string]any{"country_mode": "fractional", "concept_mode": "fractional", "concept_threshold": .3}},
		"quality": map[string]any{"publication_works": 20, "missing_country_publications": 1},
		"graph":   map[string]any{"retained_edges": 25},
	})
	rows := []aggregate.Row{
		{Country: "CN", Year: 2000, Scheme: "topics", Level: "field", DisciplineID: "f1", Publications: 2, Citations: 4},
		{Country: "CN", Year: 2000, Scheme: "topics", Level: "field", DisciplineID: "f2", Publications: 2},
		{Country: "US", Year: 2000, Scheme: "topics", Level: "field", DisciplineID: "f1", Publications: 6, Citations: 12},
		{Country: "US", Year: 2000, Scheme: "topics", Level: "field", DisciplineID: "f2", Citations: 4},
		{Country: aggregate.Unknown, Year: 2000, Scheme: "topics", Level: "field", DisciplineID: "f1", Publications: 100, Citations: 100},
		{Country: "CN", Year: 2000, Scheme: "topics", Level: "field", DisciplineID: aggregate.Unknown, Publications: 20, Citations: 50},
		{Country: "CN", Year: 2001, Scheme: "topics", Level: "field", DisciplineID: "f2", Publications: 3},
		{Country: "CN", Year: 2026, Scheme: "topics", Level: "field", DisciplineID: "f1", Publications: 1, Citations: 2},
		{Country: "CN", Year: 2000, Scheme: "topics", Level: "subfield", DisciplineID: "s1", Publications: 2, Citations: 4},
		{Country: "CN", Year: 2000, Scheme: "concepts", Level: "0", DisciplineID: "c0", Publications: 2, Citations: 4},
		{Country: "CN", Year: 2000, Scheme: "concepts", Level: "1", DisciplineID: "c1", Publications: 2, Citations: 4},
	}
	if derived {
		main := results.Scenario{ID: "fractional-concept-0.3", CountryMode: "fractional", ConceptMode: "fractional", ConceptThreshold: .3, Main: true}
		whole := results.Scenario{ID: "whole-concept-0.3", CountryMode: "whole", ConceptMode: "fractional", ConceptThreshold: .3, Main: true}
		wholeRows := append([]aggregate.Row(nil), rows...)
		for i := range wholeRows {
			wholeRows[i].Publications *= 2
			wholeRows[i].Citations *= 2
		}
		derivedRows, _, err := results.Derive([]results.Input{{Scenario: main, Rows: rows}, {Scenario: whole, Rows: wholeRows}})
		if err != nil {
			t.Fatal(err)
		}
		parquetFixture(t, filepath.Join(root, "statistics.parquet"), derivedRows)
		jsonFile(t, root, "scenarios.json", []results.Scenario{main, whole})
	} else {
		parquetFixture(t, filepath.Join(root, "cube.parquet"), rows)
	}
	jsonFile(t, root, "dictionary.json", Dictionary{Countries: []Label{{ID: "CN", DisplayName: "中国"}, {ID: "US", DisplayName: "美国"}, {ID: "GB", DisplayName: "英国"}}, Layers: []Layer{
		{Scheme: "topics", Level: "field", Labels: []Label{{ID: "f1", DisplayName: "生物学"}, {ID: "f2", DisplayName: "物理学"}, {ID: "empty", DisplayName: "无观测学科"}}},
		{Scheme: "topics", Level: "subfield", Labels: []Label{{ID: "s1", DisplayName: "细胞生物学", ParentIDs: []string{"f1"}}}},
		{Scheme: "concepts", Level: "0", Labels: []Label{{ID: "c0", DisplayName: "Biology"}}},
		{Scheme: "concepts", Level: "1", Labels: []Label{{ID: "c1", DisplayName: "Genetics", ParentIDs: []string{"c0"}}}},
	}})
	model := eci.Result{Status: "ok", Countries: []string{"US", "CN", "GB"}, Disciplines: []string{"f2", "f1"}, ActiveCountries: []int{1, 0}, ActiveDisciplines: []int{1, 0}, ECI: []float64{1.2, -1.2}, PCI: []float64{.7, -.7}}
	jsonFile(t, root, "eci.json", []modelResult{
		{Year: 2000, Scheme: "topics", Level: "field", Metric: "publications", Result: model},
		{Year: 2001, Scheme: "topics", Level: "field", Metric: "publications", Result: eci.Result{Status: "disconnected"}},
		{Year: 2002, Scheme: "topics", Level: "field", Metric: "publications", Result: eci.Result{Status: "degenerate"}},
	})
	return root
}

func request(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}
func series(t *testing.T, handler http.Handler, values url.Values) Response {
	t.Helper()
	w := request(t, handler, "/api/series?"+values.Encode())
	if w.Code != http.StatusOK {
		t.Fatalf("series status %d: %s", w.Code, w.Body.String())
	}
	var response Response
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}
func params(metric string) url.Values {
	return url.Values{"scheme": {"topics"}, "level": {"field"}, "discipline": {"f1"}, "countries": {"CN,US"}, "metric": {metric}, "start": {"2000"}, "end": {"2002"}}
}
func closeValue(t *testing.T, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-12 {
		t.Fatalf("got %v want %g", got, want)
	}
}

func TestRealCountsSharesRCAAndSparseZeros(t *testing.T) {
	handler, err := New(fixture(t, false))
	if err != nil {
		t.Fatal(err)
	}
	pub := series(t, handler, params("publications"))
	closeValue(t, pub.Series[0].Points[0].Value, 2)
	closeValue(t, pub.Series[1].Points[0].Value, 6)
	closeValue(t, pub.Series[0].Points[1].Value, 0)
	cit := series(t, handler, params("citations"))
	closeValue(t, cit.Series[0].Points[0].Value, 4)
	if cit.Attribution != "country_and_discipline_of_cited_work" {
		t.Fatal("citations attributed to wrong endpoint")
	}
	share := series(t, handler, params("share"))
	closeValue(t, share.Series[0].Points[0].Value, .25)
	closeValue(t, share.Series[1].Points[0].Value, .75)
	if share.Series[0].Points[1].Value != nil || share.Series[0].Points[1].Status != "no_denominator" {
		t.Fatal("zero denominator must remain undefined")
	}
	rca := series(t, handler, params("rca"))
	closeValue(t, rca.Series[0].Points[0].Value, .625)
	closeValue(t, rca.Series[1].Points[0].Value, 1.25)
	p := params("rca")
	p.Set("basis", "citations")
	rcaC := series(t, handler, p)
	closeValue(t, rcaC.Series[0].Points[0].Value, 1.25)
	closeValue(t, rcaC.Series[1].Points[0].Value, .9375)
	// Known countries with no stored rows remain selectable: counts are zero,
	// while RCA cannot be defined for their empty country portfolio.
	p = params("publications")
	p.Set("countries", "GB")
	closeValue(t, series(t, handler, p).Series[0].Points[0].Value, 0)
	p.Set("metric", "rca")
	if series(t, handler, p).Series[0].Points[0].Value != nil {
		t.Fatal("empty portfolio incorrectly received RCA zero")
	}
}

func TestCustomMainScenarioOwnsComplexity(t *testing.T) {
	root := fixture(t, true)
	path := filepath.Join(root, "statistics.parquet")
	rows, err := parquet.ReadFile[results.Row](path)
	if err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		if rows[i].ScenarioID == "fractional-concept-0.3" {
			rows[i].ScenarioID = "custom-main-04"
			rows[i].ConceptThreshold = .4
		}
	}
	parquetFixture(t, path, rows)
	jsonFile(t, root, "scenarios.json", []results.Scenario{{ID: "whole-concept-0.3", CountryMode: "whole", ConceptMode: "fractional", ConceptThreshold: .3, Main: true}, {ID: "custom-main-04", CountryMode: "fractional", ConceptMode: "fractional", ConceptThreshold: .4, Main: true}})
	var models []modelResult
	bytes, err := os.ReadFile(filepath.Join(root, "eci.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(bytes, &models); err != nil {
		t.Fatal(err)
	}
	for i := range models {
		models[i].ScenarioID = "custom-main-04"
	}
	jsonFile(t, root, "eci.json", models)
	d, err := loadDataset(root)
	if err != nil {
		t.Fatal(err)
	}
	if d.eciScenario != "custom-main-04" || d.scenarios[0].ID != "custom-main-04" {
		t.Fatal("custom ECI owner not selected", d.eciScenario, d.scenarios)
	}
	handler, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	response := series(t, handler, params("eci"))
	if response.Query.Scenario != "custom-main-04" {
		t.Fatal("wrong default scenario", response.Query)
	}
	closeValue(t, response.Series[0].Points[0].Value, 1.2)
}

func TestConceptHitWholeCountingAndLegacyCompatibility(t *testing.T) {
	root := fixture(t, false)
	main := results.Scenario{ID: "fractional-concept-whole-0.3", CountryMode: "fractional", ConceptMode: "whole", ConceptThreshold: .3, Main: true}
	whole := results.Scenario{ID: "whole-concept-whole-0.3", CountryMode: "whole", ConceptMode: "whole", ConceptThreshold: .3, Main: true}
	// Hand-counted: two works hit Biology and another same-level label. One
	// belongs to CN+US; the second belongs to CN. Their citation counts are 2,1.
	// Each concept receives the entire work, while country fractions still apply.
	var rows []results.Row
	for _, s := range []results.Scenario{main, whole} {
		for _, country := range []string{"CN", "US"} {
			pub, cit := 1.5, 2.0
			if country == "US" {
				pub, cit = .5, 1
			}
			if s.CountryMode == "whole" {
				pub, cit = 2, 3
				if country == "US" {
					pub, cit = 1, 2
				}
			}
			for _, label := range []string{"c0", "other"} {
				rows = append(rows, results.Row{ScenarioID: s.ID, CountryMode: s.CountryMode, ConceptMode: s.ConceptMode, ConceptThreshold: s.ConceptThreshold, Main: true, Country: country, Year: 2000, Scheme: "concepts", Level: "0", DisciplineID: label, Publications: pub, Citations: cit})
			}
			rows = append(rows, results.Row{ScenarioID: s.ID, CountryMode: s.CountryMode, ConceptMode: s.ConceptMode, ConceptThreshold: s.ConceptThreshold, Main: true, Country: country, Year: 2000, Scheme: "topics", Level: "field", DisciplineID: "f1", Publications: pub, Citations: cit})
		}
	}
	parquetFixture(t, filepath.Join(root, "statistics.parquet"), rows)
	jsonFile(t, root, "scenarios.json", []results.Scenario{whole, main})
	jsonFile(t, root, "eci.json", []modelResult{{ScenarioID: main.ID, Year: 2000, Scheme: "concepts", Level: "0", Metric: "publications", Result: eci.Result{Status: "ok", Countries: []string{"CN", "US"}, Disciplines: []string{"c0", "other"}, ActiveCountries: []int{0, 1}, ActiveDisciplines: []int{0, 1}, ECI: []float64{1, -1}, PCI: []float64{.7, -.7}}}})
	handler, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	p := params("publications")
	p.Set("scheme", "concepts")
	p.Set("level", "0")
	p.Set("discipline", "c0")
	r := series(t, handler, p)
	if r.Query.Scenario != main.ID || r.Counting.ConceptMode != "whole" || r.Counting.PublicationCountsInteger || !r.Counting.ConceptThresholdApplies {
		t.Fatal("wrong concept counting contract", r)
	}
	closeValue(t, r.Series[0].Points[0].Value, 1.5)
	closeValue(t, r.Series[1].Points[0].Value, .5)
	if !strings.Contains(r.Definition, "每个命中标签记 1") || !strings.Contains(r.Definition, "仍可为小数") {
		t.Fatal("missing unambiguous counting definition", r.Definition)
	}
	p.Set("discipline", "other")
	other := series(t, handler, p)
	closeValue(t, other.Series[0].Points[0].Value, 1.5)
	p.Set("discipline", "c0")
	p.Set("scenario", whole.ID)
	r = series(t, handler, p)
	closeValue(t, r.Series[0].Points[0].Value, 2)
	closeValue(t, r.Series[1].Points[0].Value, 1)
	if !r.Counting.PublicationCountsInteger || !strings.Contains(r.Definition, "整数计数") {
		t.Fatal("whole countries/concepts must expose integer semantics", r)
	}
	p.Set("metric", "citations")
	r = series(t, handler, p)
	closeValue(t, r.Series[0].Points[0].Value, 3)
	closeValue(t, r.Series[1].Points[0].Value, 2)
	p.Set("metric", "eci")
	p.Set("scenario", main.ID)
	closeValue(t, series(t, handler, p).Series[0].Points[0].Value, 1)
	p = params("publications")
	r = series(t, handler, p)
	if r.Counting.ConceptThresholdApplies || !strings.Contains(r.Definition, "Concepts 阈值不适用") {
		t.Fatal("Topics must not imply concept threshold filtering", r)
	}
	legacy, err := New(fixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	p = params("publications")
	p.Set("scheme", "concepts")
	p.Set("level", "0")
	p.Set("discipline", "c0")
	r = series(t, legacy, p)
	if r.Query.Scenario != "fractional-concept-0.3" || r.Counting.ConceptMode != "fractional" || !strings.Contains(r.Definition, "旧口径") {
		t.Fatal("legacy dataset was reinterpreted", r)
	}
	closeValue(t, r.Series[0].Points[0].Value, 2)
}

func TestComplexityAttributionActiveIndicesAndGaps(t *testing.T) {
	handler, err := New(fixture(t, false))
	if err != nil {
		t.Fatal(err)
	}
	p := params("eci")
	r := series(t, handler, p)
	closeValue(t, r.Series[0].Points[0].Value, 1.2)
	closeValue(t, r.Series[1].Points[0].Value, -1.2)
	if r.Series[0].Points[1].Value != nil || r.Series[0].Points[1].Status != "disconnected" || r.Series[0].Points[2].Value != nil || r.Series[0].Points[2].Status != "degenerate" {
		t.Fatal("invalid model scores must be gaps")
	}
	p.Set("discipline", "f2")
	other := series(t, handler, p)
	closeValue(t, other.Series[0].Points[0].Value, 1.2)
	if other.Attribution != "country_over_all_disciplines_in_layer" {
		t.Fatal("ECI must be independent of selected discipline")
	}
	p.Set("metric", "pci")
	p.Set("discipline", "f1")
	r = series(t, handler, p)
	if len(r.Series) != 1 || r.Series[0].ID != "GLOBAL" || r.Attribution != "global_discipline" {
		t.Fatal("PCI duplicated into country curves")
	}
	closeValue(t, r.Series[0].Points[0].Value, .7)
}

func TestUnstableValidScoresAreWarningsNotFabricatedGaps(t *testing.T) {
	root := fixture(t, false)
	jsonFile(t, root, "eci.json", []modelResult{{Year: 2000, Scheme: "topics", Level: "field", Metric: "publications", Result: eci.Result{Status: "ok", Unstable: true, Countries: []string{"CN", "US"}, Disciplines: []string{"f1"}, ActiveCountries: []int{0, 1}, ActiveDisciplines: []int{0}, ECI: []float64{1, -1}, PCI: []float64{.5}}}})
	handler, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	r := series(t, handler, params("eci"))
	closeValue(t, r.Series[0].Points[0].Value, 1)
	if r.Series[0].Points[0].Status != "unstable" {
		t.Fatal("unstable valid model lost its warning")
	}
}

func TestGlobalModelCompetitionRanksAndCSV(t *testing.T) {
	root := fixture(t, false)
	model := eci.Result{Status: "ok", Countries: []string{"CN", "US", "GB"}, ActiveCountries: []int{0, 1, 2}, ECI: []float64{.5, 2, 2}, Disciplines: []string{"f1", "f2", "empty"}, ActiveDisciplines: []int{0, 1, 2}, PCI: []float64{-.5, 1, 1}}
	jsonFile(t, root, "eci.json", []modelResult{{Year: 2000, Scheme: "topics", Level: "field", Metric: "publications", Result: model}, {Year: 2001, Scheme: "topics", Level: "field", Metric: "publications", Result: eci.Result{Status: "disconnected"}}})
	handler, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	p := params("eci")
	p.Set("countries", "CN")
	r := series(t, handler, p)
	point := r.Series[0].Points[0]
	if point.Rank == nil || *point.Rank != 3 || point.RankUniverse != 3 {
		t.Fatal("rank wrongly computed only among selected countries", point)
	}
	if r.Series[0].Points[1].Rank != nil || r.Series[0].Points[1].Value != nil {
		t.Fatal("invalid model invented rank", r)
	}
	p.Set("countries", "US,GB,CN")
	r = series(t, handler, p)
	for i, want := range []int{1, 1, 3} {
		if r.Series[i].Points[0].Rank == nil || *r.Series[i].Points[0].Rank != want {
			t.Fatal("competition ties/selection changed global rank", r)
		}
	}
	p.Set("countries", "CN")
	p.Set("metric", "pci")
	r = series(t, handler, p)
	if len(r.Series) != 1 || r.Series[0].Points[0].Rank == nil || *r.Series[0].Points[0].Rank != 3 || r.Series[0].Points[0].RankUniverse != 3 {
		t.Fatal("PCI not globally ranked over disciplines", r)
	}
	w := request(t, handler, "/api/series.csv?"+p.Encode())
	records, e := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	if e != nil {
		t.Fatal(e)
	}
	if records[0][12] != "rank" || records[0][13] != "rank_universe" || records[1][8] != "-0.5" || records[1][12] != "3" || records[1][13] != "3" {
		t.Fatal("CSV lost original score or global rank", records)
	}
}

func TestDerivedScenariosAndPartialYear(t *testing.T) {
	handler, err := New(fixture(t, true))
	if err != nil {
		t.Fatal(err)
	}
	p := params("publications")
	p.Set("scenario", "whole-concept-0.3")
	closeValue(t, series(t, handler, p).Series[0].Points[0].Value, 4)
	p.Set("metric", "eci")
	r := series(t, handler, p)
	if r.Series[0].Points[0].Value != nil || r.Series[0].Points[0].Status != "model_not_available" {
		t.Fatal("non-model scenario reused another scenario's ECI")
	}
	p.Set("scenario", "fractional-concept-0.3")
	closeValue(t, series(t, handler, p).Series[0].Points[0].Value, 1.2)
	p.Set("metric", "citations")
	p.Set("start", "2026")
	p.Set("end", "2026")
	r = series(t, handler, p)
	if !r.Series[0].Points[0].Partial {
		t.Fatal("2026 must be marked partial for June snapshot")
	}
	closeValue(t, r.Series[0].Points[0].Value, 2)
}

func TestInvalidParametersAndDownloadTraversal(t *testing.T) {
	handler, err := New(fixture(t, false))
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range [][2]string{{"metric", "fake"}, {"scheme", "unknown"}, {"level", "domain"}, {"discipline", "missing"}, {"countries", "ZZ"}, {"start", "1999"}, {"end", "2027"}, {"basis", "bogus"}, {"scenario", "../../"}, {"extra", "1"}} {
		p := params("publications")
		p.Set(change[0], change[1])
		if w := request(t, handler, "/api/series?"+p.Encode()); w.Code != http.StatusBadRequest {
			t.Errorf("accepted invalid %v: %d", change, w.Code)
		}
	}
	for _, path := range []string{"/download?file=../run.json", "/download?file=%2e%2e%2frun.json", "/download?file=run.json&file=cube.parquet"} {
		if w := request(t, handler, path); w.Code != http.StatusBadRequest {
			t.Errorf("traversal/duplicate file accepted: %s (%d)", path, w.Code)
		}
	}
	if w := request(t, handler, "/static/../data.go"); w.Code != http.StatusNotFound {
		t.Fatal("arbitrary static paths exposed")
	}
	w := request(t, handler, "/download?file=cube.parquet")
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") || !strings.HasPrefix(w.Body.String(), "PAR1") {
		t.Fatal("real Parquet download unavailable")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/meta", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatal("unexpected write method accepted")
	}
}

func TestCSVAndEmbeddedOfflineAssets(t *testing.T) {
	handler, err := New(fixture(t, false))
	if err != nil {
		t.Fatal(err)
	}
	p := params("publications")
	w := request(t, handler, "/api/series.csv?"+p.Encode())
	rows, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	if err != nil || len(rows) != 7 || rows[1][8] != "2" || rows[1][6] != "CN" {
		t.Fatalf("CSV doesn't contain queried values: rows=%v err=%v", rows, err)
	}
	for _, path := range []string{"/", "/static/app.css", "/static/app.js", "/static/vendor/echarts.min.js"} {
		if w := request(t, handler, path); w.Code != http.StatusOK || w.Body.Len() == 0 {
			t.Fatalf("missing embedded asset %s", path)
		}
	}
	if w := request(t, handler, "/"); strings.Contains(w.Body.String(), "https://cdn") {
		t.Fatal("UI depends on runtime remote assets")
	}
}

func TestNotReadyAndSymlinkSafety(t *testing.T) {
	root := t.TempDir()
	handler, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	w := request(t, handler, "/api/meta")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ready":false`) {
		t.Fatal("missing results must have explicit not-ready state")
	}
	if w := request(t, handler, "/api/series?"+params("publications").Encode()); w.Code != http.StatusServiceUnavailable {
		t.Fatal("missing data received fabricated series")
	}
	outside := filepath.Join(t.TempDir(), "secret.json")
	if err := os.WriteFile(outside, []byte(`{"status":"complete"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "run.json")); err != nil {
		t.Skipf("symlink creation unavailable on this host: %v", err)
	}
	if _, err := New(root); err == nil {
		t.Fatal("symlink escaped the result directory")
	}
}
