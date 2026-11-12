package production

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/cache"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/graph"
	"openalexstats/internal/model"
	"openalexstats/internal/policy"
	"openalexstats/internal/store"
)

func prodPtr[T any](value T) *T { return &value }

type prodAuthorship struct {
	Countries []string `parquet:"countries,list"`
}
type prodHierarchy struct {
	ID string `parquet:"id"`
}
type prodTopic struct {
	Field    *prodHierarchy `parquet:"field"`
	Subfield *prodHierarchy `parquet:"subfield"`
}
type prodConcept struct {
	ID    string  `parquet:"id"`
	Level int32   `parquet:"level"`
	Score float32 `parquet:"score"`
}
type prodRaw struct {
	ID          string           `parquet:"id"`
	Year        *int32           `parquet:"publication_year"`
	Type        string           `parquet:"type"`
	XPAC        bool             `parquet:"is_xpac"`
	Paratext    bool             `parquet:"is_paratext"`
	Retracted   bool             `parquet:"is_retracted"`
	Authorships []prodAuthorship `parquet:"authorships,list"`
	Topic       *prodTopic       `parquet:"primary_topic"`
	Concepts    []prodConcept    `parquet:"concepts,list"`
	Refs        []string         `parquet:"referenced_works,list"`
}

type productionFixture struct {
	paths      []string
	raw        []model.Work
	scope      policy.Scope
	index, all *graph.Index
	columns    *store.Columns
}

func newProductionFixture(t *testing.T, corpusStart int) productionFixture {
	t.Helper()
	scope, err := policy.New("core", corpusStart, policy.DefaultBlacklist)
	if err != nil {
		t.Fatal(err)
	}
	works := []model.Work{
		{ID: 10, Year: 1995, Type: "article"},
		{ID: 20, Year: 2000, Type: "article", References: []uint64{10, 30, 30, 999, 40, 20, 80}},
		{ID: 30, Year: 2001, Type: "article", References: []uint64{10, 20}},
		{ID: 40, Year: 2000, Type: "dataset", References: []uint64{10}},
		{ID: 50, Year: 1999, Type: "article", References: []uint64{10}},
		{ID: 60, Type: "article", References: []uint64{10}},
		{ID: 70, Year: 2001, Type: "article", IsXPAC: true, References: []uint64{10}},
		{ID: 80, Year: 2002, Type: "article"},
	}
	var allIDs []uint64
	columns := store.New()
	var raw []prodRaw
	for i := range works {
		works[i].Countries = []string{"CN"}
		works[i].FieldID = "F17"
		works[i].SubfieldID = "S1702"
		works[i].Concepts = []model.Concept{{ID: "C1", Level: 0, Score: 0.8}}
		w := works[i]
		allIDs = append(allIDs, w.ID)
		if scope.Includes(w) {
			if err := columns.Append(w); err != nil {
				t.Fatal(err)
			}
		}
		r := prodRaw{ID: fmt.Sprintf("https://openalex.org/W%d", w.ID), Type: w.Type, XPAC: w.IsXPAC, Authorships: []prodAuthorship{{Countries: w.Countries}}, Topic: &prodTopic{Field: &prodHierarchy{ID: w.FieldID}, Subfield: &prodHierarchy{ID: w.SubfieldID}}, Concepts: []prodConcept{{ID: "C1", Level: 0, Score: 0.8}}}
		if w.Year != 0 {
			r.Year = prodPtr(int32(w.Year))
		}
		for _, ref := range w.References {
			r.Refs = append(r.Refs, fmt.Sprintf("W%d", ref))
		}
		if w.ID == 20 {
			r.Refs = append(r.Refs, "malformed-reference")
		}
		raw = append(raw, r)
	}
	dir := t.TempDir()
	rawPaths := []string{filepath.Join(dir, "a.parquet"), filepath.Join(dir, "b.parquet")}
	if err := parquet.WriteFile(rawPaths[0], raw[:3]); err != nil {
		t.Fatal(err)
	}
	if err := parquet.WriteFile(rawPaths[1], raw[3:]); err != nil {
		t.Fatal(err)
	}
	m, err := cache.Prepare(context.Background(), rawPaths, cache.Options{Directory: filepath.Join(dir, "normalized"), Workers: 2, BuildID: "production-test", ManifestSHA256: "production-test", ExpectedRows: 8}, nil)
	if err != nil {
		t.Fatal(err)
	}
	index, err := graph.NewIndex(columns.IDs())
	if err != nil {
		t.Fatal(err)
	}
	all, err := graph.NewIndex(allIDs)
	if err != nil {
		t.Fatal(err)
	}
	return productionFixture{m.Paths(), works, scope, index, all, columns}
}

func (f productionFixture) edgeOptions(t *testing.T, partitions int) EdgeOptions {
	t.Helper()
	return EdgeOptions{Directory: filepath.Join(t.TempDir(), "edges"), Key: "edge-scope-v1", Workers: 2, Partitions: partitions, YearStart: 2000, YearEnd: 2002, Scope: f.scope, ExcludeSelfLoops: true, AllIndex: f.all, Columns: f.columns, MaxPartBufferBytes: 1 << 20}
}
func countOptions(t *testing.T) CountOptions {
	t.Helper()
	return CountOptions{Directory: filepath.Join(t.TempDir(), "counts"), Key: "count-scope-v1", Workers: 3, MaxBytes: 1 << 20}
}
func checkCounts(t *testing.T, f productionFixture, c *Counts, want map[uint64][]uint64) {
	t.Helper()
	var sum uint64
	for i, id := range f.index.IDs() {
		expected := want[id]
		if expected == nil {
			expected = []uint64{0, 0, 0}
		}
		got := c.YearCounts(i)
		if !reflect.DeepEqual(got, expected) {
			t.Fatalf("W%d counts=%v want=%v", id, got, expected)
		}
		for _, n := range got {
			sum += n
		}
	}
	if sum != c.Sum {
		t.Fatalf("cell sum=%d total=%d", sum, c.Sum)
	}
}

func TestProductionInducedGraphOldTargetsSelfLoopsAndBuckets(t *testing.T) {
	f := newProductionFixture(t, 0)
	for _, partitions := range []int{1, 2, 4, 7} {
		t.Run(fmt.Sprintf("partitions=%d", partitions), func(t *testing.T) {
			opts := f.edgeOptions(t, partitions)
			edges, err := ProjectEdges(context.Background(), f.paths, f.index, opts, nil)
			if err != nil {
				t.Fatal(err)
			}
			if edges.Stats.RetainedEdges != 5 || edges.Stats.SelfLoops != 1 || edges.Stats.MissingTargetWorks != 1 || edges.Stats.ExcludedTargetWorks != 1 || edges.Stats.OutsideTargetCorpus != 2 || edges.Stats.FutureTargetEdges != 2 {
				t.Fatalf("wrong edge diagnostics: %+v", edges.Stats)
			}
			if edges.Stats.ReferenceSlots != 10 || edges.Stats.InvalidReferenceIDs != 1 || edges.Stats.DuplicateReferences != 1 {
				t.Fatalf("raw malformed slots were lost by normalized input: %+v", edges.Stats)
			}
			if edges.Stats.OutsideSourceCorpus != 2 || edges.Stats.OutsideSourceYearWindow != 2 || edges.Stats.InvalidSourceYears != 1 {
				t.Fatalf("wrong source selection: %+v", edges.Stats)
			}
			var records uint64
			for _, part := range edges.Parts {
				data, err := os.ReadFile(part.Artifact.Path)
				if err != nil {
					t.Fatal(err)
				}
				var position int
				for bucket, n := range part.PartitionCounts {
					for range n {
						if position+8 > len(data) {
							t.Fatal("partition offsets exceed artifact")
						}
						record := binary.LittleEndian.Uint64(data[position : position+8])
						position += 8
						ordinal, year := record>>8, record&255
						if ordinal%uint64(partitions) != uint64(bucket) || ordinal >= uint64(f.index.Len()) || year > 2 {
							t.Fatalf("invalid linear bucket record: %d", record)
						}
						records++
					}
				}
				if position != len(data) || uint64(len(data)) != part.Stats.RetainedEdges*8 {
					t.Fatal("artifact length does not conserve integer edges")
				}
			}
			if records != 5 {
				t.Fatalf("bucket count=%d", records)
			}
			counts, err := ReduceCounts(context.Background(), edges, f.index.Len(), countOptions(t), nil)
			if err != nil {
				t.Fatal(err)
			}
			checkCounts(t, f, counts, map[uint64][]uint64{10: {1, 1, 0}, 20: {0, 1, 0}, 30: {1, 0, 0}, 80: {1, 0, 0}})
			// W30 -> W20 remains despite identical country/field metadata. Only
			// same-work loops are removed, not author or country self-citations.
		})
	}
}

func TestProductionCorpusYearBoundaryExcludesOldTargets(t *testing.T) {
	f := newProductionFixture(t, 2000)
	edges, err := ProjectEdges(context.Background(), f.paths, f.index, f.edgeOptions(t, 3), nil)
	if err != nil {
		t.Fatal(err)
	}
	if edges.Stats.RetainedEdges != 3 || edges.Stats.ExcludedTargetWorks != 3 || edges.Stats.MissingTargetWorks != 1 {
		t.Fatalf("old-target boundary failed: %+v", edges.Stats)
	}
	c, err := ReduceCounts(context.Background(), edges, f.index.Len(), countOptions(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	checkCounts(t, f, c, map[uint64][]uint64{20: {0, 1, 0}, 30: {1, 0, 0}, 80: {1, 0, 0}})
}

func TestProductionSourceAndTargetResumeWithoutDoubleCounting(t *testing.T) {
	f := newProductionFixture(t, 0)
	opts := f.edgeOptions(t, 4)
	opts.Workers = 1
	ctx, cancel := context.WithCancel(context.Background())
	_, err := ProjectEdges(ctx, f.paths, f.index, opts, func(stage string, _ any) {
		if stage == "edges_written" {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("source interruption: %v", err)
	}
	edges, err := ProjectEdges(context.Background(), f.paths, f.index, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if edges.ReusedParts != 1 || edges.Stats.RetainedEdges != 5 {
		t.Fatalf("source resume replayed edges: %+v", edges)
	}
	co := countOptions(t)
	co.Workers = 1
	ctx, cancel = context.WithCancel(context.Background())
	_, err = ReduceCounts(ctx, edges, f.index.Len(), co, func(stage string, _ any) {
		if stage == "counts_written" {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("target interruption: %v", err)
	}
	c, err := ReduceCounts(context.Background(), edges, f.index.Len(), co, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.ReusedPartitions != 1 || c.Sum != 5 {
		t.Fatalf("target resume replayed counts: %+v", c)
	}
	checkCounts(t, f, c, map[uint64][]uint64{10: {1, 1, 0}, 20: {0, 1, 0}, 30: {1, 0, 0}, 80: {1, 0, 0}})
	c2, err := ReduceCounts(context.Background(), edges, f.index.Len(), co, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c2.ReusedPartitions != 4 || c2.Sum != 5 {
		t.Fatal("verified second resume duplicated counts")
	}
}

func corruptArtifact(t *testing.T, path string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var data [1]byte
	if _, err = f.ReadAt(data[:], 0); err != nil {
		t.Fatal(err)
	}
	data[0] ^= 1
	if _, err = f.WriteAt(data[:], 0); err != nil {
		t.Fatal(err)
	}
	if err = f.Sync(); err != nil {
		t.Fatal(err)
	}
}

func TestProductionDamagedArtifactsSidecarsAndChangedKeys(t *testing.T) {
	f := newProductionFixture(t, 0)
	opts := f.edgeOptions(t, 4)
	edges, err := ProjectEdges(context.Background(), f.paths, f.index, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	corruptArtifact(t, edges.Parts[0].Artifact.Path)
	repaired, err := ProjectEdges(context.Background(), f.paths, f.index, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.ReusedParts != 1 || repaired.Parts[0].Artifact.SHA256 != edges.Parts[0].Artifact.SHA256 {
		t.Fatal("edge corruption not repaired")
	}
	if err = os.WriteFile(edges.Parts[1].Artifact.Path+".json", []byte("truncated"), 0600); err != nil {
		t.Fatal(err)
	}
	repaired, err = ProjectEdges(context.Background(), f.paths, f.index, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.ReusedParts != 1 {
		t.Fatal("damaged edge sidecar was trusted")
	}
	co := countOptions(t)
	counts, err := ReduceCounts(context.Background(), repaired, f.index.Len(), co, nil)
	if err != nil {
		t.Fatal(err)
	}
	corruptArtifact(t, filepath.Join(co.Directory, "target-000.counts"))
	counts, err = ReduceCounts(context.Background(), repaired, f.index.Len(), co, nil)
	if err != nil {
		t.Fatal(err)
	}
	if counts.ReusedPartitions != 3 || counts.Sum != 5 {
		t.Fatal("counter corruption not rebuilt")
	}
	if err = os.WriteFile(filepath.Join(co.Directory, "target-001.counts.json"), []byte("truncated"), 0600); err != nil {
		t.Fatal(err)
	}
	counts, err = ReduceCounts(context.Background(), repaired, f.index.Len(), co, nil)
	if err != nil {
		t.Fatal(err)
	}
	if counts.ReusedPartitions != 3 || counts.Sum != 5 {
		t.Fatal("counter sidecar corruption not rebuilt")
	}
	opts.Key = "edge-scope-v2"
	edges2, err := ProjectEdges(context.Background(), f.paths, f.index, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if edges2.ReusedParts != 0 {
		t.Fatal("changed edge key reused old source parts")
	}
	co.Key = "count-scope-v2"
	counts, err = ReduceCounts(context.Background(), edges2, f.index.Len(), co, nil)
	if err != nil {
		t.Fatal(err)
	}
	if counts.ReusedPartitions != 0 || counts.Sum != 5 {
		t.Fatal("changed counter key reused old partitions")
	}
}

func TestProductionCounterMemoryBudgetAndSourceBufferBudget(t *testing.T) {
	f := newProductionFixture(t, 0)
	opts := f.edgeOptions(t, 4)
	opts.MaxPartBufferBytes = 16
	if _, err := ProjectEdges(context.Background(), f.paths, f.index, opts, nil); err == nil {
		t.Fatal("source buffer budget ignored")
	}
	opts.MaxPartBufferBytes = 1 << 20
	edges, err := ProjectEdges(context.Background(), f.paths, f.index, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	co := countOptions(t)
	co.MaxBytes = 1
	if _, err := ReduceCounts(context.Background(), edges, f.index.Len(), co, nil); err == nil {
		t.Fatal("counter memory budget ignored")
	}
}

func TestProductionCounterCheckpointBindsActualEdgeData(t *testing.T) {
	f := newProductionFixture(t, 0)
	opts := f.edgeOptions(t, 4)
	edges, err := ProjectEdges(context.Background(), f.paths, f.index, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	co := countOptions(t)
	before, err := ReduceCounts(context.Background(), edges, f.index.Len(), co, nil)
	if err != nil {
		t.Fatal(err)
	}
	if before.Sum != 5 {
		t.Fatal("unexpected baseline")
	}
	data, err := os.ReadFile(edges.Parts[0].Artifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	// Change target W10 ordinal 0 to W60 ordinal 4 in the same bucket. The
	// source/graph keys, byte size and all partition sums remain identical.
	if binary.LittleEndian.Uint64(data[:8]) != 0 {
		t.Fatal("unexpected baseline bucket order")
	}
	binary.LittleEndian.PutUint64(data[:8], uint64(4)<<8)
	edges.Parts[0].Artifact, err = checkpoint.Write(edges.Parts[0].Artifact.Path, func(w io.Writer) error { _, e := w.Write(data); return e })
	if err != nil {
		t.Fatal(err)
	}
	after, err := ReduceCounts(context.Background(), edges, f.index.Len(), co, nil)
	if err != nil {
		t.Fatal(err)
	}
	if after.ReusedPartitions != 0 {
		t.Fatal("changed edge content incorrectly reused old target checkpoints")
	}
	checkCounts(t, f, after, map[uint64][]uint64{10: {0, 1, 0}, 20: {0, 1, 0}, 30: {1, 0, 0}, 60: {1, 0, 0}, 80: {1, 0, 0}})
}
