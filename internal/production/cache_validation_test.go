package production

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"openalexstats/internal/cache"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/pipeline"
)

func cacheValidationFixture(t *testing.T) (string, pipeline.Input, cache.Manifest) {
	t.Helper()
	paths := dimensionFixture(t)
	directory := filepath.Dir(paths[0])
	var manifest cache.Manifest
	if err := checkpoint.ReadJSON(filepath.Join(directory, "manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	input := pipeline.Input{ManifestSHA256: manifest.SourceManifest}
	for _, part := range manifest.Parts {
		input.Paths = append(input.Paths, part.InputPath)
	}
	return directory, input, manifest
}

func TestParallelCacheValidationMatchesSerialAndInputOrder(t *testing.T) {
	directory, input, manifest := cacheValidationFixture(t)
	input.Paths = []string{input.Paths[3], input.Paths[1], input.Paths[0]}
	want, err := loadCache(context.Background(), directory, input, func(string, any) {})
	if err != nil {
		t.Fatal(err)
	}
	for _, workers := range []int{1, 2, 46} {
		got, err := loadCacheParallel(context.Background(), directory, input, workers, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("workers=%d differs from complete serial validation", workers)
		}
		if got.Parts[0].Path != manifest.Parts[3].Path || got.Parts[1].Path != manifest.Parts[1].Path || got.Parts[2].Path != manifest.Parts[0].Path || got.Rows != 6 {
			t.Fatal("worker completion order changed selected parts")
		}
	}
}

func TestParallelCacheValidationCancellationAndWorkerCap(t *testing.T) {
	directory, input, _ := cacheValidationFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loadCacheParallel(ctx, directory, input, 2, func(string, any) { t.Fatal("pre-cancelled loader called progress") }); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancel: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	if _, err := loadCacheParallel(ctx, directory, input, 2, func(stage string, _ any) {
		if stage == "cache_verified" {
			cancel()
		}
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel during verification: %v", err)
	}
	prior := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(prior)
	seen := 0
	_, err := loadCacheParallel(context.Background(), directory, input, 1000, func(stage string, detail any) {
		if stage == "cache_validation_workers" {
			seen = detail.(map[string]any)["workers"].(int)
		}
	})
	if err != nil || seen != 2 {
		t.Fatalf("global CPU cap not honored: workers=%d error=%v", seen, err)
	}
	if _, err := loadCacheParallel(context.Background(), directory, input, 0, nil); err == nil {
		t.Fatal("zero worker budget accepted")
	}
}

func TestParallelCacheValidationRejectsCorruptOrMissingShard(t *testing.T) {
	for _, damage := range []string{"same_size_content", "missing_file", "wrong_metadata_sha", "negative_rows"} {
		t.Run(damage, func(t *testing.T) {
			directory, input, m := cacheValidationFixture(t)
			switch damage {
			case "same_size_content":
				corruptArtifact(t, m.Parts[2].Path)
			case "missing_file":
				if err := os.Remove(m.Parts[2].Path); err != nil {
					t.Fatal(err)
				}
			case "wrong_metadata_sha":
				m.Parts[2].SHA256 = "invalid-sha"
				if err := checkpoint.JSON(filepath.Join(directory, "manifest.json"), m); err != nil {
					t.Fatal(err)
				}
			case "negative_rows":
				m.Parts[2].Rows = -1
				if err := checkpoint.JSON(filepath.Join(directory, "manifest.json"), m); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := loadCacheParallel(context.Background(), directory, input, 4, nil); err == nil {
				t.Fatal("damaged cache shard was accepted")
			}
		})
	}
}

func TestParallelCacheValidationRejectsChangedSourceAndManifest(t *testing.T) {
	for _, damage := range []string{"source_mtime", "wrong_snapshot", "incomplete", "format", "duplicate_source", "missing_source"} {
		t.Run(damage, func(t *testing.T) {
			directory, input, m := cacheValidationFixture(t)
			switch damage {
			case "source_mtime":
				info, err := os.Stat(input.Paths[1])
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(input.Paths[1], time.Now(), info.ModTime().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			case "wrong_snapshot":
				input.ManifestSHA256 = "another-snapshot"
			case "incomplete":
				m.Complete = false
			case "format":
				m.FormatVersion = "unsupported"
			case "duplicate_source":
				m.Parts = append(m.Parts, m.Parts[0])
			case "missing_source":
				input.Paths = append(input.Paths, filepath.Join(directory, "not-in-cache.parquet"))
			}
			if damage == "incomplete" || damage == "format" || damage == "duplicate_source" {
				if err := checkpoint.JSON(filepath.Join(directory, "manifest.json"), m); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := loadCacheParallel(context.Background(), directory, input, 4, nil); err == nil {
				t.Fatal("invalid input identity accepted")
			}
		})
	}
}

func TestCacheArtifactHashChecksContextAndFullContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.bin")
	data := make([]byte, 8193)
	for i := range data {
		data[i] = byte(i * 37)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	artifact, err := checkpoint.Hash(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.New()
	buffer := make([]byte, 17)
	if err := verifyCacheArtifactContext(context.Background(), artifact, digest, buffer); err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 1
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyCacheArtifactContext(context.Background(), artifact, digest, buffer); err == nil {
		t.Fatal("last-byte corruption escaped full-file hashing")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyCacheArtifactContext(ctx, artifact, digest, buffer); !errors.Is(err, context.Canceled) {
		t.Fatalf("hash context ignored: %v", err)
	}
}
