// Package cache creates restartable, verified, normalized Parquet shards.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
	"openalexstats/internal/model"
	"openalexstats/internal/scan"
)

const FormatVersion = "openalexstats-normalized-parquet-v1"

type Options struct {
	Directory      string
	Workers        int
	BuildID        string
	ManifestSHA256 string
	ExpectedRows   int64
	RowGroupRows   int64
}

type Progress struct {
	Stage          string `json:"stage"`
	FileIndex      int    `json:"file_index"`
	InputPath      string `json:"input_path"`
	FileRows       int64  `json:"file_rows"`
	CompletedFiles int    `json:"completed_files"`
	TotalFiles     int    `json:"total_files"`
	ReusedFiles    int    `json:"reused_files"`
	TotalRows      int64  `json:"total_rows"`
}

type Part struct {
	Index             int        `json:"index"`
	InputPath         string     `json:"input_path"`
	InputSize         int64      `json:"input_size"`
	InputModTimeNS    int64      `json:"input_mod_time_ns"`
	SourceFingerprint string     `json:"source_fingerprint"`
	Path              string     `json:"path"`
	Bytes             int64      `json:"bytes"`
	Rows              int64      `json:"rows"`
	SHA256            string     `json:"sha256"`
	Quality           scan.Stats `json:"quality"`
	Reused            bool       `json:"reused"`
}

type Manifest struct {
	FormatVersion  string    `json:"format_version"`
	BuildID        string    `json:"build_id"`
	SourceManifest string    `json:"source_manifest_sha256"`
	InputSHA256    string    `json:"input_sha256"`
	CreatedUTC     time.Time `json:"created_utc"`
	CompletedUTC   time.Time `json:"completed_utc"`
	Complete       bool      `json:"complete"`
	Rows           int64     `json:"rows"`
	Bytes          int64     `json:"bytes"`
	Parts          []Part    `json:"parts"`
}

func (m Manifest) Paths() []string {
	paths := make([]string, len(m.Parts))
	for i := range m.Parts {
		paths[i] = m.Parts[i].Path
	}
	return paths
}

type source struct {
	index int
	path  string
	info  os.FileInfo
	fp    string
	rows  int64
}

type metadata struct {
	FormatVersion  string `json:"format_version"`
	BuildID        string `json:"build_id"`
	SourceManifest string `json:"source_manifest_sha256"`
	Part           Part   `json:"part"`
}

// Prepare preserves every source row and independently publishes each source
// file as one verified shard. Complete shards survive cancellation/crashes.
// Source identity is an absolute path+size+mtime fingerprint tied to the source
// manifest, build and format. The immutable raw snapshot is trusted; this does
// not claim to detect source mutations that preserve both size and mtime.
func Prepare(ctx context.Context, rawPaths []string, opts Options, progress func(Progress)) (manifest Manifest, err error) {
	if err := ctx.Err(); err != nil {
		return manifest, err
	}
	if opts.Directory == "" || opts.BuildID == "" {
		return manifest, errors.New("cache: directory and build ID are required")
	}
	if opts.Workers < 0 || opts.ExpectedRows < 0 || opts.RowGroupRows < 0 {
		return manifest, errors.New("cache: negative options are invalid")
	}
	if len(rawPaths) == 0 {
		return manifest, errors.New("cache: no input files")
	}
	if opts.Workers == 0 {
		opts.Workers = 1
	}
	if opts.Workers > len(rawPaths) {
		opts.Workers = len(rawPaths)
	}
	if opts.RowGroupRows == 0 {
		opts.RowGroupRows = 100000
	}
	opts.Directory, err = filepath.Abs(opts.Directory)
	if err != nil {
		return manifest, err
	}
	if err = os.MkdirAll(opts.Directory, 0755); err != nil {
		return manifest, err
	}
	unlock, err := lockDirectory(opts.Directory)
	if err != nil {
		return manifest, err
	}
	defer unlock()
	manifest = Manifest{FormatVersion: FormatVersion, BuildID: opts.BuildID, SourceManifest: opts.ManifestSHA256, CreatedUTC: time.Now().UTC(), Parts: make([]Part, len(rawPaths))}
	sources := make([]source, len(rawPaths))
	seen := make(map[string]struct{}, len(rawPaths))
	inputHash := sha256.New()
	var footerRows int64
	for i, path := range rawPaths {
		if err := ctx.Err(); err != nil {
			return manifest, err
		}
		path, err = filepath.Abs(path)
		if err != nil {
			return manifest, err
		}
		if _, exists := seen[path]; exists {
			return manifest, fmt.Errorf("cache: duplicate source %q", path)
		}
		seen[path] = struct{}{}
		info, e := os.Stat(path)
		if e != nil {
			return manifest, e
		}
		if !info.Mode().IsRegular() {
			return manifest, fmt.Errorf("cache: source is not a regular file: %q", path)
		}
		rows, e := parquetRows(path)
		if e != nil {
			return manifest, fmt.Errorf("cache inspect %q: %w", path, e)
		}
		fp, e := fingerprint(i, path, info, opts)
		if e != nil {
			return manifest, e
		}
		sources[i] = source{i, path, info, fp, rows}
		footerRows += rows
		io.WriteString(inputHash, fp+"\n")
	}
	manifest.InputSHA256 = hex.EncodeToString(inputHash.Sum(nil))
	if opts.ExpectedRows > 0 && footerRows != opts.ExpectedRows {
		return manifest, fmt.Errorf("cache: input footer rows %d do not match expected %d", footerRows, opts.ExpectedRows)
	}
	// The directory lock excludes live writers. Only our reserved temporary
	// names are removed, and explicitly supplied source files are never removed.
	if err := cleanupTemps(opts.Directory, seen); err != nil {
		return manifest, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan source)
	var wg sync.WaitGroup
	var firstErr error
	var once sync.Once
	var mu sync.Mutex
	completed, reused := 0, 0
	emit := func(p Progress) {
		mu.Lock()
		defer mu.Unlock()
		p.CompletedFiles = completed
		p.TotalFiles = len(sources)
		p.ReusedFiles = reused
		p.TotalRows = manifest.Rows
		if progress != nil {
			progress(p)
		}
	}
	for range opts.Workers {
		wg.Go(func() {
			for s := range jobs {
				if ctx.Err() != nil {
					return
				}
				emit(Progress{Stage: "building", FileIndex: s.index, InputPath: s.path})
				part, e := preparePart(ctx, s, opts, func(n int64) { emit(Progress{Stage: "building", FileIndex: s.index, InputPath: s.path, FileRows: n}) })
				if e != nil {
					once.Do(func() { firstErr = fmt.Errorf("cache source %q: %w", s.path, e); cancel() })
					return
				}
				mu.Lock()
				manifest.Parts[s.index] = part
				manifest.Rows += part.Rows
				manifest.Bytes += part.Bytes
				completed++
				if part.Reused {
					reused++
				}
				mu.Unlock()
				stage := "complete"
				if part.Reused {
					stage = "reused"
				}
				emit(Progress{Stage: stage, FileIndex: s.index, InputPath: s.path, FileRows: part.Rows})
			}
		})
	}
	dispatchDone := false
	for _, s := range sources {
		select {
		case jobs <- s:
		case <-ctx.Done():
			dispatchDone = true
		}
		if dispatchDone {
			break
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return manifest, firstErr
	}
	if err := ctx.Err(); err != nil {
		return manifest, err
	}
	if manifest.Rows != footerRows {
		return manifest, fmt.Errorf("cache: row conservation failed: wrote %d, source %d", manifest.Rows, footerRows)
	}
	manifest.Complete = true
	manifest.CompletedUTC = time.Now().UTC()
	if err := writeJSONAtomic(filepath.Join(opts.Directory, "manifest.json"), manifest); err != nil {
		manifest.Complete = false
		return manifest, err
	}
	return manifest, nil
}

func fingerprint(index int, path string, info os.FileInfo, opts Options) (string, error) {
	b, err := json.Marshal(struct {
		Format, Build, SourceManifest, Path string
		Index                               int
		Size, MTime, RowGroupRows           int64
	}{FormatVersion, opts.BuildID, opts.ManifestSHA256, path, index, info.Size(), info.ModTime().UnixNano(), opts.RowGroupRows})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func parquetRows(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	pf, err := parquet.OpenFile(f, info.Size(), parquet.SkipPageIndex(true), parquet.SkipBloomFilters(true))
	if err != nil {
		return 0, err
	}
	return pf.NumRows(), nil
}

func preparePart(ctx context.Context, s source, opts Options, progress func(int64)) (part Part, err error) {
	path := filepath.Join(opts.Directory, fmt.Sprintf("part-%06d-%s.parquet", s.index, s.fp[:24]))
	if path == s.path {
		return part, errors.New("cache output would replace its source")
	}
	if part, ok, e := reusePart(ctx, path, s, opts); e != nil {
		return part, e
	} else if ok {
		return part, nil
	}
	part = Part{Index: s.index, InputPath: s.path, InputSize: s.info.Size(), InputModTimeNS: s.info.ModTime().UnixNano(), SourceFingerprint: s.fp, Path: path}
	tmp, err := os.CreateTemp(opts.Directory, ".part-*.tmp")
	if err != nil {
		return part, err
	}
	tmpPath := tmp.Name()
	defer func() { tmp.Close(); os.Remove(tmpPath) }()
	hash := sha256.New()
	writer := parquet.NewGenericWriter[normalizedRow](io.MultiWriter(tmp, hash), parquet.Compression(&zstd.Codec{Level: zstd.SpeedFastest, Concurrency: 1}), parquet.MaxRowsPerRowGroup(opts.RowGroupRows), parquet.KeyValueMetadata("openalexstats.cache.format", FormatVersion))
	// A writer owns only one small batch and column-page buffers, never all rows.
	batch := make([]normalizedRow, 0, 128)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		_, e := writer.Write(batch)
		clear(batch)
		batch = batch[:0]
		return e
	}
	var q scan.RowQuality
	stats, err := scan.Parquet(ctx, []string{s.path}, scan.Options{ReadReferences: true, ReadDimensions: true, ReaderWorkers: 1, OnQuality: func(quality scan.RowQuality) { q = quality }}, func(work model.Work) error {
		batch = append(batch, normalize(work, q))
		if len(batch) == cap(batch) {
			if err := flush(); err != nil {
				return err
			}
		}
		part.Rows++
		if part.Rows%50000 == 0 {
			progress(part.Rows)
		}
		return nil
	})
	if err != nil {
		writer.Close()
		return part, err
	}
	part.Quality = stats
	if err = flush(); err != nil {
		writer.Close()
		return part, err
	}
	if err = writer.Close(); err != nil {
		return part, err
	}
	if err = ctx.Err(); err != nil {
		return part, err
	}
	if part.Rows != s.rows || stats.Rows != s.rows {
		return part, fmt.Errorf("row count mismatch: source footer=%d normalized=%d scanner=%d", s.rows, part.Rows, stats.Rows)
	}
	after, err := os.Stat(s.path)
	if err != nil {
		return part, err
	}
	if !os.SameFile(s.info, after) || after.Size() != s.info.Size() || !after.ModTime().Equal(s.info.ModTime()) {
		return part, errors.New("source changed while cache was built")
	}
	if err = tmp.Sync(); err != nil {
		return part, err
	}
	info, err := tmp.Stat()
	if err != nil {
		return part, err
	}
	part.Bytes = info.Size()
	part.SHA256 = hex.EncodeToString(hash.Sum(nil))
	if err = tmp.Close(); err != nil {
		return part, err
	}
	if err = replaceFile(tmpPath, path); err != nil {
		return part, err
	}
	if err = syncDirectory(opts.Directory); err != nil {
		return part, err
	}
	meta := metadata{FormatVersion: FormatVersion, BuildID: opts.BuildID, SourceManifest: opts.ManifestSHA256, Part: part}
	if err = writeJSONAtomic(path+".meta.json", meta); err != nil {
		return part, err
	}
	return part, nil
}

func reusePart(ctx context.Context, path string, s source, opts Options) (Part, bool, error) {
	var meta metadata
	b, err := os.ReadFile(path + ".meta.json")
	if errors.Is(err, os.ErrNotExist) {
		return Part{}, false, nil
	}
	if err != nil {
		return Part{}, false, err
	}
	if json.Unmarshal(b, &meta) != nil {
		return Part{}, false, nil
	}
	p := meta.Part
	if meta.FormatVersion != FormatVersion || meta.BuildID != opts.BuildID || meta.SourceManifest != opts.ManifestSHA256 || p.SourceFingerprint != s.fp || p.Path != path || p.Rows != s.rows || p.Index != s.index {
		return Part{}, false, nil
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Part{}, false, nil
	}
	if err != nil {
		return Part{}, false, err
	}
	if !info.Mode().IsRegular() || info.Size() != p.Bytes {
		return Part{}, false, nil
	}
	actual, err := fileSHA256(ctx, path)
	if err != nil {
		return Part{}, false, err
	}
	if actual != p.SHA256 {
		return Part{}, false, nil
	}
	rows, err := parquetRows(path)
	if err != nil || rows != s.rows {
		return Part{}, false, nil
	}
	p.Reused = true
	return p, true, nil
}

func fileSHA256(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	buffer := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, e := f.Read(buffer)
		if n > 0 {
			hash.Write(buffer[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", e
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeJSONAtomic(path string, value any) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".metadata-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { tmp.Close(); os.Remove(name) }()
	encoder := json.NewEncoder(tmp)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(value); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = replaceFile(name, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func replaceFile(from, to string) error {
	err := os.Rename(from, to)
	if err != nil && runtime.GOOS == "windows" {
		if e := os.Remove(to); e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
		return os.Rename(from, to)
	}
	return err
}

func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func cleanupTemps(directory string, sources map[string]struct{}) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasSuffix(name, ".tmp") || (!strings.HasPrefix(name, ".part-") && !strings.HasPrefix(name, ".metadata-")) {
			continue
		}
		path := filepath.Join(directory, name)
		if _, source := sources[path]; source {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
