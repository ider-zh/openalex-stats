package web

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openalexstats/internal/checkpoint"
	"openalexstats/internal/complexityfilter"
	"openalexstats/internal/eci"
)

func TestFilteredModelsPreserveCountsAndBaselineAndUseGlobalRanks(t *testing.T) {
	h, err := New(fixture(t, false))
	if err != nil {
		t.Fatal(err)
	}
	s := h.(*server)
	d := s.data
	p := complexityfilter.Profile{ID: "p1000-c1000-x0-d1-v1", DisplayName: "fixture support", CountryMinPublications: 1000, CountryMinCitationEvents: 1000, MinDiversity: 1}
	key := modelKey{d.eciScenario, "topics", "field", "publications", 2000}
	model := eci.Result{Status: "ok", Countries: []string{"US", "CN", "GB"}, Disciplines: []string{"f1", "f2"}, ActiveCountries: []int{0, 2}, ActiveDisciplines: []int{0, 1}, ECI: []float64{-1, 1}, PCI: []float64{-1, 1}}
	ratio := .75
	decision := complexityfilter.CountryDecision{Country: "CN", WholePublications: 500, WholeCitations: 2000, Excluded: true, OriginalMass: 20, Reasons: []string{"country_min_publications"}}
	d.profiles = map[string]*profileDataset{p.ID: {info: profileInfo(p), runID: strings.Repeat("a", 64), models: map[string]map[modelKey]eci.Result{DefaultMethod: {key: model}}, diagnostics: map[modelKey]complexityfilter.Diagnostics{key: {Profile: p, Algorithm: complexityfilter.Algorithm, RetainedMassFraction: &ratio, Rounds: []complexityfilter.Round{{Round: 1}}}}, decisions: map[modelKey]map[string]complexityfilter.CountryDecision{key: {"CN": decision}}}}
	d.profileInfos = []ComplexityProfileInfo{profileInfo(complexityfilter.Profile{ID: baselineProfileID}), profileInfo(p)}
	d.defaultProfileID = p.ID
	q := params("eci")
	q.Set("complexity_profile", p.ID)
	r := series(t, h, q)
	if !r.ProfileApplies || r.ModelContext.ProfileID != p.ID || r.ProfileRunID != strings.Repeat("a", 64) || len(r.ModelContext.Years) != 1 {
		t.Fatal("profile context", r.ModelContext)
	}
	point := r.Series[0].Points[0]
	if point.Status != "filtered_country_publications" || point.Value != nil || point.Rank != nil || point.Support == nil || point.Support.WholePublications != 500 {
		t.Fatal("filtered country was assigned score/rank", point)
	}
	q.Set("countries", "US")
	one := series(t, h, q)
	q.Set("countries", "US,GB")
	two := series(t, h, q)
	if one.Series[0].Points[0].Rank == nil || *one.Series[0].Points[0].Rank != 2 || *two.Series[0].Points[0].Rank != 2 || one.Series[0].Points[0].RankUniverse != 2 {
		t.Fatal("selected-country reranking")
	}
	q = params("eci")
	q.Set("complexity_profile", baselineProfileID)
	r = series(t, h, q)
	closeValue(t, r.Series[0].Points[0].Value, 1.2)
	if r.ComplexityProfile.ID != baselineProfileID || len(r.ModelContext.Years) != 0 {
		t.Fatal("baseline model changed")
	}
	for _, metric := range []string{"publications", "citations", "share", "rca"} {
		q = params(metric)
		q.Set("complexity_profile", baselineProfileID)
		before := series(t, h, q)
		q.Set("complexity_profile", p.ID)
		after := series(t, h, q)
		if after.ProfileApplies || after.ProfileRunID != "" || len(after.ModelContext.Years) != 0 {
			t.Fatal("profile applied to basic statistics")
		}
		for i := range before.Series {
			for j := range before.Series[i].Points {
				a, b := before.Series[i].Points[j], after.Series[i].Points[j]
				if a.Value == nil {
					if b.Value != nil {
						t.Fatal("null changed")
					}
				} else if b.Value == nil || *a.Value != *b.Value {
					t.Fatal("basic count/denominator changed", metric, a, b)
				}
			}
		}
	}
	q = params("eci")
	q.Set("complexity_profile", p.ID)
	w := request(t, h, "/api/series.csv?"+q.Encode())
	rows, err := csv.NewReader(strings.NewReader(w.Body.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	indices := map[string]int{}
	for i, key := range rows[0] {
		indices[key] = i
	}
	if rows[1][indices["complexity_profile"]] != p.ID || rows[1][indices["profile_applies"]] != "true" || rows[1][indices["value"]] != "" || rows[1][indices["rank"]] != "" {
		t.Fatal("CSV lost profile/gap identity")
	}
	q.Set("complexity_profile", "")
	if request(t, h, "/api/series?"+q.Encode()).Code != 400 {
		t.Fatal("empty profile accepted")
	}
	q.Set("complexity_profile", "../other")
	if request(t, h, "/api/series?"+q.Encode()).Code != 400 {
		t.Fatal("unknown profile accepted")
	}
}

func TestProfileBundleSourceAndArtifactIdentityFailClosed(t *testing.T) {
	h, err := New(fixture(t, false))
	if err != nil {
		t.Fatal(err)
	}
	d := h.(*server).data
	jsonFile(t, d.root, "completion.json", map[string]any{"run_id": d.run.RunID})
	root := t.TempDir()
	jsonFile(t, root, "manifest.json", profileBundle{Version: "complexity-filter-bundle-v1", SourceRunID: "different-base"})
	if err = loadDatasetProfiles(d, root); err == nil {
		t.Fatal("wrong-country/base profile accepted")
	}
	file := filepath.Join(root, "models.json")
	if err = os.WriteFile(file, []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := checkpoint.Hash(file)
	if err != nil {
		t.Fatal(err)
	}
	a.Path = "models.json"
	if _, err = readProfileArtifact(root, a); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, []byte("[0]"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readProfileArtifact(root, a); err == nil {
		t.Fatal("tampered model read")
	}
	a.Path = "../escape.json"
	if _, err = readProfileArtifact(root, a); err == nil {
		t.Fatal("escaped profile artifact")
	}
}

func TestProfileMetadataAndDownloadsNeverFallBackToBaselineModel(t *testing.T) {
	h, err := New(fixture(t, false))
	if err != nil {
		t.Fatal(err)
	}
	s := h.(*server)
	d := s.data
	p := complexityfilter.Profile{ID: "p1000-c1000-x5-d3-v1", CountryMinPublications: 1000, CountryMinCitationEvents: 1000, CellMinSupport: 5, MinDiversity: 3}
	root := t.TempDir()
	jsonFile(t, root, "filtered.json", map[string]string{"source": "filtered"})
	a, err := checkpoint.Hash(filepath.Join(root, "filtered.json"))
	if err != nil {
		t.Fatal(err)
	}
	a.Path = "filtered.json"
	d.profiles = map[string]*profileDataset{p.ID: {info: profileInfo(p), root: root, runID: strings.Repeat("a", 64), downloads: profileDownloads{Methods: map[string]map[string]checkpoint.Artifact{DefaultMethod: {"eci.json": a}}}}}
	d.profileInfos = []ComplexityProfileInfo{profileInfo(complexityfilter.Profile{ID: baselineProfileID}), profileInfo(p)}
	d.defaultProfileID = p.ID
	w := request(t, h, "/download?file=eci.json&complexity_profile="+p.ID)
	if w.Code != 200 || w.Header().Get("X-Artifact-SHA256") != a.SHA256 || w.Header().Get("X-OpenAlex-Complexity-Profile") != p.ID {
		t.Fatal(w.Code, w.Header())
	}
	var value map[string]string
	if err = json.Unmarshal(w.Body.Bytes(), &value); err != nil || value["source"] != "filtered" {
		t.Fatal("baseline download was substituted")
	}
	if request(t, h, "/download?file=method-sensitivity.json&complexity_profile="+p.ID).Code != 400 {
		t.Fatal("original sensitivity substituted for filtered model")
	}
	if request(t, h, "/download?file=eci.json&complexity_profile=").Code != 400 {
		t.Fatal("empty download profile accepted")
	}
}
