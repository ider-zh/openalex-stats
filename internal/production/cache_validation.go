package production

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"openalexstats/internal/cache"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/pipeline"
)

// loadCacheParallel validates exactly the selected source files, preserving the
// input order and the serial loader's returned manifest semantics. Every cache
// shard is hashed in full; it never substitutes metadata-only validation. Worker
// count is capped by GOMAXPROCS, and each worker reuses one 1MiB hash buffer.
func loadCacheParallel(ctx context.Context, directory string, input pipeline.Input, workers int, progress Progress) (cache.Manifest, error) {
	var manifest cache.Manifest
	if ctx == nil {
		return manifest, fmt.Errorf("nil cache validation context")
	}
	if err := ctx.Err(); err != nil {
		return manifest, err
	}
	if workers < 1 {
		return manifest, fmt.Errorf("cache validation workers must be positive")
	}
	if err := checkpoint.ReadJSON(filepath.Join(directory, "manifest.json"), &manifest); err != nil {
		return manifest, err
	}
	if !manifest.Complete || manifest.FormatVersion != cache.FormatVersion {
		return manifest, fmt.Errorf("normalized cache not complete or format unsupported")
	}
	if input.ManifestSHA256 != "" && manifest.SourceManifest != input.ManifestSHA256 {
		return manifest, fmt.Errorf("cache belongs to a different snapshot manifest")
	}
	bySource := make(map[string]cache.Part, len(manifest.Parts))
	for _, part := range manifest.Parts {
		if _, duplicate := bySource[part.InputPath]; duplicate {
			return manifest, fmt.Errorf("cache manifest has duplicate raw source %s", part.InputPath)
		}
		bySource[part.InputPath] = part
	}
	selected := make([]cache.Part, len(input.Paths))
	workers = min(workers, runtime.GOMAXPROCS(0), len(input.Paths))
	if progress == nil {
		progress = func(string, any) {}
	}
	progress("cache_validation_workers", map[string]any{"workers": workers, "global_cpu_budget": runtime.GOMAXPROCS(0), "hash_buffer_bytes_per_worker": 1 << 20, "files": len(input.Paths)})
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	jobs := make(chan int)
	var wg sync.WaitGroup
	var once sync.Once
	var progressMu sync.Mutex
	completed := 0
	var completedBytes int64
	fail := func(err error) { once.Do(func() { cancel(err) }) }
	for range workers {
		wg.Go(func() {
			buffer := make([]byte, 1<<20)
			digest := sha256.New()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				path := input.Paths[i]
				absolute, err := filepath.Abs(path)
				if err != nil {
					fail(err)
					return
				}
				part, ok := bySource[absolute]
				if !ok {
					fail(fmt.Errorf("cache missing raw source %s", path))
					return
				}
				if part.Rows < 0 || part.Bytes < 0 {
					fail(fmt.Errorf("negative cache row or byte count: %s", part.Path))
					return
				}
				info, err := os.Stat(path)
				if err != nil {
					fail(err)
					return
				}
				if info.Size() != part.InputSize || info.ModTime().UnixNano() != part.InputModTimeNS {
					fail(fmt.Errorf("raw input changed since projection: %s", path))
					return
				}
				artifact := checkpoint.Artifact{Path: part.Path, Bytes: part.Bytes, SHA256: part.SHA256}
				if err := verifyCacheArtifactContext(ctx, artifact, digest, buffer); err != nil {
					fail(err)
					return
				}
				selected[i] = part
				progressMu.Lock()
				completed++
				completedBytes += part.Bytes
				if completed == 1 || completed%100 == 0 || completed == len(input.Paths) {
					progress("cache_verified", map[string]any{"files": completed, "total": len(input.Paths), "verified_bytes": completedBytes})
				}
				progressMu.Unlock()
			}
		})
	}
dispatch:
	for i := range input.Paths {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	if cause := context.Cause(ctx); cause != nil {
		return manifest, cause
	}
	var rows, bytes int64
	for _, part := range selected {
		if rows > math.MaxInt64-part.Rows || bytes > math.MaxInt64-part.Bytes {
			return manifest, fmt.Errorf("cache manifest total exceeds int64")
		}
		rows += part.Rows
		bytes += part.Bytes
	}
	manifest.Parts = selected
	manifest.Rows = rows
	manifest.Bytes = bytes
	return manifest, nil
}

func verifyCacheArtifactContext(ctx context.Context, artifact checkpoint.Artifact, digest hash.Hash, buffer []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Stat(artifact.Path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != artifact.Bytes {
		return fmt.Errorf("artifact checksum/size mismatch: %s", artifact.Path)
	}
	f, err := os.Open(artifact.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	digest.Reset()
	var count int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := f.Read(buffer)
		if n > 0 {
			count += int64(n)
			if _, err := digest.Write(buffer[:n]); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if count != artifact.Bytes || hex.EncodeToString(digest.Sum(nil)) != artifact.SHA256 {
		return fmt.Errorf("artifact checksum mismatch: %s", artifact.Path)
	}
	return nil
}
