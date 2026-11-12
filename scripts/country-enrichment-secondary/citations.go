package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"openalexstats/internal/checkpoint"
)

type secondaryCandidate struct {
	ID         uint64   `parquet:"id"`
	XPAC       bool     `parquet:"is_xpac"`
	Geo        bool     `parquet:"has_geo"`
	Dictionary bool     `parquet:"has_strict_dictionary"`
	Countries  []string `parquet:"country_candidates,list"`
	Review     bool     `parquet:"review_only"`
}
type primaryCitationReport struct {
	Corpus string `json:"corpus"`
	Rows   int    `json:"target_rows"`
	Total  struct {
		Events  uint64 `json:"events"`
		Missing uint64 `json:"baseline_missing_country_events"`
		New     uint64 `json:"newly_known_combined_events"`
	} `json:"totals"`
	Dimensions checkpoint.Artifact   `json:"dimensions"`
	Counters   []checkpoint.Artifact `json:"counters"`
}
type conditionalYear struct {
	Year       int    `json:"year"`
	Geo        uint64 `json:"typed_geo_country_name_events"`
	Dictionary uint64 `json:"exact_dictionary_events"`
	Both       uint64 `json:"overlap_events"`
	Union      uint64 `json:"additional_review_candidate_events"`
}

func (s *conditionalYear) add(v conditionalYear) {
	s.Geo += v.Geo
	s.Dictionary += v.Dictionary
	s.Both += v.Both
	s.Union += v.Union
}

type selectedTarget struct {
	ordinal int
	flags   byte
	id      uint64
}

func conditionalCitations(work, out, analysis string, workers int) error {
	primaryPath := filepath.Join(work, "citation-summary.json")
	var primary struct {
		Status    string                  `json:"status"`
		Reports   []primaryCitationReport `json:"reports"`
		Completed time.Time               `json:"completed_at"`
	}
	if e := checkpoint.ReadJSON(primaryPath, &primary); e != nil {
		return e
	}
	if primary.Status != "passed" || len(primary.Reports) != 2 {
		return fmt.Errorf("verified primary citation report required")
	}
	var inv []struct {
		Rows    uint64 `json:"secondary_rows"`
		Unique  uint64 `json:"unique_ids"`
		Overlap uint64 `json:"overlap_with_v1_newly_known"`
		Invalid uint64 `json:"invalid_secondary_rows"`
	}
	if e := checkpoint.ReadJSON(filepath.Join(analysis, "secondary-invariants.json"), &inv); e != nil {
		return e
	}
	if len(inv) != 1 || inv[0].Rows != inv[0].Unique || inv[0].Overlap != 0 || inv[0].Invalid != 0 {
		return fmt.Errorf("secondary invariant gate failed")
	}
	flags := map[uint64]byte{}
	xpac := map[uint64]bool{}
	candidatePath := filepath.Join(analysis, "secondary-patches.parquet")
	if e := each[secondaryCandidate](candidatePath, func(v secondaryCandidate) error {
		if v.ID == 0 || !v.Review || len(v.Countries) == 0 || !v.Geo && !v.Dictionary {
			return fmt.Errorf("invalid secondary review candidate")
		}
		if _, ok := flags[v.ID]; ok {
			return fmt.Errorf("duplicate secondary ID")
		}
		var b byte
		if v.Geo {
			b |= 1
		}
		if v.Dictionary {
			b |= 2
		}
		flags[v.ID] = b
		xpac[v.ID] = v.XPAC
		return nil
	}); e != nil {
		return e
	}
	if uint64(len(flags)) != inv[0].Rows {
		return fmt.Errorf("secondary candidate row mismatch")
	}
	var reports []map[string]any
	for _, p := range primary.Reports {
		if p.Corpus != "core" && p.Corpus != "all" {
			return fmt.Errorf("unexpected corpus")
		}
		if e := p.Dimensions.Verify(); e != nil {
			return e
		}
		selected, e := findTargets(p.Dimensions.Path, p.Rows, flags, xpac, p.Corpus)
		if e != nil {
			return e
		}
		if len(p.Counters) != 64 {
			return fmt.Errorf("unexpected target partition count")
		}
		years := make([]conditionalYear, 27)
		for y := range years {
			years[y].Year = 2000 + y
		}
		var counterEvidence []map[string]any
		for part, a := range p.Counters {
			if filepath.Base(a.Path) != fmt.Sprintf("target-%03d.counts", part) {
				return fmt.Errorf("counter ordering mismatch")
			}
			f, e := os.Open(a.Path)
			if e != nil {
				return e
			}
			s, e := f.Stat()
			if e != nil {
				_ = f.Close()
				return e
			}
			if s.Size() != a.Bytes || s.ModTime().After(primary.Completed) {
				_ = f.Close()
				return fmt.Errorf("counter changed after primary full SHA verification")
			}
			h := sha256.New()
			buf := make([]byte, 27*8)
			var reads int
			for _, target := range selected[part] {
				offset := int64(target.ordinal/64) * 27 * 8
				if offset+int64(len(buf)) > a.Bytes {
					_ = f.Close()
					return fmt.Errorf("counter target offset out of range")
				}
				if _, e = f.ReadAt(buf, offset); e != nil {
					_ = f.Close()
					return e
				}
				var key [8]byte
				binary.LittleEndian.PutUint64(key[:], uint64(target.ordinal))
				_, _ = h.Write(key[:])
				_, _ = h.Write(buf)
				reads++
				for y := 0; y < 27; y++ {
					v := binary.LittleEndian.Uint64(buf[y*8:])
					if v > p.Total.Events {
						_ = f.Close()
						return fmt.Errorf("counter value exceeds total events")
					}
					years[y].Union += v
					if target.flags&1 != 0 {
						years[y].Geo += v
					}
					if target.flags&2 != 0 {
						years[y].Dictionary += v
					}
					if target.flags == 3 {
						years[y].Both += v
					}
				}
			}
			_ = f.Close()
			counterEvidence = append(counterEvidence, map[string]any{"partition": part, "artifact": a, "selected_target_rows": reads, "selected_ordinal_counter_sha256": hex.EncodeToString(h.Sum(nil)), "full_artifact_sha_verification_reference": primaryPath, "modtime": s.ModTime().UTC()})
		}
		var total conditionalYear
		var targetCount int
		for _, v := range selected {
			targetCount += len(v)
		}
		for _, v := range years {
			total.add(v)
		}
		if total.Union != total.Geo+total.Dictionary-total.Both || total.Union+p.Total.New > p.Total.Missing {
			return fmt.Errorf("conditional citation coverage conservation failed")
		}
		reports = append(reports, map[string]any{"corpus": p.Corpus, "selected_unknown_targets": targetCount, "baseline_total_events": p.Total.Events, "baseline_missing_country_events": p.Total.Missing, "v1_newly_known_events": p.Total.New, "secondary_additional_review_events": total, "years": years, "baseline_country_coverage_percent": 100 * float64(p.Total.Events-p.Total.Missing) / float64(p.Total.Events), "v1_candidate_country_coverage_percent": 100 * float64(p.Total.Events-p.Total.Missing+p.Total.New) / float64(p.Total.Events), "v1_plus_v1b_conditional_coverage_percent": 100 * float64(p.Total.Events-p.Total.Missing+p.Total.New+total.Union) / float64(p.Total.Events), "dimensions": p.Dimensions, "counter_reuse_evidence": counterEvidence})
	}
	primaryArtifact, e := checkpoint.Hash(primaryPath)
	if e != nil {
		return e
	}
	candidateArtifact, e := checkpoint.Hash(candidatePath)
	if e != nil {
		return e
	}
	return checkpoint.JSON(filepath.Join(out, "conditional-citation-summary.json"), map[string]any{"status": "passed", "interpretation": "Conditional review scenario only: all v1b candidates remain unapplied. v1b targets are UNKNOWN after v1 and disjoint from v1 newly-known targets.", "primary_citation_verification": primaryArtifact, "secondary_candidates": candidateArtifact, "reports": reports, "counter_verification": "All counter full SHA256s were verified in the referenced immediately preceding primary citation assessment. This step checks size/modtime, target UNKNOWN status, exact ordinals and selected counter-byte hashes; it does not falsely claim a second full counter SHA rescan.", "completed_at": time.Now().UTC(), "production_mutated": false})
}

func findTargets(path string, rows int, flags map[uint64]byte, xpac map[uint64]bool, corpus string) ([][]selectedTarget, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	var header [8]byte
	if _, e = io.ReadFull(r, header[:]); e != nil || string(header[:]) != "OACOLS01" {
		return nil, fmt.Errorf("bad dimensions header")
	}
	length := func() (uint64, error) {
		_, e := io.ReadFull(r, header[:])
		return binary.LittleEndian.Uint64(header[:]), e
	}
	n, e := length()
	if e != nil || n > 16<<20 {
		return nil, fmt.Errorf("bad dictionary length")
	}
	if _, e = io.CopyN(io.Discard, r, int64(n)); e != nil {
		return nil, e
	}
	n, e = length()
	if e != nil || n != uint64(rows) {
		return nil, fmt.Errorf("bad target row count")
	}
	selected := make([][]selectedTarget, 64)
	buf := make([]byte, 1<<20)
	found := 0
	expected := 0
	for id := range flags {
		if corpus == "all" || !xpac[id] {
			expected++
		}
	}
	for start := 0; start < rows; {
		take := min(len(buf)/8, rows-start)
		if _, e = io.ReadFull(r, buf[:take*8]); e != nil {
			return nil, e
		}
		for k := 0; k < take; k++ {
			id := binary.LittleEndian.Uint64(buf[k*8:])
			b, ok := flags[id]
			if !ok || corpus == "core" && xpac[id] {
				continue
			}
			ordinal := start + k
			selected[ordinal%64] = append(selected[ordinal%64], selectedTarget{ordinal, b, id})
			found++
		}
		start += take
	}
	if found != expected {
		return nil, fmt.Errorf("candidate membership mismatch %s: %d vs %d", corpus, found, expected)
	}
	// Continue in the same buffered stream until countryOffsets; record each
	// sparse target's baseline country cardinality without loading all columns.
	for _, size := range []uint64{4, 2, 2, 2, 1} {
		n, e = length()
		if e != nil || n != uint64(rows) {
			return nil, fmt.Errorf("bad sibling array")
		}
		if _, e = io.CopyN(io.Discard, r, int64(n*size)); e != nil {
			return nil, e
		}
	}
	n, e = length()
	if e != nil || n != uint64(rows+1) {
		return nil, fmt.Errorf("bad country offsets")
	}
	position, e := f.Seek(0, io.SeekCurrent)
	if e != nil {
		return nil, e
	}
	offsetStart := position - int64(r.Buffered())
	var pair [16]byte
	for _, targets := range selected {
		for _, t := range targets {
			if _, e = f.ReadAt(pair[:], offsetStart+int64(t.ordinal)*8); e != nil {
				return nil, e
			}
			if binary.LittleEndian.Uint64(pair[:8]) != binary.LittleEndian.Uint64(pair[8:]) {
				return nil, fmt.Errorf("secondary candidate was already country-known: %d", t.id)
			}
		}
	}
	return selected, nil
}
