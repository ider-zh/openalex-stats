// Package scan streams projected OpenAlex Parquet columns into shared work values.
package scan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/model"
)

type Options struct {
	ReadReferences bool
	ReadDimensions bool
	// MaxRows caps yielded rows across all files. Zero means no cap.
	MaxRows int64
	// ReaderWorkers reads files concurrently, but yield remains serial in input
	// file order. Bounded channels apply backpressure to ahead-of-order readers.
	ReaderWorkers int
	// OnQuality, when non-nil, runs immediately before yield on the same
	// goroutine. It allows normalized caches to preserve nullable/error flags.
	OnQuality func(RowQuality) `json:"-"`
}

type RowQuality struct {
	InvalidReferenceIDs int64
	InvalidConcepts     int64
	MissingYear         bool
	NullXPAC            bool
}

type Stats struct {
	FilesOpened         int64 `json:"files_opened"`
	RowsRead            int64 `json:"rows_read"`
	Rows                int64 `json:"rows"`
	InvalidReferenceIDs int64 `json:"invalid_reference_ids"`
	InvalidConcepts     int64 `json:"invalid_concepts"`
	MissingYears        int64 `json:"missing_years"`
	NullXPACFlags       int64 `json:"null_xpac_flags"`
}

// ParseWorkID accepts canonical OpenAlex URLs or their W-prefixed short form.
// Bare numeric IDs, W0, signs, whitespace and noncanonical prefixes are rejected.
func ParseWorkID(s string) (uint64, error) {
	if strings.HasPrefix(s, "https://openalex.org/") {
		s = s[len("https://openalex.org/"):]
	}
	if len(s) < 2 || s[0] != 'W' || s[1] == '0' {
		return 0, fmt.Errorf("invalid OpenAlex work ID %q", s)
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("invalid OpenAlex work ID %q", s)
		}
	}
	id, err := strconv.ParseUint(s[1:], 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("invalid OpenAlex work ID %q", s)
	}
	return id, nil
}

// Each concrete projection omits unwanted columns entirely, including nested
// sibling fields such as authorship names and abstracts. Do not embed these
// structs: parquet-go v0.32.0's embedded-field lookup dereferences nullable
// leaves before reconstruction, losing null information.
type baseProjection struct {
	ID          *string `parquet:"id"`
	Year        *int32  `parquet:"publication_year"`
	Type        *string `parquet:"type"`
	IsXPAC      *bool   `parquet:"is_xpac"`
	IsParatext  *bool   `parquet:"is_paratext"`
	IsRetracted *bool   `parquet:"is_retracted"`
}

type hierarchyID struct {
	ID *string `parquet:"id"`
}

type topicProjection struct {
	Field    *hierarchyID `parquet:"field"`
	Subfield *hierarchyID `parquet:"subfield"`
}

type authorshipProjection struct {
	Countries []*string `parquet:"countries,list"`
}

type conceptProjection struct {
	ID    *string  `parquet:"id"`
	Level *int32   `parquet:"level"`
	Score *float64 `parquet:"score"`
}

type dimensionProjection struct {
	Authorships []*authorshipProjection `parquet:"authorships,list"`
	Topic       *topicProjection        `parquet:"primary_topic"`
	Concepts    []*conceptProjection    `parquet:"concepts,list"`
}

type referenceProjection struct {
	References []*string `parquet:"referenced_works,list"`
}

type baseRow baseProjection
type dimensionRow struct {
	ID          *string                 `parquet:"id"`
	Year        *int32                  `parquet:"publication_year"`
	Type        *string                 `parquet:"type"`
	IsXPAC      *bool                   `parquet:"is_xpac"`
	IsParatext  *bool                   `parquet:"is_paratext"`
	IsRetracted *bool                   `parquet:"is_retracted"`
	Authorships []*authorshipProjection `parquet:"authorships,list"`
	Topic       *topicProjection        `parquet:"primary_topic"`
	Concepts    []*conceptProjection    `parquet:"concepts,list"`
}
type referenceRow struct {
	ID          *string   `parquet:"id"`
	Year        *int32    `parquet:"publication_year"`
	Type        *string   `parquet:"type"`
	IsXPAC      *bool     `parquet:"is_xpac"`
	IsParatext  *bool     `parquet:"is_paratext"`
	IsRetracted *bool     `parquet:"is_retracted"`
	References  []*string `parquet:"referenced_works,list"`
}
type completeRow struct {
	ID          *string                 `parquet:"id"`
	Year        *int32                  `parquet:"publication_year"`
	Type        *string                 `parquet:"type"`
	IsXPAC      *bool                   `parquet:"is_xpac"`
	IsParatext  *bool                   `parquet:"is_paratext"`
	IsRetracted *bool                   `parquet:"is_retracted"`
	Authorships []*authorshipProjection `parquet:"authorships,list"`
	Topic       *topicProjection        `parquet:"primary_topic"`
	Concepts    []*conceptProjection    `parquet:"concepts,list"`
	References  []*string               `parquet:"referenced_works,list"`
}

func (r dimensionRow) base() baseProjection {
	return baseProjection{r.ID, r.Year, r.Type, r.IsXPAC, r.IsParatext, r.IsRetracted}
}
func (r referenceRow) base() baseProjection {
	return baseProjection{r.ID, r.Year, r.Type, r.IsXPAC, r.IsParatext, r.IsRetracted}
}
func (r completeRow) base() baseProjection {
	return baseProjection{r.ID, r.Year, r.Type, r.IsXPAC, r.IsParatext, r.IsRetracted}
}

type quality struct {
	invalidReferences, invalidConcepts, missingYears, nullXPAC int64
}

type envelope struct {
	work model.Work
	quality
}

type fileResult struct {
	rows chan envelope
	read int64
	open bool
	err  error
}

// Parquet reads paths in the given order. Callback errors cancel outstanding
// readers and are returned unchanged (errors.Is remains usable). MaxRows is a
// successful early stop. RowsRead may exceed Rows because of bounded read-ahead;
// quality counters refer only to yielded rows. Null XPAC flags become false in
// model.Work but are explicitly counted for callers to enforce their policy.
func Parquet(ctx context.Context, paths []string, opts Options, yield func(model.Work) error) (stats Stats, err error) {
	if yield == nil {
		return stats, errors.New("scan: nil yield callback")
	}
	if opts.MaxRows < 0 || opts.ReaderWorkers < 0 {
		return stats, errors.New("scan: row limit and reader workers cannot be negative")
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
	results := make([]fileResult, len(paths))
	for i := range results {
		results[i].rows = make(chan envelope, 16)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for index := range jobs {
				r := &results[index]
				r.err = readFile(ctx, paths[index], opts, r, func(item envelope) error {
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
	var dispatch sync.WaitGroup
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
						return stats, fmt.Errorf("scan %q: %w", paths[i], results[i].err)
					}
					goto nextFile
				}
				if err := ctx.Err(); err != nil {
					return stats, err
				}
				if opts.OnQuality != nil {
					opts.OnQuality(RowQuality{item.invalidReferences, item.invalidConcepts, item.missingYears != 0, item.nullXPAC != 0})
				}
				if err := yield(item.work); err != nil {
					return stats, err
				}
				stats.Rows++
				stats.InvalidReferenceIDs += item.invalidReferences
				stats.InvalidConcepts += item.invalidConcepts
				stats.MissingYears += item.missingYears
				stats.NullXPACFlags += item.nullXPAC
				if opts.MaxRows > 0 && stats.Rows >= opts.MaxRows {
					return stats, nil
				}
			}
		}
	nextFile:
	}
	return stats, nil
}

func readFile(ctx context.Context, path string, opts Options, result *fileResult, yield func(envelope) error) (err error) {
	// parquet-go constructors panic on incompatible schemas. Convert that library
	// contract into a contextual error so corrupt input cannot crash a long run.
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("parquet schema/decoder failure: %v", v)
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
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
	for _, name := range []string{"id", "publication_year", "type", "is_xpac", "is_paratext", "is_retracted"} {
		if _, ok := pf.Schema().Lookup(name); !ok {
			return fmt.Errorf("required projection column %q is absent", name)
		}
	}
	if opts.ReadReferences {
		found := false
		for _, field := range pf.Schema().Fields() {
			found = found || field.Name() == "referenced_works"
		}
		if !found {
			return errors.New("requested referenced_works column is absent")
		}
	}
	if opts.ReadDimensions {
		for _, name := range []string{"authorships", "primary_topic", "concepts"} {
			found := false
			for _, field := range pf.Schema().Fields() {
				found = found || field.Name() == name
			}
			if !found {
				return fmt.Errorf("requested dimension column %q is absent", name)
			}
		}
	}
	switch {
	case opts.ReadDimensions && opts.ReadReferences:
		return readProjected(ctx, pf, result, func(r completeRow) (envelope, error) {
			return convert(r.base(), &dimensionProjection{r.Authorships, r.Topic, r.Concepts}, &referenceProjection{r.References})
		}, yield)
	case opts.ReadDimensions:
		return readProjected(ctx, pf, result, func(r dimensionRow) (envelope, error) {
			return convert(r.base(), &dimensionProjection{r.Authorships, r.Topic, r.Concepts}, nil)
		}, yield)
	case opts.ReadReferences:
		return readProjected(ctx, pf, result, func(r referenceRow) (envelope, error) {
			return convert(r.base(), nil, &referenceProjection{r.References})
		}, yield)
	default:
		return readProjected(ctx, pf, result, func(r baseRow) (envelope, error) { return convert(baseProjection(r), nil, nil) }, yield)
	}
}

func readProjected[T any](ctx context.Context, f *parquet.File, result *fileResult, convertRow func(T) (envelope, error), yield func(envelope) error) error {
	r := parquet.NewGenericReader[T](f)
	defer r.Close()
	batch := make([]T, 64)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := r.Read(batch)
		result.read += int64(n)
		for i := 0; i < n; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			item, e := convertRow(batch[i])
			if e != nil {
				return fmt.Errorf("row %d: %w", result.read-int64(n)+int64(i)+1, e)
			}
			if e := yield(item); e != nil {
				return e
			}
		}
		clear(batch)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
}

func convert(base baseProjection, dims *dimensionProjection, refs *referenceProjection) (item envelope, err error) {
	if base.ID == nil {
		return item, errors.New("null work ID")
	}
	item.work.ID, err = ParseWorkID(*base.ID)
	if err != nil {
		return item, err
	}
	if base.Year != nil {
		item.work.Year = int(*base.Year)
	} else {
		item.missingYears++
	}
	if base.Type != nil {
		item.work.Type = *base.Type
	}
	if base.IsXPAC != nil {
		item.work.IsXPAC = *base.IsXPAC
	} else {
		item.nullXPAC++
	}
	item.work.IsParatext = base.IsParatext != nil && *base.IsParatext
	item.work.IsRetracted = base.IsRetracted != nil && *base.IsRetracted
	if dims != nil {
		for _, authorship := range dims.Authorships {
			if authorship == nil {
				continue
			}
			for _, country := range authorship.Countries {
				if country != nil && *country != "" {
					item.work.Countries = append(item.work.Countries, *country)
				}
			}
		}
		sort.Strings(item.work.Countries)
		if len(item.work.Countries) > 1 {
			n := 1
			for _, country := range item.work.Countries[1:] {
				if country != item.work.Countries[n-1] {
					item.work.Countries[n] = country
					n++
				}
			}
			item.work.Countries = item.work.Countries[:n]
		}
		if dims.Topic != nil {
			if dims.Topic.Field != nil && dims.Topic.Field.ID != nil {
				item.work.FieldID = *dims.Topic.Field.ID
			}
			if dims.Topic.Subfield != nil && dims.Topic.Subfield.ID != nil {
				item.work.SubfieldID = *dims.Topic.Subfield.ID
			}
		}
		for _, c := range dims.Concepts {
			if c == nil || c.Level == nil || c.ID == nil || *c.ID == "" || c.Score == nil || math.IsNaN(*c.Score) || math.IsInf(*c.Score, 0) {
				item.invalidConcepts++
				continue
			}
			if *c.Level == 0 || *c.Level == 1 {
				item.work.Concepts = append(item.work.Concepts, model.Concept{ID: *c.ID, Level: int(*c.Level), Score: *c.Score})
			}
		}
	}
	if refs != nil {
		for _, ref := range refs.References {
			if ref == nil {
				item.invalidReferences++
				continue
			}
			id, e := ParseWorkID(*ref)
			if e != nil {
				item.invalidReferences++
				continue
			}
			item.work.References = append(item.work.References, id)
		}
	}
	return item, nil
}
