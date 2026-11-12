package production

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/cache"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/model"
	"openalexstats/internal/policy"
	"openalexstats/internal/scan"
	"openalexstats/internal/store"
)

type dimensionSource struct {
	Index     int
	Path, Key string
	Info      os.FileInfo
	Upper     store.Shape
	RawRows   int64
}
type dimensionPart struct {
	Version   int                 `json:"version"`
	Key       string              `json:"key"`
	Source    string              `json:"source"`
	Shape     store.Shape         `json:"shape"`
	RawIDs    uint64              `json:"raw_ids"`
	Columns   checkpoint.Artifact `json:"columns"`
	AllIDs    checkpoint.Artifact `json:"all_ids"`
	Inventory Inventory           `json:"inventory"`
	Scan      scan.Stats          `json:"scan"`
	Checksum  string              `json:"checksum"`
}

func inspectDimensionSources(ctx context.Context, paths []string, key string) ([]dimensionSource, string, int64, error) {
	result := make([]dimensionSource, len(paths))
	keys := make([]string, len(paths))
	var total int64
	for i, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, "", 0, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, "", 0, err
		}
		upper, rows, err := dimensionSourceShape(path)
		if err != nil {
			return nil, "", 0, err
		}
		partKey, err := checkpoint.Key(struct {
			Version, Q, Path string
			Index            int
			Size, Modified   int64
		}{"dimension-part-v1", key, path, i, info.Size(), info.ModTime().UnixNano()})
		if err != nil {
			return nil, "", 0, err
		}
		if rows > int64(^uint64(0)>>1)-total {
			return nil, "", 0, fmt.Errorf("source row sum overflow")
		}
		total += rows
		result[i] = dimensionSource{i, path, partKey, info, upper, rows}
		keys[i] = partKey
	}
	identity, err := checkpoint.Key(keys)
	return result, identity, total, err
}

func dimensionSourceShape(path string) (store.Shape, int64, error) {
	var shape store.Shape
	f, err := os.Open(path)
	if err != nil {
		return shape, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return shape, 0, err
	}
	pf, err := parquet.OpenFile(f, info.Size(), parquet.SkipPageIndex(true), parquet.SkipBloomFilters(true))
	if err != nil {
		return shape, 0, err
	}
	rows := pf.NumRows()
	if rows < 0 || uint64(rows) > uint64(^uint(0)>>1) {
		return shape, 0, fmt.Errorf("dimension source row count exceeds capacity")
	}
	shape.Rows = int(rows)
	var country, concept uint64
	for _, group := range pf.Metadata().RowGroups {
		for _, column := range group.Columns {
			p := column.MetaData.PathInSchema
			if len(p) == 0 {
				continue
			}
			v := column.MetaData.NumValues
			if v < 0 {
				return shape, 0, fmt.Errorf("negative dimension value count")
			}
			switch p[0] {
			case "countries":
				country += uint64(v)
			case "concept_ids0", "concept_ids1":
				concept += uint64(v)
			}
		}
	}
	if country > uint64(^uint(0)>>1) || concept > uint64(^uint(0)>>1) {
		return shape, 0, fmt.Errorf("dimension source lists exceed capacity")
	}
	shape.Countries = int(country)
	shape.Concepts = int(concept)
	return shape, rows, nil
}

func prepareDimensionParts(ctx context.Context, sources []dimensionSource, directory string, scope policy.Scope, maxPartBytes uint64, workers int, progress Progress) ([]dimensionPart, int, error) {
	parts := make([]dimensionPart, len(sources))
	reused := make([]bool, len(sources))
	if err := os.MkdirAll(directory, 0755); err != nil {
		return parts, 0, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan dimensionSource)
	var wg sync.WaitGroup
	var firstErr error
	var once sync.Once
	for range workers {
		wg.Go(func() {
			for source := range jobs {
				if ctx.Err() != nil {
					return
				}
				path := filepath.Join(directory, fmt.Sprintf("part-%05d.columns", source.Index))
				idsPath := filepath.Join(directory, fmt.Sprintf("part-%05d.ids", source.Index))
				metaPath := filepath.Join(directory, fmt.Sprintf("part-%05d.json", source.Index))
				var part dimensionPart
				if checkpoint.ReadJSON(metaPath, &part) == nil && validDimensionPart(part, source, path, idsPath) && part.Columns.Verify() == nil && part.AllIDs.Verify() == nil {
					parts[source.Index] = part
					reused[source.Index] = true
					progress("dimension_part_reused", map[string]any{"part": source.Index, "rows": part.RawIDs, "selected": part.Shape.Rows})
					continue
				}
				part, err := buildDimensionPart(ctx, source, path, idsPath, scope, maxPartBytes)
				if err == nil {
					part.Checksum, err = dimensionPartChecksum(part)
				}
				if err == nil {
					err = ctx.Err()
				}
				if err == nil {
					err = checkpoint.JSON(metaPath, part)
				}
				if err != nil {
					once.Do(func() { firstErr = fmt.Errorf("dimension part %d: %w", source.Index, err); cancel() })
					return
				}
				parts[source.Index] = part
				progress("dimension_part_written", map[string]any{"part": source.Index, "rows": part.RawIDs, "selected": part.Shape.Rows, "bytes": part.Columns.Bytes + part.AllIDs.Bytes})
			}
		})
	}
dispatch:
	for _, source := range sources {
		select {
		case jobs <- source:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return parts, 0, firstErr
	}
	if err := ctx.Err(); err != nil {
		return parts, 0, err
	}
	n := 0
	for _, yes := range reused {
		if yes {
			n++
		}
	}
	return parts, n, nil
}

func dimensionPartChecksum(part dimensionPart) (string, error) {
	part.Checksum = ""
	return checkpoint.Key(part)
}
func validDimensionPart(part dimensionPart, s dimensionSource, path, idsPath string) bool {
	actual, err := dimensionPartChecksum(part)
	return err == nil && part.Version == 1 && part.Key == s.Key && part.Source == s.Path && part.Columns.Path == path && part.AllIDs.Path == idsPath && part.RawIDs == uint64(s.RawRows) && part.Shape.Rows >= 0 && uint64(part.Shape.Rows) == part.Inventory.Selected && part.Inventory.Rows == part.RawIDs && part.Scan.Rows == s.RawRows && part.Checksum != "" && part.Checksum == actual
}

func buildDimensionPart(ctx context.Context, source dimensionSource, path, idsPath string, scope policy.Scope, maxBytes uint64) (dimensionPart, error) {
	part := dimensionPart{Version: 1, Key: source.Key, Source: source.Path, Inventory: newInventory()}
	estimated, err := source.Upper.EstimatedBytes()
	if err != nil {
		return part, err
	}
	idBytes := uint64(source.RawRows) * 8
	if estimated > maxBytes || idBytes > maxBytes-estimated {
		return part, fmt.Errorf("source upper-bound arrays require %d bytes, per-worker budget %d; lower dimension reader count", estimated+idBytes, maxBytes)
	}
	columns, err := store.NewReserved(source.Upper)
	if err != nil {
		return part, err
	}
	ids := make([]uint64, 0, int(source.RawRows))
	blacklist := make(map[string]bool, len(scope.Blacklist))
	for _, typ := range scope.Blacklist {
		blacklist[typ] = true
	}
	var quality scan.RowQuality
	stats, err := cache.Scan(ctx, []string{source.Path}, scan.Options{ReadDimensions: true, ReaderWorkers: 1, OnQuality: func(q scan.RowQuality) { quality = q }}, func(w model.Work) error {
		ids = append(ids, w.ID)
		if err := observeDimension(&part.Inventory, columns, w, quality, scope, blacklist); err != nil {
			return err
		}
		if part.Inventory.Rows%4096 == 0 && columns.EstimatedBytes()+uint64(cap(ids))*8 > maxBytes {
			return fmt.Errorf("per-file dimensions exceed worker budget")
		}
		return nil
	})
	if err != nil {
		return part, err
	}
	part.Scan = stats
	part.RawIDs = uint64(len(ids))
	part.Shape = columns.Shape()
	if int64(len(ids)) != source.RawRows || stats.Rows != source.RawRows || part.Inventory.Rows != part.RawIDs || part.Inventory.Selected != uint64(columns.Len()) {
		return part, fmt.Errorf("per-file dimension row conservation failed")
	}
	if columns.EstimatedBytes()+uint64(cap(ids))*8 > maxBytes {
		return part, fmt.Errorf("per-file dimension memory budget exceeded")
	}
	after, err := os.Stat(source.Path)
	if err != nil {
		return part, err
	}
	if !os.SameFile(source.Info, after) || source.Info.Size() != after.Size() || !source.Info.ModTime().Equal(after.ModTime()) {
		return part, fmt.Errorf("normalized source changed during dimension scan")
	}
	part.Columns, err = checkpoint.Write(path, func(w io.Writer) error { _, e := columns.WriteTo(dimensionContextWriter{ctx, w}); return e })
	if err != nil {
		return part, err
	}
	part.AllIDs, err = checkpoint.Write(idsPath, func(w io.Writer) error { return writeIDs(dimensionContextWriter{ctx, w}, ids) })
	return part, err
}

func appendDimensionPart(ctx context.Context, columns *store.Columns, ids *[]uint64, part dimensionPart, maxBytes uint64) error {
	idBytes := part.RawIDs * 8
	if idBytes > maxBytes {
		return fmt.Errorf("ID shard exceeds merge budget")
	}
	f, err := os.Open(part.Columns.Path)
	if err != nil {
		return err
	}
	local, err := store.ReadFrom(dimensionContextReader{ctx, f}, maxBytes-idBytes)
	f.Close()
	if err != nil {
		return err
	}
	if local.Shape() != part.Shape {
		return fmt.Errorf("dimension shard shape does not match its checkpoint")
	}
	localIDs, err := readIDs(part.AllIDs.Path, maxBytes-local.EstimatedBytes())
	if err != nil {
		return err
	}
	if uint64(len(localIDs)) != part.RawIDs {
		return fmt.Errorf("dimension raw-ID shard count mismatch")
	}
	if err := columns.AppendColumns(local); err != nil {
		return err
	}
	*ids = append(*ids, localIDs...)
	return nil
}

func newInventory() Inventory {
	return Inventory{TypeCounts: map[string][3]uint64{}, ExcludedReasons: map[string]uint64{}}
}
func observeDimension(inventory *Inventory, columns *store.Columns, w model.Work, quality scan.RowQuality, scope policy.Scope, blacklist map[string]bool) error {
	inventory.Rows++
	group := 0
	coverage := &inventory.Core
	if w.IsXPAC {
		group = 1
		coverage = &inventory.XPAC
	}
	if quality.NullXPAC {
		group = 2
		coverage = &inventory.NullXPAC
	}
	coverage.Works++
	if w.Year <= 0 {
		coverage.MissingYear++
	}
	if len(w.Countries) == 0 {
		coverage.MissingCountry++
	}
	if w.FieldID == "" {
		coverage.MissingField++
	}
	if w.SubfieldID == "" {
		coverage.MissingSubfield++
	}
	var levels [2]bool
	for _, concept := range w.Concepts {
		if concept.Level >= 0 && concept.Level <= 1 && concept.Score >= .3 {
			levels[concept.Level] = true
		}
	}
	if !levels[0] {
		coverage.MissingConcept0++
	}
	if !levels[1] {
		coverage.MissingConcept1++
	}
	counts := inventory.TypeCounts[w.Type]
	counts[group]++
	inventory.TypeCounts[w.Type] = counts
	reason := ""
	switch {
	case blacklist[w.Type]:
		reason = "type:" + w.Type
	case w.IsParatext:
		reason = "is_paratext"
	case scope.Corpus == "core" && quality.NullXPAC:
		reason = "xpac_unknown"
	case scope.Corpus == "core" && w.IsXPAC:
		reason = "xpac"
	case scope.CorpusYearStart > 0 && w.Year < scope.CorpusYearStart:
		reason = "corpus_year"
	}
	if reason != "" {
		inventory.ExcludedReasons[reason]++
		return nil
	}
	if !scope.Includes(w) {
		return fmt.Errorf("scope/inventory disagreement W%d", w.ID)
	}
	inventory.Selected++
	if w.IsRetracted {
		inventory.SelectedRetracted++
	}
	return columns.Append(w)
}

func addCoverage(a *GroupCoverage, b GroupCoverage) {
	a.Works += b.Works
	a.MissingYear += b.MissingYear
	a.MissingCountry += b.MissingCountry
	a.MissingField += b.MissingField
	a.MissingSubfield += b.MissingSubfield
	a.MissingConcept0 += b.MissingConcept0
	a.MissingConcept1 += b.MissingConcept1
}
func addInventory(a *Inventory, b Inventory) {
	a.Rows += b.Rows
	addCoverage(&a.Core, b.Core)
	addCoverage(&a.XPAC, b.XPAC)
	addCoverage(&a.NullXPAC, b.NullXPAC)
	a.Selected += b.Selected
	a.SelectedRetracted += b.SelectedRetracted
	for typ, counts := range b.TypeCounts {
		old := a.TypeCounts[typ]
		for i, n := range counts {
			old[i] += n
		}
		a.TypeCounts[typ] = old
	}
	for reason, n := range b.ExcludedReasons {
		a.ExcludedReasons[reason] += n
	}
}
func addScanStats(a *scan.Stats, b scan.Stats) {
	a.FilesOpened += b.FilesOpened
	a.RowsRead += b.RowsRead
	a.Rows += b.Rows
	a.InvalidReferenceIDs += b.InvalidReferenceIDs
	a.InvalidConcepts += b.InvalidConcepts
	a.MissingYears += b.MissingYears
	a.NullXPACFlags += b.NullXPACFlags
}
func addShape(a *store.Shape, b store.Shape) error {
	maxInt := int(^uint(0) >> 1)
	if b.Rows < 0 || b.Countries < 0 || b.Concepts < 0 || a.Rows > maxInt-b.Rows || a.Countries > maxInt-b.Countries || a.Concepts > maxInt-b.Concepts {
		return fmt.Errorf("dimension shape sum overflow")
	}
	a.Rows += b.Rows
	a.Countries += b.Countries
	a.Concepts += b.Concepts
	return nil
}
