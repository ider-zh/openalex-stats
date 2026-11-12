package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/eci"
	"openalexstats/internal/pipeline"
	"openalexstats/internal/results"
)

type Config struct {
	Data, Output, BuildID string
	Workers               int
}

type Audit struct {
	Status                        string         `json:"status"`
	Models                        int            `json:"models"`
	ValidModels                   int            `json:"valid_models"`
	Statuses                      map[string]int `json:"statuses"`
	NetworkChecks                 int            `json:"network_checks"`
	IndependentIterationChecks    int            `json:"independent_iteration_checks"`
	SignChecks                    int            `json:"sign_checks"`
	MaxIndependentScoreDifference float64        `json:"max_independent_score_difference"`
	LowPrecisionModels            int            `json:"low_precision_models"`
	BaseArtifactsUnchanged        bool           `json:"base_artifacts_unchanged"`
}

type Comparison struct {
	CountryIterations      int      `json:"country_iterations"`
	Year                   int      `json:"year"`
	Scheme                 string   `json:"scheme"`
	Level                  string   `json:"level"`
	Metric                 string   `json:"metric"`
	Status                 string   `json:"status"`
	CountrySpearman        *float64 `json:"country_spearman,omitempty"`
	DisciplineSpearman     *float64 `json:"discipline_spearman,omitempty"`
	CountryTop10Overlap    int      `json:"country_top10_overlap"`
	DisciplineTop10Overlap int      `json:"discipline_top10_overlap"`
}

// Derive never changes a base result. Its only source of networks is the
// checksummed eci.json already certified by the base completion manifest.
func Derive(ctx context.Context, cfg Config) (Manifest, error) {
	var manifest Manifest
	if cfg.Data == "" || cfg.Output == "" || cfg.BuildID == "" || cfg.Workers < 1 || cfg.Workers > 64 {
		return manifest, fmt.Errorf("invalid method derivation config")
	}
	root, err := filepath.EvalSymlinks(cfg.Data)
	if err != nil {
		return manifest, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return manifest, err
	}
	out, err := filepath.Abs(cfg.Output)
	if err != nil {
		return manifest, err
	}
	if resolved, e := filepath.EvalSymlinks(out); e == nil {
		out = resolved
	}
	rel, err := filepath.Rel(root, out)
	if err != nil || rel == "." || (!filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return manifest, fmt.Errorf("method output must be outside immutable base result")
	}
	unlock, err := checkpoint.LockDirectory(out)
	if err != nil {
		return manifest, err
	}
	defer unlock()
	entries, err := os.ReadDir(out)
	if err != nil {
		return manifest, err
	}
	for _, entry := range entries {
		if entry.Name() != ".run.lock" {
			return manifest, fmt.Errorf("method output must be new and empty: %s", out)
		}
	}
	var completion struct {
		RunID     string                `json:"run_id"`
		Run       checkpoint.Artifact   `json:"run"`
		Artifacts []checkpoint.Artifact `json:"artifacts"`
	}
	if err = checkpoint.ReadJSON(filepath.Join(root, "completion.json"), &completion); err != nil {
		return manifest, err
	}
	var run struct {
		RunID  string          `json:"run_id"`
		Status string          `json:"status"`
		Config pipeline.Config `json:"config"`
	}
	if err = checkpoint.ReadJSON(filepath.Join(root, "run.json"), &run); err != nil {
		return manifest, err
	}
	if run.RunID == "" || completion.RunID != run.RunID || run.Status != "full_snapshot_complete" {
		return manifest, fmt.Errorf("method requires a completed full-snapshot base")
	}
	if run.Config.Scope.Corpus != "core" && run.Config.Scope.Corpus != "all" {
		return manifest, fmt.Errorf("unsupported corpus")
	}
	var sources []checkpoint.Artifact
	for _, name := range []string{"completion.json", "run.json", "eci.json", "scenarios.json"} {
		a, e := checkpoint.Hash(filepath.Join(root, name))
		if e != nil {
			return manifest, e
		}
		if name != "completion.json" {
			found := false
			for _, expected := range append(append([]checkpoint.Artifact(nil), completion.Artifacts...), completion.Run) {
				if filepath.Base(expected.Path) == name {
					found = true
					if expected.SHA256 != a.SHA256 || expected.Bytes != a.Bytes {
						return manifest, fmt.Errorf("base checksum mismatch: %s", name)
					}
				}
			}
			if !found {
				return manifest, fmt.Errorf("base completion does not own %s", name)
			}
		}
		a.Path = name
		sources = append(sources, a)
	}
	var scenarios []results.Scenario
	if err = checkpoint.ReadJSON(filepath.Join(root, "scenarios.json"), &scenarios); err != nil {
		return manifest, err
	}
	scenario := ""
	for _, s := range scenarios {
		if s.ForECI() {
			if scenario != "" {
				return manifest, fmt.Errorf("multiple main scenarios")
			}
			scenario = s.ID
		}
	}
	if scenario == "" {
		return manifest, fmt.Errorf("base has no main scenario")
	}
	var base []Model
	if err = checkpoint.ReadJSON(filepath.Join(root, "eci.json"), &base); err != nil {
		return manifest, err
	}
	expectedModels := (run.Config.YearEnd - run.Config.YearStart + 1) * 8
	if len(base) != expectedModels {
		return manifest, fmt.Errorf("incomplete base model inventory: %d/%d", len(base), expectedModels)
	}
	seen := map[string]bool{}
	for i := range base {
		b := &base[i]
		if b.ScenarioID == "" {
			b.ScenarioID = scenario
		}
		key := fmt.Sprintf("%d/%s/%s/%s", b.Year, b.Scheme, b.Level, b.Metric)
		validLayer := (b.Scheme == "topics" && (b.Level == "field" || b.Level == "subfield")) || (b.Scheme == "concepts" && (b.Level == "0" || b.Level == "1"))
		if seen[key] || !validLayer || b.Year < run.Config.YearStart || b.Year > run.Config.YearEnd || (b.Metric != "publications" && b.Metric != "citations") || b.ScenarioID != scenario {
			return manifest, fmt.Errorf("invalid/duplicate base model %s", key)
		}
		seen[key] = true
		b.NetworkSHA256, err = networkKey(b.Result)
		if err != nil {
			return manifest, err
		}
	}
	variants := []int{12, 16, 20}
	computed := make([][]Model, len(variants))
	for i := range computed {
		computed[i] = make([]Model, len(base))
	}
	type job struct{ variant, index int }
	jobs := make(chan job)
	errs := make([]error, len(base)*len(variants))
	var wg sync.WaitGroup
	for w := 0; w < min(cfg.Workers, len(errs)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					errs[j.variant*len(base)+j.index] = ctx.Err()
					continue
				}
				b := base[j.index]
				b.Result, b.ReflectionDiagnostics, errs[j.variant*len(base)+j.index] = eci.ReflectionsFromPrepared(b.Result, eci.ReflectionConfig{CountryIterations: variants[j.variant], DisciplineIterations: variants[j.variant] + 1})
				computed[j.variant][j.index] = b
			}
		}()
	}
	for v := range variants {
		for i := range base {
			jobs <- job{v, i}
		}
	}
	close(jobs)
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return manifest, e
		}
	}
	var comparisons []Comparison
	var audit Audit
	for v := range variants {
		aligned := make([]pipeline.ModelResult, len(base))
		for i, m := range computed[v] {
			aligned[i] = pipeline.ModelResult{Year: m.Year, Scheme: m.Scheme, Level: m.Level, Metric: m.Metric, Result: m.Result}
		}
		flips := pipeline.AlignModelYears(aligned)
		for i := range computed[v] {
			computed[v][i].Result = aligned[i].Result
			computed[v][i].TemporalSignFlipped = flips[i]
		}
		a, e := auditModels(base, computed[v])
		if e != nil {
			return manifest, fmt.Errorf("reflection n=%d audit: %w", variants[v], e)
		}
		if variants[v] == 16 {
			audit = a
		}
		for i, m := range computed[v] {
			c := Comparison{CountryIterations: variants[v], Year: m.Year, Scheme: m.Scheme, Level: m.Level, Metric: m.Metric, Status: m.Result.Status}
			if m.Result.Status == "ok" && base[i].Result.Status == "ok" {
				c.CountrySpearman = spearman(m.Result.ECI, base[i].Result.ECI)
				c.DisciplineSpearman = spearman(m.Result.PCI, base[i].Result.PCI)
				c.CountryTop10Overlap = topOverlap(m.Result.ECI, base[i].Result.ECI)
				c.DisciplineTop10Overlap = topOverlap(m.Result.PCI, base[i].Result.PCI)
			}
			comparisons = append(comparisons, c)
		}
	}
	// Recheck source identities after derivation; no base file is opened writable.
	for _, a := range sources {
		actual, e := checkpoint.Hash(filepath.Join(root, a.Path))
		if e != nil {
			return manifest, e
		}
		if actual.SHA256 != a.SHA256 || actual.Bytes != a.Bytes {
			return manifest, fmt.Errorf("base changed during derivation: %s", a.Path)
		}
	}
	audit.BaseArtifactsUnchanged = true
	var artifacts []checkpoint.Artifact
	write := func(name string, value any) error {
		a, e := checkpoint.Write(filepath.Join(out, name), func(w io.Writer) error { return json.NewEncoder(w).Encode(value) })
		if e == nil {
			a.Path = name
			artifacts = append(artifacts, a)
		}
		return e
	}
	if err = write(eci.ReflectionsMethodID+".json", computed[1]); err != nil {
		return manifest, err
	}
	modelSHA := artifacts[len(artifacts)-1].SHA256
	if err = write("audit.json", audit); err != nil {
		return manifest, err
	}
	if err = write("sensitivity.json", comparisons); err != nil {
		return manifest, err
	}
	inputs := make([]results.ModelInput, len(base))
	for i, m := range computed[1] {
		inputs[i] = results.ModelInput{ScenarioID: scenario, Year: m.Year, Scheme: m.Scheme, Level: m.Level, Metric: m.Metric, Result: m.Result}
	}
	countries, disciplines, diagnostics, e := results.FlattenModels(inputs)
	if e != nil {
		return manifest, e
	}
	if err = write("model_diagnostics.json", diagnostics); err != nil {
		return manifest, err
	}
	for _, item := range []struct {
		name string
		rows []results.ScoreRow
	}{{"country_eci.parquet", countries}, {"discipline_pci.parquet", disciplines}} {
		a, e := checkpoint.Write(filepath.Join(out, item.name), func(w io.Writer) error {
			pw := parquet.NewGenericWriter[results.ScoreRow](w, parquet.Compression(&zstd.Codec{}))
			_, e := pw.Write(item.rows)
			if e != nil {
				_ = pw.Close()
				return e
			}
			return pw.Close()
		})
		if e != nil {
			return manifest, e
		}
		a.Path = item.name
		artifacts = append(artifacts, a)
	}
	manifest = Manifest{Version: 1, BaseRunID: run.RunID, Corpus: run.Config.Scope.Corpus, SourceCompletionSHA256: sources[0].SHA256, BuildID: cfg.BuildID, ScenarioID: scenario, Sources: sources, Methods: []Definition{{ID: eci.ReflectionsMethodID, DisplayName: "Java 反射法·修正版", CountryIterations: 16, DisciplineIterations: 17, Normalization: "population-z", Models: eci.ReflectionsMethodID + ".json", ModelsSHA256: modelSHA, ResidualKind: computed[1][0].ReflectionDiagnostics.ResidualKind}}, Notes: []string{"Exact audited base RCA and binary network; publication, citation, share and RCA artifacts are unchanged.", "Finite country iteration 16 and discipline iteration 17, both reading the previous step; initial values are binary-network degrees.", "No Harvard/Population stopping rule or initial-degree fallback; zero variance, disconnected and unidentifiable models have no fabricated scores.", "Population-z scaling, paired ECI/PCI sign and the existing temporal common-country convention.", "Sensitivity compares 12/13,16/17,20/21 finite steps with the original spectral scores; no historical run parameters are assumed.", "Derived at " + time.Now().UTC().Format(time.RFC3339)}}
	if err = write("manifest.json", manifest); err != nil {
		return manifest, err
	}
	manifestSHA := artifacts[len(artifacts)-1].SHA256
	if err = checkpoint.JSON(filepath.Join(out, "completion.json"), Completion{Version: 1, Status: "complete", BaseRunID: run.RunID, ManifestSHA256: manifestSHA, Artifacts: artifacts}); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func networkKey(r eci.Result) (string, error) {
	return checkpoint.Key(struct {
		Countries, Disciplines             []string
		RCA                                [][]float64
		Advantage                          [][]bool
		ActiveCountries, ActiveDisciplines []int
	}{r.Countries, r.Disciplines, r.RCA, r.Advantage, r.ActiveCountries, r.ActiveDisciplines})
}

func ranked(v []float64) []int {
	order := make([]int, len(v))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return v[order[i]] > v[order[j]] })
	return order
}
func ranks(v []float64) []float64 {
	o := ranked(v)
	r := make([]float64, len(v))
	for i := 0; i < len(o); {
		j := i + 1
		for j < len(o) && v[o[j]] == v[o[i]] {
			j++
		}
		for k := i; k < j; k++ {
			r[o[k]] = float64(i+j+1) / 2
		}
		i = j
	}
	return r
}
func spearman(a, b []float64) *float64 {
	if len(a) != len(b) || len(a) < 2 {
		return nil
	}
	x, y := ranks(a), ranks(b)
	m := float64(len(a)+1) / 2
	xy, xx, yy := 0.0, 0.0, 0.0
	for i := range x {
		u, v := x[i]-m, y[i]-m
		xy += u * v
		xx += u * u
		yy += v * v
	}
	if xx == 0 || yy == 0 {
		return nil
	}
	r := xy / math.Sqrt(xx*yy)
	return &r
}
func topOverlap(a, b []float64) int {
	x, y := ranked(a), ranked(b)
	set := map[int]bool{}
	for _, i := range x[:min(10, len(x))] {
		set[i] = true
	}
	n := 0
	for _, i := range y[:min(10, len(y))] {
		if set[i] {
			n++
		}
	}
	return n
}
