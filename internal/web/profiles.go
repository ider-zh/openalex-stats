package web

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"openalexstats/internal/checkpoint"
	"openalexstats/internal/complexityfilter"
	"openalexstats/internal/eci"
)

const baselineProfileID = complexityfilter.BaselineProfileID
const profileDirectionPolicy = "baseline-pooled-common-year-sign-v1"

type ComplexityProfileInfo struct {
	complexityfilter.Profile
	Engine          string `json:"engine"`
	Baseline        bool   `json:"baseline"`
	DirectionPolicy string `json:"direction_policy"`
}
type ProfileSupport struct {
	WholePublications uint64 `json:"whole_publications"`
	WholeCitations    uint64 `json:"whole_citations"`
	FinalDiversity    int    `json:"final_diversity"`
}
type ProfileAnnualContext struct {
	Year                    int      `json:"year"`
	Status                  string   `json:"status"`
	EligibleCountryCount    int      `json:"eligible_country_count"`
	EligibleDisciplineCount int      `json:"eligible_discipline_count"`
	RetainedMassFraction    *float64 `json:"retained_mass_fraction"`
	ClosurePasses           int      `json:"closure_passes"`
	SourceMatrixSHA256      string   `json:"source_matrix_sha256"`
	FinalMatrixSHA256       string   `json:"final_matrix_sha256"`
	FinalNetworkSHA256      string   `json:"final_network_sha256"`
}
type ProfileModelContext struct {
	ProfileID              string                 `json:"profile_id"`
	ProfileRunID           string                 `json:"profile_run_id"`
	SourceRunID            string                 `json:"source_run_id"`
	SourceCompletionSHA256 string                 `json:"source_completion_sha256"`
	Algorithm              string                 `json:"algorithm"`
	DirectionPolicy        string                 `json:"direction_policy"`
	Years                  []ProfileAnnualContext `json:"years"`
}
type profileDiagnostic struct {
	Year        int                          `json:"year"`
	Scheme      string                       `json:"scheme"`
	Level       string                       `json:"level"`
	Metric      string                       `json:"metric"`
	Diagnostics complexityfilter.Diagnostics `json:"diagnostics"`
}
type profileDownloads struct {
	Methods map[string]map[string]checkpoint.Artifact `json:"methods"`
	Shared  map[string]checkpoint.Artifact            `json:"shared"`
}

func (d *profileDownloads) UnmarshalJSON(b []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		return err
	}
	d.Methods = map[string]map[string]checkpoint.Artifact{}
	for key, raw := range fields {
		if key == "methods" {
			if err := json.Unmarshal(raw, &d.Methods); err != nil {
				return err
			}
			continue
		}
		if key == "shared" {
			if err := json.Unmarshal(raw, &d.Shared); err != nil {
				return err
			}
			continue
		}
		var files map[string]checkpoint.Artifact
		if err := json.Unmarshal(raw, &files); err != nil {
			return err
		}
		d.Methods[key] = files
	}
	return nil
}

type profileEntry struct {
	Profile     complexityfilter.Profile       `json:"profile"`
	RunID       string                         `json:"profile_run_id"`
	Models      map[string]checkpoint.Artifact `json:"models"`
	Diagnostics checkpoint.Artifact            `json:"diagnostics"`
	Downloads   profileDownloads               `json:"downloads"`
}
type profileBundle struct {
	Version                string         `json:"version"`
	SourceRunID            string         `json:"source_run_id"`
	SourceCompletionSHA256 string         `json:"source_completion_sha256"`
	DefaultProfileID       string         `json:"default_profile_id"`
	Profiles               []profileEntry `json:"profiles"`
	DirectionPolicy        string         `json:"direction_policy"`
}
type profileDataset struct {
	info        ComplexityProfileInfo
	root        string
	runID       string
	models      map[string]map[modelKey]eci.Result
	diagnostics map[modelKey]complexityfilter.Diagnostics
	decisions   map[modelKey]map[string]complexityfilter.CountryDecision
	downloads   profileDownloads
}

func profileInfo(p complexityfilter.Profile) ComplexityProfileInfo {
	if p.ID == baselineProfileID {
		p.DisplayName = "不筛选 · 原模型"
		return ComplexityProfileInfo{Profile: p, Engine: "baseline-v1", Baseline: true, DirectionPolicy: "published-baseline-v1"}
	}
	return ComplexityProfileInfo{Profile: p, Engine: "support-filter-v1", DirectionPolicy: profileDirectionPolicy}
}
func (d *dataset) defaultComplexityProfile() string {
	if d.defaultProfileID != "" {
		return d.defaultProfileID
	}
	return baselineProfileID
}
func (d *dataset) complexityProfiles() []ComplexityProfileInfo {
	if len(d.profileInfos) > 0 {
		return d.profileInfos
	}
	return []ComplexityProfileInfo{profileInfo(complexityfilter.Profile{ID: baselineProfileID})}
}
func (d *dataset) profileInfo(id string) (ComplexityProfileInfo, bool) {
	for _, p := range d.complexityProfiles() {
		if p.ID == id {
			return p, true
		}
	}
	return ComplexityProfileInfo{}, false
}
func (d *dataset) profileModel(id, method string, key modelKey) (eci.Result, bool) {
	if id == baselineProfileID || id == "" {
		r, ok := d.methodModels[method][key]
		return r, ok
	}
	if p := d.profiles[id]; p != nil {
		r, ok := p.models[method][key]
		return r, ok
	}
	return eci.Result{}, false
}
func (d *dataset) profileCountry(id string, key modelKey, country string) (complexityfilter.CountryDecision, bool) {
	if p := d.profiles[id]; p != nil {
		r, ok := p.decisions[key][country]
		return r, ok
	}
	return complexityfilter.CountryDecision{}, false
}
func (d *dataset) profileContext(q Query, method MethodInfo) ProfileModelContext {
	out := ProfileModelContext{ProfileID: q.ComplexityProfile, SourceRunID: method.SourceRunID, SourceCompletionSHA256: method.SourceCompletionSHA256, Algorithm: "baseline-v1", DirectionPolicy: "published-baseline-v1", Years: []ProfileAnnualContext{}}
	if q.ComplexityProfile != baselineProfileID {
		out.Algorithm = complexityfilter.Algorithm
		out.DirectionPolicy = profileDirectionPolicy
	}
	if q.Metric != "eci" && q.Metric != "pci" {
		return out
	}
	if q.ComplexityProfile == baselineProfileID {
		out.ProfileRunID = d.run.RunID
		return out
	}
	p := d.profiles[q.ComplexityProfile]
	if p == nil {
		return out
	}
	out.ProfileRunID = p.runID
	for year := q.Start; year <= q.End; year++ {
		key := modelKey{q.Scenario, q.Scheme, q.Level, q.Basis, year}
		diag, exists := p.diagnostics[key]
		if !exists {
			continue
		}
		model, exists := p.models[q.Method][key]
		if !exists {
			continue
		}
		out.Years = append(out.Years, ProfileAnnualContext{Year: year, Status: model.Status, EligibleCountryCount: len(model.ActiveCountries), EligibleDisciplineCount: len(model.ActiveDisciplines), RetainedMassFraction: diag.RetainedMassFraction, ClosurePasses: len(diag.Rounds), SourceMatrixSHA256: diag.SourceMatrixSHA256, FinalMatrixSHA256: diag.FinalMatrixSHA256, FinalNetworkSHA256: diag.FinalNetworkSHA256})
	}
	return out
}

func publicFilterReasons(d complexityfilter.CountryDecision) []string {
	reasons := map[string]bool{}
	for _, reason := range d.Reasons {
		switch reason {
		case "country_min_publications":
			reasons["country_publications"] = true
		case "country_min_citation_events":
			reasons["country_citations"] = true
		case "min_diversity":
			reasons["min_diversity"] = true
		case "zero_matrix_support":
			if d.OriginalMass > 0 {
				reasons["cell_support"] = true
			} else {
				reasons["original_zero"] = true
			}
		}
	}
	var out []string
	for _, reason := range []string{"country_publications", "country_citations", "cell_support", "min_diversity", "original_zero"} {
		if reasons[reason] {
			out = append(out, reason)
		}
	}
	return out
}
func filteredStatus(reasons []string) string {
	for _, reason := range reasons {
		if reason != "original_zero" {
			return "filtered_" + reason
		}
	}
	return ""
}

func profilePath(root, name string) (string, error) {
	if name == "" || filepath.IsAbs(name) {
		return "", fmt.Errorf("profile artifact path must be relative")
	}
	path := filepath.Join(root, filepath.FromSlash(name))
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || (len(rel) > 3 && rel[:3] == ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("profile artifact escapes root")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	rel, err = filepath.Rel(root, real)
	if err != nil || rel == ".." || (len(rel) > 3 && rel[:3] == ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("profile artifact symlink escapes root")
	}
	return real, nil
}
func readProfileArtifact(root string, a checkpoint.Artifact) ([]byte, error) {
	p, err := profilePath(root, a.Path)
	if err != nil {
		return nil, err
	}
	actual, err := checkpoint.Hash(p)
	if err != nil {
		return nil, err
	}
	if actual.Bytes != a.Bytes || actual.SHA256 != a.SHA256 {
		return nil, fmt.Errorf("profile artifact checksum mismatch: %s", a.Path)
	}
	return os.ReadFile(p)
}

func loadDatasetProfiles(d *dataset, directory string) error {
	if directory == "" {
		return nil
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return err
	}
	var manifest profileBundle
	if err = json.Unmarshal(b, &manifest); err != nil {
		return err
	}
	completion, err := readSafe(d.root, "completion.json")
	if err != nil {
		return err
	}
	if manifest.Version != "complexity-filter-bundle-v1" || manifest.DirectionPolicy != profileDirectionPolicy || manifest.SourceRunID != d.run.RunID || manifest.SourceCompletionSHA256 != bytesSHA(completion) {
		return fmt.Errorf("profile bundle does not match base completion")
	}
	profiles := map[string]*profileDataset{}
	var infos []ComplexityProfileInfo
	validID := regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)
	for _, entry := range manifest.Profiles {
		if err = entry.Profile.Validate(); err != nil {
			return err
		}
		id := entry.Profile.ID
		if id == baselineProfileID && (entry.Profile.CountryMinPublications != 0 || entry.Profile.CountryMinCitationEvents != 0 || entry.Profile.CellMinSupport != 0 || entry.Profile.MinDiversity != 0) {
			return fmt.Errorf("baseline profile must have zero thresholds")
		}
		if !validID.MatchString(id) || profiles[id] != nil || len(entry.RunID) != 64 {
			return fmt.Errorf("invalid/duplicate profile identity")
		}
		p := &profileDataset{info: profileInfo(entry.Profile), root: root, runID: entry.RunID, models: map[string]map[modelKey]eci.Result{}, diagnostics: map[modelKey]complexityfilter.Diagnostics{}, decisions: map[modelKey]map[string]complexityfilter.CountryDecision{}, downloads: entry.Downloads}
		for _, method := range []string{DefaultMethod, eci.ReflectionsMethodID} {
			a, exists := entry.Models[method]
			if !exists {
				return fmt.Errorf("profile missing method %s", method)
			}
			data, err := readProfileArtifact(root, a)
			if err != nil {
				return err
			}
			var models []modelResult
			if err = json.Unmarshal(data, &models); err != nil {
				return err
			}
			loaded := map[modelKey]eci.Result{}
			for _, m := range models {
				scenario := m.ScenarioID
				if scenario == "" {
					scenario = d.eciScenario
				}
				key := modelKey{scenario, m.Scheme, m.Level, m.Metric, m.Year}
				if _, exists := loaded[key]; exists {
					return fmt.Errorf("duplicate profile model")
				}
				if err = validateProfileModel(d, m); err != nil {
					return err
				}
				loaded[key] = m.Result
			}
			if len(loaded) != (d.end-d.start+1)*4*2 {
				return fmt.Errorf("incomplete profile method: %s", method)
			}
			p.models[method] = loaded
		}
		data, err := readProfileArtifact(root, entry.Diagnostics)
		if err != nil {
			return err
		}
		var diagnostics []profileDiagnostic
		if err = json.Unmarshal(data, &diagnostics); err != nil {
			return err
		}
		for _, row := range diagnostics {
			key := modelKey{d.eciScenario, row.Scheme, row.Level, row.Metric, row.Year}
			if _, exists := p.diagnostics[key]; exists {
				return fmt.Errorf("duplicate profile diagnostic")
			}
			if row.Diagnostics.Profile != entry.Profile || row.Diagnostics.Algorithm != complexityfilter.Algorithm {
				return fmt.Errorf("profile diagnostic rules mismatch")
			}
			p.diagnostics[key] = row.Diagnostics
			p.decisions[key] = map[string]complexityfilter.CountryDecision{}
			for _, decision := range row.Diagnostics.Countries {
				if _, exists := p.decisions[key][decision.Country]; exists {
					return fmt.Errorf("duplicate country diagnostic")
				}
				p.decisions[key][decision.Country] = decision
			}
		}
		if len(p.diagnostics) != (d.end-d.start+1)*4*2 {
			return fmt.Errorf("incomplete profile diagnostics")
		}
		for _, downloads := range p.downloads.Methods {
			for _, a := range downloads {
				if _, err = readProfileArtifact(root, a); err != nil {
					return err
				}
			}
		}
		for _, a := range p.downloads.Shared {
			if _, err = readProfileArtifact(root, a); err != nil {
				return err
			}
		}
		profiles[id] = p
		infos = append(infos, p.info)
	}
	if profiles[baselineProfileID] == nil || profiles[manifest.DefaultProfileID] == nil {
		return fmt.Errorf("profile bundle missing baseline/default")
	}
	// Keep every original baseline score and global universe exactly as published.
	profiles[baselineProfileID].models = d.methodModels
	d.profiles = profiles
	d.profileInfos = infos
	d.defaultProfileID = manifest.DefaultProfileID
	return nil
}

func validateProfileModel(d *dataset, m modelResult) error {
	if m.ScenarioID != "" && m.ScenarioID != d.eciScenario {
		return fmt.Errorf("profile model is not in the declared main scenario")
	}
	if m.Year < d.start || m.Year > d.end || !validLayer(m.Scheme, m.Level) || (m.Metric != "publications" && m.Metric != "citations") {
		return fmt.Errorf("invalid profile model coordinates")
	}
	r := m.Result
	if r.Status != "ok" && r.Status != "empty" && r.Status != "disconnected" && r.Status != "degenerate" {
		return fmt.Errorf("unknown profile model status")
	}
	if len(r.Countries) != len(d.countrySet) || len(r.Disciplines) != len(d.disciplineSet[m.Scheme+"/"+m.Level]) {
		return fmt.Errorf("profile model label universe mismatch")
	}
	seenCountries := map[string]bool{}
	for _, id := range r.Countries {
		if !d.countrySet[id] || seenCountries[id] {
			return fmt.Errorf("unknown profile country")
		}
		seenCountries[id] = true
	}
	seenDisciplines := map[string]bool{}
	for _, id := range r.Disciplines {
		if !d.disciplineSet[m.Scheme+"/"+m.Level][id] || seenDisciplines[id] {
			return fmt.Errorf("unknown profile discipline")
		}
		seenDisciplines[id] = true
	}
	for _, group := range []struct {
		active []int
		labels []string
		scores []float64
	}{{r.ActiveCountries, r.Countries, r.ECI}, {r.ActiveDisciplines, r.Disciplines, r.PCI}} {
		seen := map[int]bool{}
		for _, i := range group.active {
			if i < 0 || i >= len(group.labels) || seen[i] {
				return fmt.Errorf("invalid profile active universe")
			}
			seen[i] = true
		}
		if r.Status == "ok" && len(group.active) != len(group.scores) {
			return fmt.Errorf("profile score cardinality mismatch")
		}
		for _, v := range group.scores {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("nonfinite profile score")
			}
		}
	}
	return nil
}

func (d *dataset) profileModelDownloads() []string {
	for id, p := range d.profiles {
		if id == baselineProfileID {
			continue
		}
		var out []string
		for name := range p.downloads.Shared {
			out = append(out, name)
		}
		for name := range p.downloads.Methods[DefaultMethod] {
			out = append(out, name)
		}
		sort.Strings(out)
		return out
	}
	return []string{}
}

func isProfileModelFile(name string) bool {
	return name == "eci.json" || name == "country_eci.parquet" || name == "discipline_pci.parquet" || name == "model_diagnostics.json" || strings.HasPrefix(name, "method-") || strings.HasPrefix(name, "profile-")
}

func (s *server) profileDownload(w http.ResponseWriter, r *http.Request, profileID, method, name string) {
	p := s.data.profiles[profileID]
	if p == nil {
		apiError(w, 400, "invalid_parameter", "当前样本方案未提供模型下载。")
		return
	}
	a, ok := p.downloads.Methods[method][name]
	if !ok {
		a, ok = p.downloads.Shared[name]
	}
	if !ok {
		apiError(w, 400, "invalid_parameter", "该文件不是当前筛选模型的产物。")
		return
	}
	path, err := profilePath(p.root, a.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.Size() != a.Bytes {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s-%s-%s\"", profileID, method, name))
	w.Header().Set("X-Artifact-SHA256", a.SHA256)
	w.Header().Set("X-OpenAlex-Method", method)
	w.Header().Set("X-OpenAlex-Profile-Run-ID", p.runID)
	w.Header().Set("Cache-Control", "no-store")
	if strings.HasSuffix(name, ".json") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	http.ServeContent(w, r, name, stat.ModTime(), f)
}
