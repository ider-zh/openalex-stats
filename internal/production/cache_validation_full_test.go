package production

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"openalexstats/internal/cache"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/pipeline"
)

// TestFullCacheValidationBenchmark is deliberately opt-in: an ordinary go test
// must never read a production snapshot. A dedicated output path is required so
// complete correctness/resource evidence survives the subprocess.
func TestFullCacheValidationBenchmark(t *testing.T) {
	directory := os.Getenv("OPENALEX_FULL_CACHE_BENCH_DIRECTORY")
	if directory == "" {
		t.Skip("full cache benchmark disabled; explicit environment is required")
	}
	sourceManifest := os.Getenv("OPENALEX_FULL_CACHE_BENCH_MANIFEST")
	output := os.Getenv("OPENALEX_FULL_CACHE_BENCH_OUTPUT")
	mode := os.Getenv("OPENALEX_FULL_CACHE_BENCH_MODE")
	if sourceManifest == "" || output == "" || (mode != "serial" && mode != "parallel") {
		t.Fatal("full cache benchmark needs MANIFEST, OUTPUT and MODE=serial|parallel")
	}
	workers := 1
	if mode == "parallel" {
		var err error
		workers, err = strconv.Atoi(os.Getenv("OPENALEX_FULL_CACHE_BENCH_WORKERS"))
		if err != nil || workers < 1 {
			t.Fatal("parallel benchmark requires positive WORKERS")
		}
	}
	input, err := pipeline.Resolve(sourceManifest, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var expected cache.Manifest
	if err := checkpoint.ReadJSON(filepath.Join(directory, "manifest.json"), &expected); err != nil {
		t.Fatal(err)
	}
	bySource := make(map[string]cache.Part, len(expected.Parts))
	for _, part := range expected.Parts {
		bySource[part.InputPath] = part
	}
	parts := make([]cache.Part, len(input.Paths))
	var rows, bytes int64
	for i, path := range input.Paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		part, ok := bySource[absolute]
		if !ok {
			t.Fatalf("metadata missing raw source %s", absolute)
		}
		parts[i] = part
		rows += part.Rows
		bytes += part.Bytes
	}
	expected.Parts, expected.Rows, expected.Bytes = parts, rows, bytes
	wantKey, err := checkpoint.Key(expected)
	if err != nil {
		t.Fatal(err)
	}
	stop := pipeline.StartResourceSampler()
	started := time.Now()
	effective := 1
	var actual cache.Manifest
	if mode == "serial" {
		actual, err = loadCache(context.Background(), directory, input, func(string, any) {})
	} else {
		actual, err = loadCacheParallel(context.Background(), directory, input, workers, func(stage string, detail any) {
			if stage == "cache_validation_workers" {
				effective = detail.(map[string]any)["workers"].(int)
			}
		})
	}
	elapsed := time.Since(started).Seconds()
	resources := stop()
	status := "passed"
	var failures []string
	if err != nil {
		status = "failed"
		failures = append(failures, err.Error())
	}
	actualKey, keyErr := checkpoint.Key(actual)
	if keyErr != nil {
		status = "failed"
		failures = append(failures, keyErr.Error())
	}
	ordered := len(actual.Parts) == len(input.Paths)
	if ordered {
		for i, path := range input.Paths {
			absolute, e := filepath.Abs(path)
			if e != nil || actual.Parts[i].InputPath != absolute {
				ordered = false
				break
			}
		}
	}
	if actualKey != wantKey || actual.Rows != input.ManifestWorks || !ordered {
		status = "failed"
		failures = append(failures, "manifest hash/row/input-order conservation failed")
	}
	report := struct {
		Version                 string                   `json:"version"`
		Status                  string                   `json:"status"`
		Mode                    string                   `json:"mode"`
		RequestedWorkers        int                      `json:"requested_workers"`
		EffectiveWorkers        int                      `json:"effective_workers"`
		SourceManifestSHA256    string                   `json:"source_manifest_sha256"`
		Rows                    int64                    `json:"rows"`
		Bytes                   int64                    `json:"bytes"`
		Files                   int                      `json:"files"`
		ExpectedManifestSHA256  string                   `json:"expected_manifest_sha256"`
		ValidatedManifestSHA256 string                   `json:"validated_manifest_sha256"`
		InputOrderEqual         bool                     `json:"input_order_equal"`
		ElapsedSeconds          float64                  `json:"elapsed_seconds"`
		Resources               pipeline.ResourceSummary `json:"resources"`
		Failures                []string                 `json:"failures,omitempty"`
	}{"full-cache-validation-benchmark-v1", status, mode, workers, effective, input.ManifestSHA256, actual.Rows, actual.Bytes, len(actual.Parts), wantKey, actualKey, ordered, elapsed, resources, failures}
	if err := checkpoint.JSON(output, report); err != nil {
		t.Fatal(err)
	}
	t.Logf("mode=%s requested=%d effective=%d files=%d rows=%d bytes=%d manifest=%s elapsed=%.6fs", mode, workers, effective, report.Files, report.Rows, report.Bytes, actualKey, elapsed)
	if status != "passed" {
		t.Fatal(fmt.Sprintf("full cache benchmark failed: %v", failures))
	}
}
