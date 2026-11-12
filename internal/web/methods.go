package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"

	"openalexstats/internal/checkpoint"
	"openalexstats/internal/eci"
	"openalexstats/internal/methods"
)

type MethodInfo struct {
	ID                     string `json:"id"`
	DisplayName            string `json:"display_name"`
	CountryIterations      int    `json:"country_iterations"`
	DisciplineIterations   int    `json:"discipline_iterations"`
	Normalization          string `json:"normalization"`
	ResidualKind           string `json:"residual_kind"`
	SourceRunID            string `json:"source_run_id"`
	SourceCompletionSHA256 string `json:"source_completion_sha256"`
}

const DefaultMethod = methods.SpectralID

// Keep the immutable base-artifact identity separate from the UI preference.
// A dataset without the optional sidecar must remain usable with its base model.
func (d *dataset) defaultMethod() string {
	if _, ok := d.method(eci.ReflectionsMethodID); ok {
		return eci.ReflectionsMethodID
	}
	return DefaultMethod
}

func bytesSHA(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

func initDatasetMethods(d *dataset) {
	if d.methodModels != nil {
		return
	}
	completionSHA := ""
	if b, err := readSafe(d.root, "completion.json"); err == nil {
		completionSHA = bytesSHA(b)
	}
	d.methodModels = map[string]map[modelKey]eci.Result{methods.SpectralID: d.models}
	d.methodInfo = []MethodInfo{{ID: methods.SpectralID, DisplayName: "谱 ECI · 当前方法", Normalization: "population-z", ResidualKind: "spectral-eigen-equation", SourceRunID: d.run.RunID, SourceCompletionSHA256: completionSHA}}
	d.methodRoots = map[string]string{}
	d.methodFiles = map[string]map[string]string{}
}

func (d *dataset) method(id string) (MethodInfo, bool) {
	initDatasetMethods(d)
	for _, info := range d.methodInfo {
		if info.ID == id {
			return info, true
		}
	}
	return MethodInfo{}, false
}

// loadDatasetMethods only loads an admin-configured immutable sidecar, never a
// request path. It checks its completion, every artifact, the exact base run and
// completion, and equality of every shared RCA/network/label universe.
func loadDatasetMethods(d *dataset, dir string) error {
	initDatasetMethods(d)
	if dir == "" {
		return nil
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(d.root, root)
	if err != nil {
		return err
	}
	if rel == "." || (!filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return fmt.Errorf("web: method sidecar must be outside immutable base")
	}
	read := func(name string, target any) ([]byte, error) {
		b, e := readSafe(root, name)
		if e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b, target); e != nil {
			return nil, e
		}
		return b, nil
	}
	var completion methods.Completion
	if _, err = read("completion.json", &completion); err != nil {
		return fmt.Errorf("web: method completion: %w", err)
	}
	if completion.Version != 1 || completion.Status != "complete" || completion.BaseRunID == "" || completion.BaseRunID != d.run.RunID {
		return fmt.Errorf("web: incomplete or mismatched method base run")
	}
	var manifest methods.Manifest
	mb, err := read("manifest.json", &manifest)
	if err != nil {
		return err
	}
	if bytesSHA(mb) != completion.ManifestSHA256 {
		return fmt.Errorf("web: method manifest checksum mismatch")
	}
	base, ok := d.method(methods.SpectralID)
	if !ok || base.SourceCompletionSHA256 == "" || manifest.SourceCompletionSHA256 != base.SourceCompletionSHA256 || manifest.Version != 1 || manifest.BaseRunID != d.run.RunID || manifest.Corpus != d.run.Config.Scope.Corpus || manifest.ScenarioID != d.eciScenario {
		return fmt.Errorf("web: method source identity does not match selected corpus/base")
	}
	artifacts := map[string]checkpoint.Artifact{}
	for _, a := range completion.Artifacts {
		if _, duplicate := artifacts[a.Path]; duplicate {
			return fmt.Errorf("web: duplicate method artifact")
		}
		path, e := safeFile(root, a.Path)
		if e != nil {
			return e
		}
		got, e := checkpoint.Hash(path)
		if e != nil {
			return e
		}
		if a.Bytes != got.Bytes || a.SHA256 != got.SHA256 {
			return fmt.Errorf("web: method artifact checksum mismatch: %s", a.Path)
		}
		artifacts[a.Path] = a
	}
	if a, ok := artifacts["manifest.json"]; !ok || a.SHA256 != completion.ManifestSHA256 {
		return fmt.Errorf("web: method completion does not own manifest")
	}
	for _, a := range manifest.Sources {
		path, e := safeFile(d.root, a.Path)
		if e != nil {
			return e
		}
		got, e := checkpoint.Hash(path)
		if e != nil {
			return e
		}
		if a.Bytes != got.Bytes || a.SHA256 != got.SHA256 {
			return fmt.Errorf("web: changed method base source: %s", a.Path)
		}
	}
	if len(manifest.Methods) != 1 {
		return fmt.Errorf("web: unsupported method inventory")
	}
	for _, def := range manifest.Methods {
		if def.ID != eci.ReflectionsMethodID || def.CountryIterations != 16 || def.DisciplineIterations != 17 || def.Normalization != "population-z" {
			return fmt.Errorf("web: unsupported method definition")
		}
		if _, exists := d.methodModels[def.ID]; exists {
			return fmt.Errorf("web: duplicate method identity")
		}
		a, ok := artifacts[def.Models]
		if !ok || a.SHA256 != def.ModelsSHA256 {
			return fmt.Errorf("web: unowned method models")
		}
		var rows []modelResult
		if _, err = read(def.Models, &rows); err != nil {
			return err
		}
		loaded := map[modelKey]eci.Result{}
		for _, m := range rows {
			scenario := m.ScenarioID
			if scenario == "" {
				scenario = manifest.ScenarioID
			}
			key := modelKey{scenario, m.Scheme, m.Level, m.Metric, m.Year}
			baseModel, exists := d.models[key]
			if _, duplicate := loaded[key]; duplicate {
				return fmt.Errorf("web: duplicate method model")
			}
			if !exists || !sameNetwork(baseModel, m.Result) {
				return fmt.Errorf("web: method model changed shared network/labels")
			}
			if m.Result.Status == "ok" {
				if len(m.Result.ECI) != len(m.Result.ActiveCountries) || len(m.Result.PCI) != len(m.Result.ActiveDisciplines) {
					return fmt.Errorf("web: method score length mismatch")
				}
				for _, scores := range [][]float64{m.Result.ECI, m.Result.PCI} {
					for _, v := range scores {
						if math.IsNaN(v) || math.IsInf(v, 0) {
							return fmt.Errorf("web: non-finite method score")
						}
					}
				}
			}
			loaded[key] = m.Result
		}
		if len(loaded) != len(d.models) {
			return fmt.Errorf("web: method does not cover every base model")
		}
		d.methodModels[def.ID] = loaded
		d.methodInfo = append(d.methodInfo, MethodInfo{ID: def.ID, DisplayName: "小倪方法论", CountryIterations: def.CountryIterations, DisciplineIterations: def.DisciplineIterations, Normalization: def.Normalization, ResidualKind: def.ResidualKind, SourceRunID: manifest.BaseRunID, SourceCompletionSHA256: manifest.SourceCompletionSHA256})
		d.methodRoots[def.ID] = root
		files := map[string]string{"eci.json": def.Models, "method-manifest.json": "manifest.json", "method-completion.json": "completion.json"}
		for alias, file := range map[string]string{"country_eci.parquet": "country_eci.parquet", "discipline_pci.parquet": "discipline_pci.parquet", "model_diagnostics.json": "model_diagnostics.json", "method-audit.json": "audit.json", "method-sensitivity.json": "sensitivity.json"} {
			if _, ok := artifacts[file]; ok {
				files[alias] = file
			}
		}
		d.methodFiles[def.ID] = files
	}
	return nil
}

func sameNetwork(a, b eci.Result) bool {
	return reflect.DeepEqual(a.Countries, b.Countries) && reflect.DeepEqual(a.Disciplines, b.Disciplines) && reflect.DeepEqual(a.ActiveCountries, b.ActiveCountries) && reflect.DeepEqual(a.ActiveDisciplines, b.ActiveDisciplines) && reflect.DeepEqual(a.RCA, b.RCA) && reflect.DeepEqual(a.Advantage, b.Advantage)
}

// NewWithMethods keeps New's single-directory API while selecting an optional
// immutable method sidecar. The request may only select its published method ID.
func NewWithMethods(dataDir, methodDir string) (http.Handler, error) {
	h, err := New(dataDir)
	if err != nil {
		return nil, err
	}
	s := h.(*server)
	if err = loadDatasetMethods(s.data, methodDir); err != nil {
		return nil, err
	}
	return h, nil
}
