// Offline complexity profiles; immutable counts and published baselines remain
// untouched. No arbitrary runtime threshold or ranking manipulation is served.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"openalexstats/internal/checkpoint"
	"openalexstats/internal/complexityfilter"
	"openalexstats/internal/pipeline"
)

const bundleVersion = "complexity-filter-bundle-v1"

type artifact struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type profileEntry struct {
	Profile         complexityfilter.Profile       `json:"profile"`
	RunID           string                         `json:"profile_run_id"`
	Models          map[string]artifact            `json:"models"`
	Ranks           map[string]artifact            `json:"ranks"`
	Downloads       map[string]map[string]artifact `json:"downloads"`
	Diagnostics     artifact                       `json:"diagnostics"`
	Source          string                         `json:"model_source"`
	ModelCount      int                            `json:"model_count_per_method"`
	Comparisons     artifact                       `json:"baseline_comparisons"`
	DirectionPolicy string                         `json:"direction_policy"`
	Alignment       artifact                       `json:"alignment"`
}

type bundle struct {
	Version                string            `json:"version"`
	SourceRunID            string            `json:"source_run_id"`
	SourceCompletionSHA256 string            `json:"source_completion_sha256"`
	DefaultProfileID       string            `json:"default_profile_id"`
	Corpus                 string            `json:"corpus"`
	DatasetVersion         string            `json:"dataset_version"`
	CountryVersion         string            `json:"country_version"`
	YearStart              int               `json:"year_start"`
	YearEnd                int               `json:"year_end"`
	ScenarioID             string            `json:"scenario_id"`
	EngineBinarySHA256     string            `json:"engine_binary_sha256"`
	Source                 sourceIdentity    `json:"source"`
	Profiles               []profileEntry    `json:"profiles"`
	BaselineModelsSHA256   map[string]string `json:"baseline_models_sha256"`
	Notes                  []string          `json:"notes"`
	DirectionPolicy        string            `json:"direction_policy"`
}

type modelDiagnostics struct {
	Year        int                          `json:"year"`
	Scheme      string                       `json:"scheme"`
	Level       string                       `json:"level"`
	Metric      string                       `json:"metric"`
	Diagnostics complexityfilter.Diagnostics `json:"diagnostics"`
}

type comparison struct {
	Year                int      `json:"year"`
	Scheme              string   `json:"scheme"`
	Level               string   `json:"level"`
	Metric              string   `json:"metric"`
	Method              string   `json:"method"`
	CommonCountries     int      `json:"common_countries"`
	SignedCorrelation   *float64 `json:"signed_score_correlation"`
	AbsoluteCorrelation *float64 `json:"absolute_score_correlation"`
}

type computed struct {
	models    map[string][]pipeline.ModelResult
	diag      []modelDiagnostics
	direction directionAudit
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "complexity-profiles:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	data := flag.String("data", "", "immutable source statistics directory")
	methods := flag.String("methods", "", "matching published reflection sidecar")
	corpus := flag.String("corpus", "", "core or all")
	version := flag.String("dataset-version", "", "source country dataset version")
	profilePath := flag.String("profiles", "", "optional JSON array; default disclosed19 presets")
	output := flag.String("output", "", "new immutable bundle directory")
	defaultID := flag.String("default-profile", complexityfilter.DefaultProfileID, "profile included in frozen bundle")
	workers := flag.Int("workers", min(32, runtime.NumCPU()), "parallel model jobs,1..64")
	flag.Parse()
	if *data == "" || *methods == "" || *output == "" || (*corpus != "core" && *corpus != "all") || *version == "" || *workers < 1 || *workers > 64 {
		return fmt.Errorf("--data --methods --corpus --dataset-version --output and1..64 workers required")
	}
	profiles := complexityfilter.Presets()
	if *profilePath != "" {
		if err := readJSON(*profilePath, &profiles); err != nil {
			return err
		}
	}
	if err := validateProfiles(profiles, *defaultID); err != nil {
		return err
	}
	if err := ensureSeparateOutput(*output, *data, *methods); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	binary, err := checkpoint.Hash(executable)
	if err != nil {
		return err
	}
	started := time.Now()
	source, err := loadSource(ctx, *data, *methods, *corpus)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "source loaded: %s/%s %d annual inputs in %.2fs\n", *corpus, *version, len(source.Inputs), time.Since(started).Seconds())
	computedProfiles, err := computeProfiles(ctx, source, profiles, *workers)
	if err != nil {
		return err
	}
	// Recheck every input actually used, including source counts and both
	// published model files, before creating any completed output manifest.
	for _, a := range source.Identity.Inputs {
		root := *data
		if filepath.Dir(a.Path) == "methods" {
			root = *methods
		}
		actual, err := checkpoint.Hash(filepath.Join(root, filepath.Base(a.Path)))
		if err != nil || actual.SHA256 != a.SHA256 || actual.Bytes != a.Bytes {
			return fmt.Errorf("source changed during profile derivation: %s", a.Path)
		}
	}
	if err = os.MkdirAll(filepath.Dir(*output), 0755); err != nil {
		return err
	}
	if err = os.Mkdir(*output, 0755); err != nil {
		return fmt.Errorf("output must be new: %w", err)
	}
	m := bundle{Version: bundleVersion, SourceRunID: source.Identity.RunID, SourceCompletionSHA256: source.Identity.CompletionSHA256, DefaultProfileID: *defaultID, Corpus: *corpus, DatasetVersion: *version, CountryVersion: *version, YearStart: source.Inputs[0].Year, YearEnd: source.Inputs[len(source.Inputs)-1].Year, ScenarioID: "fractional-concept-whole-0.3", DirectionPolicy: directionPolicy, EngineBinarySHA256: binary.SHA256, Source: source.Identity, BaselineModelsSHA256: map[string]string{"spectral-v1": source.Identity.SpectralModelsSHA256, "java-reflections-v1": source.Identity.ReflectionModelsSHA256}, Notes: []string{
		"Profiles change only internally recomputed RCA/ECI/PCI; original count, share and displayed count-RCA tables remain unchanged.",
		"Country minimum publications uses whole-country annual primary-Field totals including UNKNOWN subject, not Concepts hit sums.",
		"country_min_citation_events applies only to citation input; events are not independent cited-target-paper samples.",
		"Scale gate then X<cell_min_support zeroing, followed by synchronous permanent low-diversity removal and full RCA recalculation to fixed point; no coauthor reweighting.",
		"Both methods share final M. Production spectral diagnostics and stable16/17 reflections; no fabricated scores for disconnected/degenerate models.",
		"Each profile/method first follows existing temporal continuity, then one2000-2025 pooled common-year baseline factor jointly aligns its entire27-year ECI/PCI chain; no country/year/rank-based or cross-method fitting.",
		"Direction policy baseline-pooled-common-year-sign-v1 is an axis-comparability convention, not a validated national-capability interpretation. Weak and unavailable references are explicit.",
		"baseline-v1 actual output copies published scores, active sets and direction exactly; zero-profile recomputation supplies diagnostics and verifies the identical source M only.",
		"Thresholds are disclosed engineering starting points, not externally validated universal national-capability criteria.",
	}}
	for p, profile := range profiles {
		folder := filepath.Join(*output, profile.ID)
		if err = os.Mkdir(folder, 0755); err != nil {
			return err
		}
		entry := profileEntry{Profile: profile, RunID: profileIdentity(source.Identity, profile, binary.SHA256, *corpus, *version), Models: map[string]artifact{}, Ranks: map[string]artifact{}, Downloads: map[string]map[string]artifact{}, ModelCount: len(source.Inputs), Source: "recomputed_filtered_matrix", DirectionPolicy: directionPolicy}
		if profile.ID == complexityfilter.BaselineProfileID {
			entry.Source = "copied_published_models_exact_scores_active_sets_and_direction"
		}
		for _, method := range []string{"spectral-v1", "java-reflections-v1"} {
			if entry.Models[method], err = writeArtifact(*output, filepath.Join(profile.ID, method+".json"), computedProfiles[p].models[method]); err != nil {
				return err
			}
			methodFolder := filepath.Join(profile.ID, method)
			if err = os.Mkdir(filepath.Join(*output, methodFolder), 0755); err != nil {
				return err
			}
			countries, disciplines, ranks, e := scoreExports(computedProfiles[p].models[method], source.Identity.RunID, profile.ID, entry.RunID, method)
			if e != nil {
				return e
			}
			if entry.Ranks[method], err = writeArtifact(*output, filepath.Join(methodFolder, "model-ranks.json"), ranks); err != nil {
				return err
			}
			countryArtifact, e := writeScoreArtifact(*output, filepath.Join(methodFolder, "country_eci.parquet"), countries)
			if e != nil {
				return e
			}
			disciplineArtifact, e := writeScoreArtifact(*output, filepath.Join(methodFolder, "discipline_pci.parquet"), disciplines)
			if e != nil {
				return e
			}
			entry.Downloads[method] = map[string]artifact{"eci.json": entry.Models[method], "country_eci.parquet": countryArtifact, "discipline_pci.parquet": disciplineArtifact}
		}
		if entry.Diagnostics, err = writeArtifact(*output, filepath.Join(profile.ID, "filter-diagnostics.json"), computedProfiles[p].diag); err != nil {
			return err
		}
		comparisons := compareWithBaseline(source, computedProfiles[p])
		if entry.Comparisons, err = writeArtifact(*output, filepath.Join(profile.ID, "baseline-comparisons.json"), comparisons); err != nil {
			return err
		}
		if entry.Alignment, err = writeArtifact(*output, filepath.Join(profile.ID, "direction-alignment.json"), computedProfiles[p].direction); err != nil {
			return err
		}
		profileSources := map[string]any{"version": bundleVersion, "algorithm": complexityfilter.Algorithm, "direction_policy": directionPolicy, "alignment": entry.Alignment, "profile": profile, "profile_run_id": entry.RunID, "source_run_id": m.SourceRunID, "source_completion_sha256": m.SourceCompletionSHA256, "source": source.Identity, "engine_binary_sha256": m.EngineBinarySHA256, "corpus": *corpus, "country_version": *version, "year_start": m.YearStart, "year_end": m.YearEnd, "scenario_id": m.ScenarioID, "model_source": entry.Source, "models": entry.Models, "ranks": entry.Ranks, "diagnostics": entry.Diagnostics, "baseline_comparisons": entry.Comparisons, "download_score_artifacts": entry.Downloads, "applies_to_metrics": []string{"eci", "pci"}, "changes_original_count_share_or_displayed_rca": false, "heavy_network_arrays_omitted_from_model_downloads": true, "notes": m.Notes}
		profileSourcesArtifact, e := writeArtifact(*output, filepath.Join(profile.ID, "profile-sources.json"), profileSources)
		if e != nil {
			return e
		}
		entry.Downloads["shared"] = map[string]artifact{"profile-diagnostics.json": entry.Diagnostics, "model_diagnostics.json": entry.Diagnostics, "profile-manifest.json": profileSourcesArtifact, "profile-direction.json": entry.Alignment}
		m.Profiles = append(m.Profiles, entry)
	}
	if _, err = writeArtifact(*output, "manifest.json", m); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "completed %s/%s: %d profiles,%d network jobs,%.2fs\n", *corpus, *version, len(profiles), len(profiles)*len(source.Inputs), time.Since(started).Seconds())
	return nil
}

func computeProfiles(ctx context.Context, source sourceData, profiles []complexityfilter.Profile, workers int) ([]computed, error) {
	out := make([]computed, len(profiles))
	for p := range out {
		out[p] = computed{models: map[string][]pipeline.ModelResult{"spectral-v1": make([]pipeline.ModelResult, len(source.Inputs)), "java-reflections-v1": make([]pipeline.ModelResult, len(source.Inputs))}, diag: make([]modelDiagnostics, len(source.Inputs))}
	}
	type job struct{ p, i int }
	jobs := make(chan job)
	errs := make([]error, len(out)*len(source.Inputs))
	var wg sync.WaitGroup
	for w := 0; w < min(workers, max(1, len(errs))); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					errs[j.p*len(source.Inputs)+j.i] = ctx.Err()
					continue
				}
				in := source.Inputs[j.i]
				computed, err := complexityfilter.Compute(in.Input, profiles[j.p])
				if err != nil {
					errs[j.p*len(source.Inputs)+j.i] = err
					continue
				}
				out[j.p].diag[j.i] = modelDiagnostics{in.Year, in.Scheme, in.Level, in.Metric, computed.Diagnostics}
				if profiles[j.p].ID == complexityfilter.BaselineProfileID {
					if computed.Diagnostics.FinalNetworkSHA256 != source.NetworkHashes[j.i] {
						errs[j.p*len(source.Inputs)+j.i] = fmt.Errorf("zero-profile M differs from source baseline %d/%s/%s/%s", in.Year, in.Scheme, in.Level, in.Metric)
						continue
					}
					for _, method := range []string{"spectral-v1", "java-reflections-v1"} {
						out[j.p].models[method][j.i] = source.Baseline[method][j.i]
					}
					continue
				}
				spectral, reflections := computed.Spectral, computed.Reflections
				spectral.RCA, spectral.Advantage, spectral.Components = nil, nil, nil
				reflections.RCA, reflections.Advantage, reflections.Components = nil, nil, nil
				out[j.p].models["spectral-v1"][j.i] = pipeline.ModelResult{Year: in.Year, Scheme: in.Scheme, Level: in.Level, Metric: in.Metric, Result: spectral}
				out[j.p].models["java-reflections-v1"][j.i] = pipeline.ModelResult{Year: in.Year, Scheme: in.Scheme, Level: in.Level, Metric: in.Metric, Result: reflections}
			}
		}()
	}
	for p := range profiles {
		for i := range source.Inputs {
			jobs <- job{p, i}
		}
	}
	close(jobs)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("profile %s input%d: %w", profiles[i/len(source.Inputs)].ID, i%len(source.Inputs), err)
		}
	}
	for p, profile := range profiles {
		out[p].direction = directionAudit{Policy: directionPolicy, ModelSource: "pooled_complete_year_baseline_reference", BaselineModelsSHA256: map[string]string{"spectral-v1": source.Identity.SpectralModelsSHA256, "java-reflections-v1": source.Identity.ReflectionModelsSHA256}, Chains: []directionRecord{}}
		if profile.ID != complexityfilter.BaselineProfileID {
			for _, method := range []string{"spectral-v1", "java-reflections-v1"} {
				pipeline.AlignModelYears(out[p].models[method])
				sha := source.Identity.SpectralModelsSHA256
				if method == "java-reflections-v1" {
					sha = source.Identity.ReflectionModelsSHA256
				}
				out[p].direction.Chains = append(out[p].direction.Chains, alignToBaseline(out[p].models[method], source.Baseline[method], method, sha)...)
			}
		} else {
			out[p].direction.ModelSource = "copied_publication"
		}
	}
	return out, nil
}

func validateProfiles(profiles []complexityfilter.Profile, defaultID string) error {
	safe := regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	seen := map[string]bool{}
	for _, p := range profiles {
		if err := p.Validate(); err != nil || !safe.MatchString(p.ID) || seen[p.ID] {
			return fmt.Errorf("invalid or duplicate profile %q", p.ID)
		}
		if p.ID == complexityfilter.BaselineProfileID && (p.CountryMinPublications != 0 || p.CountryMinCitationEvents != 0 || p.CellMinSupport != 0 || p.MinDiversity != 0) {
			return fmt.Errorf("baseline-v1 must have zero thresholds")
		}
		seen[p.ID] = true
	}
	if !seen[defaultID] || !seen[complexityfilter.BaselineProfileID] {
		return fmt.Errorf("profiles require baseline-v1 and selected default")
	}
	return nil
}

func profileIdentity(source sourceIdentity, p complexityfilter.Profile, engineSHA, corpus, version string) string {
	profile := map[string]any{"id": p.ID, "display_name": p.DisplayName, "country_min_publications": p.CountryMinPublications, "country_min_citation_events": p.CountryMinCitationEvents, "cell_min_support": p.CellMinSupport, "min_diversity": p.MinDiversity}
	identity := map[string]any{"version": bundleVersion, "algorithm": complexityfilter.Algorithm, "direction_policy": directionPolicy, "source_run_id": source.RunID, "source_completion_sha256": source.CompletionSHA256, "source_method_completion_sha256": source.MethodCompletionSHA256, "statistics_sha256": source.StatisticsSHA256, "dictionary_sha256": source.DictionarySHA256, "scenarios_sha256": source.ScenariosSHA256, "baseline_spectral_sha256": source.SpectralModelsSHA256, "baseline_reflections_sha256": source.ReflectionModelsSHA256, "engine_binary_sha256": engineSHA, "corpus": corpus, "dataset_version": version, "profile": profile}
	b, _ := json.Marshal(identity) // Nested maps are sorted by encoding/json.
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func writeArtifact(root, relative string, value any) (artifact, error) {
	p := filepath.Join(root, relative)
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return artifact{}, err
	}
	err = json.NewEncoder(f).Encode(value)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return artifact{}, err
	}
	a, err := checkpoint.Hash(p)
	return artifact{Path: filepath.ToSlash(relative), Bytes: a.Bytes, SHA256: a.SHA256}, err
}

func ensureSeparateOutput(output string, inputs ...string) error {
	out, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	// The parent must already exist and be resolved; this rejects nonexistent
	// ancestor tricks and ensures no new output goes through an input symlink.
	parent, err := filepath.EvalSymlinks(filepath.Dir(out))
	if err != nil {
		return fmt.Errorf("output parent must exist: %w", err)
	}
	out = filepath.Join(parent, filepath.Base(out))
	for _, input := range inputs {
		root, err := filepath.EvalSymlinks(input)
		if err != nil {
			return err
		}
		root, _ = filepath.Abs(root)
		rel, err := filepath.Rel(root, out)
		if err != nil || rel == "." || (rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return fmt.Errorf("output must be outside immutable inputs")
		}
	}
	if _, err := os.Lstat(out); err == nil || !os.IsNotExist(err) {
		return fmt.Errorf("output must not already exist")
	}
	return nil
}
