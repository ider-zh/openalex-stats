// Package methods derives immutable method sidecars from completed base models.
package methods

import (
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/eci"
)

const SpectralID = "spectral-v1"

type Definition struct {
	ID                   string `json:"id"`
	DisplayName          string `json:"display_name"`
	CountryIterations    int    `json:"country_iterations"`
	DisciplineIterations int    `json:"discipline_iterations"`
	Normalization        string `json:"normalization"`
	Models               string `json:"models"`
	ModelsSHA256         string `json:"models_sha256"`
	ResidualKind         string `json:"residual_kind"`
}

type Manifest struct {
	Version                int                   `json:"version"`
	BaseRunID              string                `json:"base_run_id"`
	Corpus                 string                `json:"corpus"`
	SourceCompletionSHA256 string                `json:"source_completion_sha256"`
	BuildID                string                `json:"build_id"`
	ScenarioID             string                `json:"scenario_id"`
	Methods                []Definition          `json:"methods"`
	Sources                []checkpoint.Artifact `json:"sources"`
	Notes                  []string              `json:"notes"`
}

type Completion struct {
	Version        int                   `json:"version"`
	Status         string                `json:"status"`
	BaseRunID      string                `json:"base_run_id"`
	ManifestSHA256 string                `json:"manifest_sha256"`
	Artifacts      []checkpoint.Artifact `json:"artifacts"`
}

type Model struct {
	ScenarioID            string                    `json:"scenario_id"`
	Year                  int                       `json:"year"`
	Scheme                string                    `json:"scheme"`
	Level                 string                    `json:"level"`
	Metric                string                    `json:"metric"`
	Result                eci.Result                `json:"result"`
	NetworkSHA256         string                    `json:"network_sha256"`
	ReflectionDiagnostics eci.ReflectionDiagnostics `json:"reflection_diagnostics"`
	TemporalSignFlipped   bool                      `json:"temporal_sign_flipped"`
}
