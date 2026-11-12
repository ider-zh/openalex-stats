package production

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/cache"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/model"
	"openalexstats/internal/policy"
	"openalexstats/internal/store"
)

type dimensionRaw struct {
	ID          string           `parquet:"id"`
	Year        *int32           `parquet:"publication_year"`
	Type        string           `parquet:"type"`
	XPAC        *bool            `parquet:"is_xpac"`
	Paratext    bool             `parquet:"is_paratext"`
	Retracted   bool             `parquet:"is_retracted"`
	Authorships []prodAuthorship `parquet:"authorships,list"`
	Topic       *prodTopic       `parquet:"primary_topic"`
	Concepts    []prodConcept    `parquet:"concepts,list"`
	Refs        []string         `parquet:"referenced_works,list"`
}

func dimensionFixture(t *testing.T) []string {
	t.Helper()
	makeRow := func(id string, year int32) dimensionRaw {
		return dimensionRaw{ID: id, Year: prodPtr(year), Type: "article", XPAC: prodPtr(false), Authorships: []prodAuthorship{{Countries: []string{"US"}}}, Topic: &prodTopic{Field: &prodHierarchy{ID: "F1"}, Subfield: &prodHierarchy{ID: "S1"}}, Concepts: []prodConcept{{ID: "C0", Level: 0, Score: .8}, {ID: "C1", Level: 1, Score: .8}}}
	}
	a, b, c, d, e, f, g := makeRow("W100", 1995), makeRow("W101", 2001), makeRow("W200", 2000), makeRow("W201", 2001), makeRow("W300", 2001), makeRow("W400", 2002), makeRow("W401", 2002)
	a.Concepts = []prodConcept{{ID: "C0", Level: 0, Score: .5}, {ID: "C9", Level: 1, Score: .2}}
	b.Type = "dataset"
	b.Concepts = []prodConcept{{ID: "C0", Level: 0, Score: .9}}
	c.XPAC = prodPtr(true)
	c.Concepts = []prodConcept{{ID: "C1", Level: 1, Score: .6}}
	c.Authorships = []prodAuthorship{{Countries: []string{"CN"}}}
	c.Topic = &prodTopic{Field: &prodHierarchy{ID: "F2"}, Subfield: &prodHierarchy{ID: "S2"}}
	d.XPAC = nil
	e.Year = nil
	e.Type = "other"
	e.Authorships = nil
	e.Topic = nil
	e.Concepts = nil
	e.Retracted = true
	f.Authorships = []prodAuthorship{{Countries: []string{"MX"}}}
	f.Topic = &prodTopic{Field: &prodHierarchy{ID: "F3"}, Subfield: &prodHierarchy{ID: "S3"}}
	f.Concepts = []prodConcept{{ID: "C0", Level: 0, Score: .1}, {ID: "C1", Level: 1, Score: .4}}
	g.Type = "report"
	g.Paratext = true
	dir := t.TempDir()
	var paths []string
	for i, rows := range [][]dimensionRaw{{a, b}, {c, d}, {e}, {f, g}} {
		path := filepath.Join(dir, string(rune('a'+i))+".parquet")
		if err := parquet.WriteFile(path, rows); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	m, err := cache.Prepare(context.Background(), paths, cache.Options{Directory: filepath.Join(dir, "cache"), Workers: 2, BuildID: "dimension-test", ManifestSHA256: "dimension-test", ExpectedRows: 7}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m.Paths()
}

func dimensionScope(t *testing.T, corpus string, start int) policy.Scope {
	t.Helper()
	scope, err := policy.New(corpus, start, policy.DefaultBlacklist)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}
func dimensionViews(c *store.Columns) []model.Work {
	var result []model.Work
	var scratch model.Work
	for i := 0; i < c.Len(); i++ {
		c.View(i, &scratch)
		result = append(result, scratch.Clone())
	}
	return result
}

func TestParallelDimensionsStableOrdinalsInventoryAndQuality(t *testing.T) {
	paths := dimensionFixture(t)
	scope := dimensionScope(t, "core", 0)
	one, err := dimensions(context.Background(), paths, t.TempDir(), "q-v1", scope, 64<<20, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	many, err := dimensions(context.Background(), paths, t.TempDir(), "q-v1", scope, 64<<20, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(one.Columns.IDs(), []uint64{100, 300, 400}) || !reflect.DeepEqual(one.AllIDs, []uint64{100, 101, 200, 201, 300, 400, 401}) {
		t.Fatalf("source or Q order changed: %v / %v", one.Columns.IDs(), one.AllIDs)
	}
	if !reflect.DeepEqual(dimensionViews(one.Columns), dimensionViews(many.Columns)) || !reflect.DeepEqual(one.AllIDs, many.AllIDs) || one.Checkpoint.Columns.SHA256 != many.Checkpoint.Columns.SHA256 || !reflect.DeepEqual(one.Checkpoint.Inventory, many.Checkpoint.Inventory) || one.Checkpoint.Scan != many.Checkpoint.Scan {
		t.Fatal("parallel schedule changed dimensions, inventory or serialization")
	}
	inv := many.Checkpoint.Inventory
	if inv.Rows != 7 || inv.Selected != 3 || inv.SelectedRetracted != 1 || inv.Core.Works != 5 || inv.XPAC.Works != 1 || inv.NullXPAC.Works != 1 {
		t.Fatalf("group counts: %+v", inv)
	}
	if inv.Core.MissingYear != 1 || inv.Core.MissingCountry != 1 || inv.Core.MissingField != 1 || inv.Core.MissingSubfield != 1 || inv.Core.MissingConcept0 != 2 || inv.Core.MissingConcept1 != 3 || inv.XPAC.MissingConcept0 != 1 {
		t.Fatalf("coverage changed: %+v", inv)
	}
	if inv.ExcludedReasons["type:dataset"] != 1 || inv.ExcludedReasons["xpac"] != 1 || inv.ExcludedReasons["xpac_unknown"] != 1 || inv.ExcludedReasons["is_paratext"] != 1 || inv.TypeCounts["article"] != [3]uint64{2, 1, 1} {
		t.Fatalf("exclusion/types changed: %+v", inv)
	}
	if many.Checkpoint.Scan.Rows != 7 || many.Checkpoint.Scan.MissingYears != 1 || many.Checkpoint.Scan.NullXPACFlags != 1 {
		t.Fatalf("quality lost: %+v", many.Checkpoint.Scan)
	}
	boundary, err := dimensions(context.Background(), paths, t.TempDir(), "q-from-2000", dimensionScope(t, "core", 2000), 64<<20, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(boundary.Columns.IDs(), []uint64{400}) || boundary.Checkpoint.Inventory.ExcludedReasons["corpus_year"] != 2 {
		t.Fatal("old or undated target boundary not applied")
	}
}

func TestDimensionPartCancellationAndResume(t *testing.T) {
	paths := dimensionFixture(t)
	directory := t.TempDir()
	scope := dimensionScope(t, "core", 0)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := dimensions(ctx, paths, directory, "q-v1", scope, 64<<20, 1, func(stage string, _ any) {
		if stage == "dimension_part_written" {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("did not interrupt dimensions: %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "dimensions.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("incomplete global dimensions were published")
	}
	reused, written := 0, 0
	result, err := dimensions(context.Background(), paths, directory, "q-v1", scope, 64<<20, 3, func(stage string, _ any) {
		if stage == "dimension_part_reused" {
			reused++
		}
		if stage == "dimension_part_written" {
			written++
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if reused != 1 || written != 3 || result.Reused || result.Checkpoint.Inventory.Rows != 7 || result.Columns.Len() != 3 {
		t.Fatalf("wrong resumption: reused=%d written=%d result=%+v", reused, written, result)
	}
	loaded, err := dimensions(context.Background(), paths, directory, "q-v1", scope, 64<<20, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Reused || !reflect.DeepEqual(loaded.AllIDs, result.AllIDs) || !reflect.DeepEqual(dimensionViews(loaded.Columns), dimensionViews(result.Columns)) {
		t.Fatal("whole-stage checkpoint cannot be reused")
	}
}

func TestDimensionPartDamageRebuildAndInputIdentity(t *testing.T) {
	paths := dimensionFixture(t)
	directory := t.TempDir()
	scope := dimensionScope(t, "core", 0)
	baseline, err := dimensions(context.Background(), paths, directory, "q-v1", scope, 64<<20, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	runParts := func(wantWritten int) dimensionResult {
		t.Helper()
		if err := os.Remove(filepath.Join(directory, "dimensions.json")); err != nil {
			t.Fatal(err)
		}
		written := 0
		result, err := dimensions(context.Background(), paths, directory, "q-v1", scope, 64<<20, 3, func(stage string, _ any) {
			if stage == "dimension_part_written" {
				written++
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		if written != wantWritten || result.Checkpoint.Columns.SHA256 != baseline.Checkpoint.Columns.SHA256 || !reflect.DeepEqual(result.AllIDs, baseline.AllIDs) {
			t.Fatalf("bad repair: written=%d want=%d", written, wantWritten)
		}
		return result
	}
	corruptArtifact(t, filepath.Join(directory, "dimension-parts", "part-00000.columns"))
	runParts(1)
	corruptArtifact(t, filepath.Join(directory, "dimension-parts", "part-00001.ids"))
	runParts(1)
	partPath := filepath.Join(directory, "dimension-parts", "part-00002.json")
	var part dimensionPart
	if err := checkpoint.ReadJSON(partPath, &part); err != nil {
		t.Fatal(err)
	}
	part.Shape.Concepts++
	if err := checkpoint.JSON(partPath, part); err != nil {
		t.Fatal(err)
	}
	runParts(1)
	// New cached input metadata invalidates exactly that part and the new
	// global checkpoint, even if a caller accidentally keeps its Q key.
	info, err := os.Stat(paths[3])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(paths[3], time.Now(), info.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	written := 0
	changed, err := dimensions(context.Background(), paths, directory, "q-v1", scope, 64<<20, 3, func(stage string, _ any) {
		if stage == "dimension_part_written" {
			written++
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Reused || written != 1 || changed.Checkpoint.InputsKey == baseline.Checkpoint.InputsKey {
		t.Fatal("input metadata changes reused stale global/part")
	}
}

func TestDimensionKeysLegacyCheckpointAndBudget(t *testing.T) {
	paths := dimensionFixture(t)
	directory := t.TempDir()
	scope := dimensionScope(t, "core", 0)
	result, err := dimensions(context.Background(), paths, directory, "q-v1", scope, 64<<20, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	legacy := result.Checkpoint
	legacy.InputsKey = ""
	legacy.Shape = store.Shape{}
	if err := checkpoint.JSON(filepath.Join(directory, "dimensions.json"), legacy); err != nil {
		t.Fatal(err)
	}
	loaded, err := dimensions(context.Background(), paths, directory, "q-v1", scope, 64<<20, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Reused {
		t.Fatal("valid legacy whole-stage checkpoint not reused")
	}
	written := 0
	all, err := dimensions(context.Background(), paths, directory, "q-all-v2", dimensionScope(t, "all", 0), 64<<20, 3, func(stage string, _ any) {
		if stage == "dimension_part_written" {
			written++
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if written != 4 || all.Reused || all.Columns.Len() != 5 || !reflect.DeepEqual(all.AllIDs, result.AllIDs) {
		t.Fatal("changed Q key reused incompatible dimension parts")
	}
	if _, err := dimensions(context.Background(), paths, t.TempDir(), "small", scope, 1024, 4, nil); err == nil {
		t.Fatal("per-worker dimension budget ignored")
	}
}
