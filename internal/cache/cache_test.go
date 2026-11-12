package cache

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/model"
	"openalexstats/internal/scan"
)

func pointer[T any](v T) *T { return &v }

type rawAuthorship struct {
	Countries []*string `parquet:"countries,list"`
}
type rawID struct {
	ID *string `parquet:"id"`
}
type rawTopic struct {
	Field    *rawID `parquet:"field"`
	Subfield *rawID `parquet:"subfield"`
}
type rawConcept struct {
	ID    *string  `parquet:"id"`
	Level *int32   `parquet:"level"`
	Score *float32 `parquet:"score"`
}
type rawWork struct {
	ID          *string          `parquet:"id"`
	Year        *int32           `parquet:"publication_year"`
	Type        *string          `parquet:"type"`
	IsXPAC      *bool            `parquet:"is_xpac"`
	IsParatext  *bool            `parquet:"is_paratext"`
	IsRetracted *bool            `parquet:"is_retracted"`
	Authorships []*rawAuthorship `parquet:"authorships,list"`
	Topic       *rawTopic        `parquet:"primary_topic"`
	Concepts    []*rawConcept    `parquet:"concepts,list"`
	References  []*string        `parquet:"referenced_works,list"`
	Unneeded    string           `parquet:"abstract_inverted_index"`
}

func raw(id string, year int32) rawWork {
	return rawWork{ID: pointer(id), Year: pointer(year), Type: pointer("article"), IsXPAC: pointer(false), IsParatext: pointer(false), IsRetracted: pointer(false)}
}
func writeRaw(t *testing.T, path string, rows []rawWork) {
	t.Helper()
	if err := parquet.WriteFile(path, rows); err != nil {
		t.Fatal(err)
	}
}
func options(t *testing.T) Options {
	return Options{Directory: filepath.Join(t.TempDir(), "cache"), Workers: 2, BuildID: "test-build-1", ManifestSHA256: "test-manifest", RowGroupRows: 2}
}
func fixturePaths(t *testing.T) []string {
	t.Helper()
	dir := t.TempDir()
	first := raw("https://openalex.org/W1", 1995)
	first.IsXPAC = nil
	first.Authorships = []*rawAuthorship{{Countries: []*string{pointer("US"), pointer("CN"), pointer("CN")}}}
	first.Topic = &rawTopic{Field: &rawID{ID: pointer("https://openalex.org/fields/17")}, Subfield: &rawID{ID: pointer("https://openalex.org/subfields/1702")}}
	first.Concepts = []*rawConcept{{ID: pointer("C2"), Level: pointer(int32(1)), Score: pointer(float32(0.3))}, {ID: pointer("C1"), Level: pointer(int32(0)), Score: pointer(float32(0.8))}, {ID: pointer("C3"), Level: nil, Score: pointer(float32(0.8))}}
	first.References = []*string{pointer("W2"), nil, pointer("bad")}
	first.Unneeded = strings.Repeat("ignore this", 10000)
	second := raw("W2", 2001)
	second.Year = nil
	third := raw("W3", 2002)
	third.References = []*string{pointer("W1"), pointer("W2")}
	paths := []string{filepath.Join(dir, "a.parquet"), filepath.Join(dir, "b.parquet")}
	writeRaw(t, paths[0], []rawWork{first, second})
	writeRaw(t, paths[1], []rawWork{third})
	return paths
}
func collect(t *testing.T, paths []string, opts scan.Options, cached bool) ([]model.Work, scan.Stats) {
	t.Helper()
	var works []model.Work
	yield := func(w model.Work) error { works = append(works, w.Clone()); return nil }
	var stats scan.Stats
	var err error
	if cached {
		stats, err = Scan(context.Background(), paths, opts, yield)
	} else {
		stats, err = scan.Parquet(context.Background(), paths, opts, yield)
	}
	if err != nil {
		t.Fatal(err)
	}
	for i := range works {
		sort.Slice(works[i].Concepts, func(a, b int) bool {
			if works[i].Concepts[a].Level != works[i].Concepts[b].Level {
				return works[i].Concepts[a].Level < works[i].Concepts[b].Level
			}
			return works[i].Concepts[a].ID < works[i].Concepts[b].ID
		})
	}
	return works, stats
}

func TestRoundTripProjectionQualityAndVerifiedReuse(t *testing.T) {
	paths := fixturePaths(t)
	opts := options(t)
	opts.ExpectedRows = 3
	m, err := Prepare(context.Background(), paths, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Complete || m.Rows != 3 || len(m.Parts) != 2 {
		t.Fatalf("bad manifest: %+v", m)
	}
	for _, read := range []scan.Options{{ReadDimensions: true, ReadReferences: true, ReaderWorkers: 2}, {ReadDimensions: true}, {ReadReferences: true}, {}, {ReadDimensions: true, ReadReferences: true, MaxRows: 1}} {
		rawWorks, rawStats := collect(t, paths, read, false)
		cachedWorks, cachedStats := collect(t, m.Paths(), read, true)
		if !reflect.DeepEqual(rawWorks, cachedWorks) {
			t.Fatalf("round trip changed projected data: raw=%+v cached=%+v", rawWorks, cachedWorks)
		}
		if rawStats.Rows != cachedStats.Rows || rawStats.InvalidReferenceIDs != cachedStats.InvalidReferenceIDs || rawStats.InvalidConcepts != cachedStats.InvalidConcepts || rawStats.MissingYears != cachedStats.MissingYears || rawStats.NullXPACFlags != cachedStats.NullXPACFlags {
			t.Fatalf("quality lost: raw=%+v cached=%+v", rawStats, cachedStats)
		}
	}
	before, err := os.Stat(m.Parts[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := Prepare(context.Background(), paths, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.InputSHA256 != m2.InputSHA256 || !reflect.DeepEqual(m.Paths(), m2.Paths()) {
		t.Fatal("stable source identities changed")
	}
	for _, p := range m2.Parts {
		if !p.Reused {
			t.Fatal("verified complete part not reused")
		}
	}
	after, _ := os.Stat(m2.Parts[0].Path)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("reused artifact was rewritten")
	}
	var persisted Manifest
	b, err := os.ReadFile(filepath.Join(opts.Directory, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = jsonUnmarshal(b, &persisted); err != nil {
		t.Fatal(err)
	}
	if !persisted.Complete || persisted.Rows != 3 {
		t.Fatal("manifest was not committed")
	}
}

func TestCancellationResumesOnlyCompleteShards(t *testing.T) {
	paths := fixturePaths(t)
	opts := options(t)
	opts.Workers = 1
	ctx, cancel := context.WithCancel(context.Background())
	_, err := Prepare(ctx, paths, opts, func(p Progress) {
		if p.CompletedFiles == 1 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(opts.Directory, "manifest.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("incomplete global manifest published")
	}
	m, err := Prepare(context.Background(), paths, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Parts[0].Reused || m.Parts[1].Reused {
		t.Fatalf("wrong shard reuse after cancellation: %+v", m.Parts)
	}
	partials, _ := filepath.Glob(filepath.Join(opts.Directory, "*.tmp"))
	if len(partials) != 0 {
		t.Fatalf("uncommitted temporary files remain: %v", partials)
	}
}

func TestCacheContentCorruptionAndMetadataCorruptionRebuild(t *testing.T) {
	paths := fixturePaths(t)
	opts := options(t)
	m, err := Prepare(context.Background(), paths, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(m.Parts[0].Path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if _, err = f.ReadAt(b[:], 20); err != nil {
		t.Fatal(err)
	}
	b[0] ^= 0xFF
	if _, err = f.WriteAt(b[:], 20); err != nil {
		t.Fatal(err)
	}
	if err = f.Sync(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	repaired, err := Prepare(context.Background(), paths, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.Parts[0].Reused || !repaired.Parts[1].Reused {
		t.Fatal("corrupted shard was trusted or good shard rebuilt")
	}
	if repaired.Parts[0].SHA256 != m.Parts[0].SHA256 {
		t.Fatal("repair did not restore deterministic normalized content")
	}
	if err = os.WriteFile(m.Parts[1].Path+".meta.json", []byte("truncated"), 0600); err != nil {
		t.Fatal(err)
	}
	repaired2, err := Prepare(context.Background(), paths, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !repaired2.Parts[0].Reused || repaired2.Parts[1].Reused {
		t.Fatal("invalid sidecar was trusted")
	}
}

func TestSourceChangesBuildChangesAndExpectedRows(t *testing.T) {
	paths := fixturePaths(t)
	opts := options(t)
	m, err := Prepare(context.Background(), paths, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(paths[0])
	writeRaw(t, paths[0], []rawWork{raw("W9", 2001), raw("W10", 2002)})
	if err = os.Chtimes(paths[0], time.Now(), info.ModTime().Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	m2, err := Prepare(context.Background(), paths, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m2.Parts[0].Reused || m2.Parts[0].Path == m.Parts[0].Path || !m2.Parts[1].Reused {
		t.Fatal("changed source reused stale shard")
	}
	opts.BuildID = "test-build-2"
	m3, err := Prepare(context.Background(), paths, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range m3.Parts {
		if p.Reused {
			t.Fatal("changed build reused prior schema/logic")
		}
	}
	opts.ExpectedRows = 999
	if _, err := Prepare(context.Background(), paths, opts, nil); err == nil {
		t.Fatal("row mismatch accepted")
	}
}

func TestDirectoryLockAndInvalidFormat(t *testing.T) {
	paths := fixturePaths(t)
	opts := options(t)
	if err := os.MkdirAll(opts.Directory, 0755); err != nil {
		t.Fatal(err)
	}
	unlock, err := lockDirectory(opts.Directory)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Prepare(context.Background(), paths, opts, nil)
	unlock()
	if err == nil {
		t.Fatal("concurrent cache builder accepted")
	}
	_, err = Scan(context.Background(), paths, scan.Options{}, func(model.Work) error { return nil })
	if err == nil {
		t.Fatal("raw parquet accepted as normalized cache")
	}
}

type crashFixture struct {
	Paths   []string
	Options Options
}

func TestCrashHelper(t *testing.T) {
	payload := os.Getenv("OPENALEX_CACHE_CRASH_FIXTURE")
	if payload == "" {
		return
	}
	var input crashFixture
	if err := json.Unmarshal([]byte(payload), &input); err != nil {
		t.Fatal(err)
	}
	_, err := Prepare(context.Background(), input.Paths, input.Options, func(p Progress) {
		if p.Stage == "complete" && p.CompletedFiles == 1 {
			if err := os.WriteFile(filepath.Join(input.Options.Directory, ".crash-ready"), []byte("ready"), 0600); err != nil {
				t.Fatal(err)
			}
			for {
				time.Sleep(time.Hour)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHardProcessInterruptionReleasesLockAndResumes(t *testing.T) {
	paths := fixturePaths(t)
	opts := options(t)
	opts.Workers = 1
	payload, err := json.Marshal(crashFixture{paths, opts})
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestCrashHelper$")
	child.Env = append(os.Environ(), "OPENALEX_CACHE_CRASH_FIXTURE="+string(payload))
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	ready := filepath.Join(opts.Directory, ".crash-ready")
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			child.Process.Kill()
			child.Wait()
			t.Fatal("child did not publish a complete shard")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	child.Wait()
	orphan := filepath.Join(opts.Directory, ".part-abandoned.tmp")
	if err := os.WriteFile(orphan, []byte("uncommitted"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := Prepare(context.Background(), paths, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Complete || !m.Parts[0].Reused || m.Parts[1].Reused {
		t.Fatalf("hard-crash recovery failed: %+v", m.Parts)
	}
	if _, err := os.Stat(orphan); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("orphaned temporary shard was not cleaned")
	}
}

// Keeping json handling here avoids weakening the test with file-name-only checks.
func jsonUnmarshal(b []byte, target any) error { return json.Unmarshal(b, target) }
