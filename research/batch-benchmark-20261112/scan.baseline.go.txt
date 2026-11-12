package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"sync"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/model"
	"openalexstats/internal/scan"
)

// normalizedRow is deliberately flat, including the two concept levels. Work
// and reference IDs are numeric, so repeat graph scans do not parse W URLs.
type normalizedRow struct {
	ID                uint64    `parquet:"id"`
	Year              int32     `parquet:"year"`
	Type              string    `parquet:"type,dict"`
	IsXPAC            bool      `parquet:"is_xpac"`
	IsParatext        bool      `parquet:"is_paratext"`
	IsRetracted       bool      `parquet:"is_retracted"`
	Countries         []string  `parquet:"countries,list" parquet-element:",dict"`
	FieldID           string    `parquet:"field_id,dict"`
	SubfieldID        string    `parquet:"subfield_id,dict"`
	ConceptIDs0       []string  `parquet:"concept_ids0,list" parquet-element:",dict"`
	ConceptScores0    []float64 `parquet:"concept_scores0,list"`
	ConceptIDs1       []string  `parquet:"concept_ids1,list" parquet-element:",dict"`
	ConceptScores1    []float64 `parquet:"concept_scores1,list"`
	References        []uint64  `parquet:"references,list"`
	InvalidReferences int64     `parquet:"quality_invalid_references"`
	InvalidConcepts   int64     `parquet:"quality_invalid_concepts"`
	MissingYear       bool      `parquet:"quality_missing_year"`
	NullXPAC          bool      `parquet:"quality_null_xpac"`
}

func normalize(work model.Work, q scan.RowQuality) normalizedRow {
	r := normalizedRow{ID: work.ID, Year: int32(work.Year), Type: work.Type, IsXPAC: work.IsXPAC, IsParatext: work.IsParatext, IsRetracted: work.IsRetracted, Countries: work.Countries, FieldID: work.FieldID, SubfieldID: work.SubfieldID, References: work.References, InvalidReferences: q.InvalidReferenceIDs, InvalidConcepts: q.InvalidConcepts, MissingYear: q.MissingYear, NullXPAC: q.NullXPAC}
	n0, n1 := 0, 0
	for _, c := range work.Concepts {
		if c.Level == 0 {
			n0++
		} else if c.Level == 1 {
			n1++
		}
	}
	if n0+n1 > 0 {
		ids := make([]string, n0+n1)
		scores := make([]float64, n0+n1)
		r.ConceptIDs0, r.ConceptIDs1 = ids[:n0:n0], ids[n0:]
		r.ConceptScores0, r.ConceptScores1 = scores[:n0:n0], scores[n0:]
	}
	i0, i1 := 0, 0
	for _, c := range work.Concepts {
		if c.Level == 0 {
			r.ConceptIDs0[i0], r.ConceptScores0[i0] = c.ID, c.Score
			i0++
		} else if c.Level == 1 {
			r.ConceptIDs1[i1], r.ConceptScores1[i1] = c.ID, c.Score
			i1++
		}
	}
	return r
}

func projection(opts scan.Options) *parquet.Schema {
	var replacements []parquet.SchemaOption
	omit := func(fields ...string) {
		for _, field := range fields {
			replacements = append(replacements, parquet.StructTag(reflect.StructTag(`parquet:"-"`), field))
		}
	}
	if !opts.ReadDimensions {
		omit("Countries", "FieldID", "SubfieldID", "ConceptIDs0", "ConceptScores0", "ConceptIDs1", "ConceptScores1", "InvalidConcepts")
	}
	if !opts.ReadReferences {
		omit("References", "InvalidReferences")
	}
	return parquet.SchemaOf(normalizedRow{}, replacements...)
}

type cachedItem struct {
	work    model.Work
	quality scan.RowQuality
}
type scanResult struct {
	rows chan cachedItem
	read int64
	open bool
	err  error
}

// Scan matches scan.Parquet's deterministic callback, row-limit and projection
// semantics. It verifies format/schema and reads normalized files; Prepare
// verifies complete content hashes before their paths are handed to this call.
func Scan(ctx context.Context, paths []string, opts scan.Options, yield func(model.Work) error) (stats scan.Stats, err error) {
	if yield == nil {
		return stats, errors.New("cache scan: nil callback")
	}
	if opts.MaxRows < 0 || opts.ReaderWorkers < 0 {
		return stats, errors.New("cache scan: negative options")
	}
	if err := ctx.Err(); err != nil {
		return stats, err
	}
	workers := opts.ReaderWorkers
	if workers == 0 {
		workers = 1
	}
	if workers > len(paths) {
		workers = len(paths)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]scanResult, len(paths))
	for i := range results {
		results[i].rows = make(chan cachedItem, 16)
	}
	jobs := make(chan int)
	var wg, dispatch sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for i := range jobs {
				r := &results[i]
				r.err = scanPart(ctx, paths[i], opts, r, func(item cachedItem) error {
					select {
					case r.rows <- item:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
				close(r.rows)
			}
		})
	}
	dispatch.Go(func() {
		defer close(jobs)
		for i := range paths {
			select {
			case jobs <- i:
			case <-ctx.Done():
				return
			}
		}
	})
	defer func() {
		cancel()
		dispatch.Wait()
		wg.Wait()
		for i := range results {
			stats.RowsRead += results[i].read
			if results[i].open {
				stats.FilesOpened++
			}
		}
	}()
	for i := range results {
		for {
			if err := ctx.Err(); err != nil {
				return stats, err
			}
			select {
			case <-ctx.Done():
				return stats, ctx.Err()
			case item, ok := <-results[i].rows:
				if !ok {
					if results[i].err != nil {
						return stats, fmt.Errorf("cache scan %q: %w", paths[i], results[i].err)
					}
					goto nextFile
				}
				if err := ctx.Err(); err != nil {
					return stats, err
				}
				if opts.OnQuality != nil {
					opts.OnQuality(item.quality)
				}
				if err := yield(item.work); err != nil {
					return stats, err
				}
				stats.Rows++
				stats.InvalidReferenceIDs += item.quality.InvalidReferenceIDs
				stats.InvalidConcepts += item.quality.InvalidConcepts
				if item.quality.MissingYear {
					stats.MissingYears++
				}
				if item.quality.NullXPAC {
					stats.NullXPACFlags++
				}
				if opts.MaxRows > 0 && stats.Rows >= opts.MaxRows {
					return stats, nil
				}
			}
		}
	nextFile:
	}
	return stats, nil
}

func scanPart(ctx context.Context, path string, opts scan.Options, result *scanResult, yield func(cachedItem) error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("normalized parquet decoder failure: %v", p)
		}
	}()
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	pf, err := parquet.OpenFile(f, info.Size(), parquet.SkipPageIndex(true), parquet.SkipBloomFilters(true))
	if err != nil {
		return err
	}
	result.open = true
	if format, ok := pf.Lookup("openalexstats.cache.format"); !ok || format != FormatVersion {
		return errors.New("not a supported normalized cache file")
	}
	schema := projection(opts)
	for _, column := range schema.Columns() {
		if _, ok := pf.Schema().Lookup(column...); !ok {
			return fmt.Errorf("normalized cache column %v is missing", column)
		}
	}
	r := parquet.NewGenericReader[normalizedRow](pf, schema)
	defer r.Close()
	batch := make([]normalizedRow, 128)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, e := r.Read(batch)
		result.read += int64(n)
		for i := 0; i < n; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			row := &batch[i]
			if row.ID == 0 {
				return errors.New("zero numeric work ID in normalized cache")
			}
			w := model.Work{ID: row.ID, Year: int(row.Year), Type: row.Type, IsXPAC: row.IsXPAC, IsParatext: row.IsParatext, IsRetracted: row.IsRetracted, Countries: row.Countries, FieldID: row.FieldID, SubfieldID: row.SubfieldID, References: row.References}
			if len(row.ConceptIDs0) != len(row.ConceptScores0) || len(row.ConceptIDs1) != len(row.ConceptScores1) {
				return errors.New("normalized concept IDs/scores are not aligned")
			}
			if n := len(row.ConceptIDs0) + len(row.ConceptIDs1); n > 0 {
				w.Concepts = make([]model.Concept, 0, n)
			}
			for j, id := range row.ConceptIDs0 {
				w.Concepts = append(w.Concepts, model.Concept{ID: id, Level: 0, Score: row.ConceptScores0[j]})
			}
			for j, id := range row.ConceptIDs1 {
				w.Concepts = append(w.Concepts, model.Concept{ID: id, Level: 1, Score: row.ConceptScores1[j]})
			}
			q := scan.RowQuality{InvalidReferenceIDs: row.InvalidReferences, InvalidConcepts: row.InvalidConcepts, MissingYear: row.MissingYear, NullXPAC: row.NullXPAC}
			if err := yield(cachedItem{w, q}); err != nil {
				return err
			}
		}
		clear(batch)
		if errors.Is(e, io.EOF) {
			return nil
		}
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
}
