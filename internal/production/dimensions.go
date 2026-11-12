package production

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"openalexstats/internal/checkpoint"
	"openalexstats/internal/policy"
	"openalexstats/internal/scan"
	"openalexstats/internal/store"
)

type GroupCoverage struct {
	Works           uint64 `json:"works"`
	MissingYear     uint64 `json:"missing_year"`
	MissingCountry  uint64 `json:"missing_country"`
	MissingField    uint64 `json:"missing_field"`
	MissingSubfield uint64 `json:"missing_subfield"`
	MissingConcept0 uint64 `json:"missing_concept_0"`
	MissingConcept1 uint64 `json:"missing_concept_1"`
}
type Inventory struct {
	Rows                 uint64 `json:"rows"`
	Core, XPAC, NullXPAC GroupCoverage
	TypeCounts           map[string][3]uint64 `json:"type_counts_core_xpac_unknown"`
	ExcludedReasons      map[string]uint64    `json:"excluded_reasons"`
	Selected             uint64               `json:"selected"`
	SelectedRetracted    uint64               `json:"selected_retracted"`
}
type dimensionCheckpoint struct {
	Version   int                 `json:"version"`
	Key       string              `json:"key"`
	InputsKey string              `json:"inputs_key,omitempty"`
	Shape     store.Shape         `json:"shape,omitempty"`
	Columns   checkpoint.Artifact `json:"columns"`
	AllIDs    checkpoint.Artifact `json:"all_ids"`
	Inventory Inventory           `json:"inventory"`
	Scan      scan.Stats          `json:"scan"`
}
type dimensionResult struct {
	Columns    *store.Columns
	AllIDs     []uint64
	Checkpoint dimensionCheckpoint
	Reused     bool
}

func dimensions(ctx context.Context, paths []string, directory, key string, scope policy.Scope, maxBytes uint64, readers int, progress Progress) (dimensionResult, error) {
	var out dimensionResult
	if len(paths) == 0 || readers < 1 || maxBytes < 1024 {
		return out, fmt.Errorf("invalid dimension inputs or budget")
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if progress == nil {
		progress = func(string, any) {}
	}
	originalProgress := progress
	var progressMu sync.Mutex
	progress = func(stage string, detail any) {
		progressMu.Lock()
		defer progressMu.Unlock()
		originalProgress(stage, detail)
	}
	sources, inputsKey, totalRows, err := inspectDimensionSources(ctx, paths, key)
	if err != nil {
		return out, err
	}
	budget := maxBytes * 45 / 100
	path := filepath.Join(directory, "dimensions.bin")
	idsPath := filepath.Join(directory, "all-ids.bin")
	metaPath := filepath.Join(directory, "dimensions.json")
	var prior dimensionCheckpoint
	// Legacy v1 whole-stage checkpoints are accepted on their Q key, which the
	// run coordinator binds to verified source hashes. New ones also bind inputs.
	if checkpoint.ReadJSON(metaPath, &prior) == nil && prior.Version == 1 && prior.Key == key && (prior.InputsKey == "" || prior.InputsKey == inputsKey) && prior.Columns.Path == path && prior.AllIDs.Path == idsPath && prior.Columns.Verify() == nil && prior.AllIDs.Verify() == nil {
		f, e := os.Open(path)
		if e != nil {
			return out, e
		}
		columns, e := store.ReadFrom(dimensionContextReader{ctx, f}, budget)
		f.Close()
		if e != nil {
			return out, e
		}
		if columns.EstimatedBytes() > budget {
			return out, fmt.Errorf("reused dimensions exceed budget")
		}
		ids, e := readIDs(idsPath, budget-columns.EstimatedBytes())
		if e != nil {
			return out, e
		}
		if uint64(columns.Len()) != prior.Inventory.Selected || uint64(len(ids)) != prior.Inventory.Rows || int64(len(ids)) != totalRows {
			return out, fmt.Errorf("reused dimension row conservation failed")
		}
		progress("dimensions_reused", map[string]any{"selected": columns.Len(), "all": len(ids)})
		return dimensionResult{Columns: columns, AllIDs: ids, Checkpoint: prior, Reused: true}, nil
	}
	workers := min(readers, len(sources))
	partBudget := maxBytes * 20 / 100 / uint64(workers)
	parts, reused, err := prepareDimensionParts(ctx, sources, filepath.Join(directory, "dimension-parts"), scope, partBudget, workers, progress)
	if err != nil {
		return out, err
	}
	var shape store.Shape
	var rawCount uint64
	var largestPart uint64
	inventory := newInventory()
	var stats scan.Stats
	for _, part := range parts {
		if err := addShape(&shape, part.Shape); err != nil {
			return out, err
		}
		rawCount += part.RawIDs
		n, e := part.Shape.EstimatedBytes()
		if e != nil {
			return out, e
		}
		n += part.RawIDs * 8
		if n > largestPart {
			largestPart = n
		}
		addInventory(&inventory, part.Inventory)
		addScanStats(&stats, part.Scan)
	}
	estimated, err := shape.EstimatedBytes()
	if err != nil {
		return out, err
	}
	if rawCount > uint64(^uint(0)>>1) || rawCount != uint64(totalRows) || inventory.Rows != rawCount || inventory.Selected != uint64(shape.Rows) || uint64(stats.Rows) != rawCount {
		return out, fmt.Errorf("dimension part row conservation failed")
	}
	// Reserve both final vectors plus one loaded shard before allocating any
	// whole-corpus arrays. Dictionaries/decoder state use the remaining run RAM.
	if rawCount > (^uint64(0)-estimated)/8 {
		return out, fmt.Errorf("dimension size overflow")
	}
	required := estimated + rawCount*8
	if required > budget || largestPart > budget-required {
		return out, fmt.Errorf("global dimensions plus one merge shard need %d bytes; dimension budget %d", required+largestPart, budget)
	}
	columns, err := store.NewReserved(shape)
	if err != nil {
		return out, err
	}
	ids := make([]uint64, 0, int(rawCount))
	for i, part := range parts {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		if err := appendDimensionPart(ctx, columns, &ids, part, partBudget); err != nil {
			return out, fmt.Errorf("merge dimension part %d: %w", i, err)
		}
		progress("dimensions_merge", map[string]any{"part": i, "parts": len(parts), "rows": len(ids), "selected": columns.Len(), "bytes": columns.EstimatedBytes()})
	}
	if columns.Shape() != shape || uint64(len(ids)) != rawCount || columns.EstimatedBytes()+uint64(cap(ids))*8 > budget {
		return out, fmt.Errorf("merged dimension shape or memory budget mismatch")
	}
	meta := dimensionCheckpoint{Version: 1, Key: key, InputsKey: inputsKey, Shape: shape, Inventory: inventory, Scan: stats}
	meta.Columns, err = checkpoint.Write(path, func(w io.Writer) error { _, e := columns.WriteTo(dimensionContextWriter{ctx, w}); return e })
	if err != nil {
		return out, err
	}
	meta.AllIDs, err = checkpoint.Write(idsPath, func(w io.Writer) error { return writeIDs(dimensionContextWriter{ctx, w}, ids) })
	if err != nil {
		return out, err
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if err := checkpoint.JSON(metaPath, meta); err != nil {
		return out, err
	}
	progress("dimensions_complete", map[string]any{"parts": len(parts), "reused_parts": reused, "selected": columns.Len(), "all": len(ids)})
	return dimensionResult{Columns: columns, AllIDs: ids, Checkpoint: meta}, nil
}

type dimensionContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r dimensionContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

type dimensionContextWriter struct {
	ctx context.Context
	w   io.Writer
}

func (w dimensionContextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.w.Write(p)
}
func writeIDs(w io.Writer, ids []uint64) error {
	var n [8]byte
	binary.LittleEndian.PutUint64(n[:], uint64(len(ids)))
	if _, e := w.Write(n[:]); e != nil {
		return e
	}
	buf := make([]byte, 64<<10)
	for start := 0; start < len(ids); {
		take := min(len(buf)/8, len(ids)-start)
		for i, v := range ids[start : start+take] {
			binary.LittleEndian.PutUint64(buf[i*8:(i+1)*8], v)
		}
		if _, e := w.Write(buf[:take*8]); e != nil {
			return e
		}
		start += take
	}
	return nil
}
func readIDs(path string, maxBytes uint64) ([]uint64, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	var b [8]byte
	if _, e = io.ReadFull(f, b[:]); e != nil {
		return nil, e
	}
	n := binary.LittleEndian.Uint64(b[:])
	if n > maxBytes/8 || n > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("ID vector exceeds allocation limit")
	}
	ids := make([]uint64, n)
	buf := make([]byte, 64<<10)
	for start := 0; start < len(ids); {
		take := min(len(buf)/8, len(ids)-start)
		if _, e = io.ReadFull(f, buf[:take*8]); e != nil {
			return nil, e
		}
		for i := 0; i < take; i++ {
			ids[start+i] = binary.LittleEndian.Uint64(buf[i*8 : (i+1)*8])
		}
		start += take
	}
	var extra [1]byte
	if n, e := f.Read(extra[:]); n != 0 || e != io.EOF {
		return nil, fmt.Errorf("trailing ID vector data")
	}
	return ids, nil
}
