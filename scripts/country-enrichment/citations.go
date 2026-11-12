package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"openalexstats/internal/checkpoint"
)

type citationFlags struct {
	ID       uint64   `parquet:"id"`
	Original []string `parquet:"original_countries,list"`
	Inst     []string `parquet:"institution_added,list"`
	Text     []string `parquet:"text_added,list"`
	New      bool     `parquet:"newly_known"`
	Added    []string `parquet:"added_countries,list"`
	Review   []string `parquet:"review_only_countries,list"`
}
type CitationYear struct {
	Year        int    `json:"year"`
	Events      uint64 `json:"events"`
	Missing     uint64 `json:"baseline_missing_country_events"`
	Institution uint64 `json:"newly_known_institution_events"`
	Text        uint64 `json:"newly_known_text_events"`
	Both        uint64 `json:"newly_known_both_events"`
	Combined    uint64 `json:"newly_known_combined_events"`
	Extended    uint64 `json:"already_known_extended_events"`
	Review      uint64 `json:"review_only_missing_events"`
}

func (s *CitationYear) add(t CitationYear) {
	s.Events += t.Events
	s.Missing += t.Missing
	s.Institution += t.Institution
	s.Text += t.Text
	s.Both += t.Both
	s.Combined += t.Combined
	s.Extended += t.Extended
	s.Review += t.Review
}

type CitationReport struct {
	Corpus          string                `json:"corpus"`
	Rows            int                   `json:"target_rows"`
	Total           CitationYear          `json:"totals"`
	Years           []CitationYear        `json:"years"`
	Dimensions      checkpoint.Artifact   `json:"dimensions"`
	Counters        []checkpoint.Artifact `json:"counters"`
	BaselinePercent float64               `json:"baseline_country_coverage_percent"`
	EnhancedPercent float64               `json:"conservative_candidate_coverage_percent"`
	Seconds         float64               `json:"seconds"`
}

func citations(ctx context.Context, c Config) error {
	var completion struct {
		Status string `json:"status"`
	}
	if er := checkpoint.ReadJSON(filepath.Join(c.Work, "completion.json"), &completion); er != nil {
		return er
	}
	if completion.Status != "full_snapshot_evaluation_complete" {
		return fmt.Errorf("citation assessment requires complete full-source evaluation")
	}
	files, er := filepath.Glob(filepath.Join(c.Work, "patch", "*.parquet"))
	if er != nil {
		return er
	}
	if len(files) != 2446 {
		return fmt.Errorf("expected 2446 patch shards")
	}
	flags := map[uint64]byte{}
	for _, p := range files {
		if er = ctx.Err(); er != nil {
			return er
		}
		if er = each[citationFlags](p, func(r citationFlags) error {
			var v byte
			if len(r.Original) > 0 {
				v |= 1
			}
			if r.New {
				v |= 8
				if len(r.Inst) > 0 {
					v |= 2
				}
				if len(r.Text) > 0 {
					v |= 4
				}
			}
			if len(r.Original) > 0 && len(r.Added) > 0 {
				v |= 16
			}
			if len(r.Original) == 0 && len(r.Review) > 0 && !r.New {
				v |= 32
			}
			if _, ok := flags[r.ID]; ok {
				return fmt.Errorf("duplicate patch ID %d", r.ID)
			}
			flags[r.ID] = v
			return nil
		}); er != nil {
			return er
		}
	}
	log(map[string]any{"stage": "citation_flags_loaded", "patch_works": len(flags)})
	var reports []CitationReport
	for _, spec := range []struct {
		corpus, work, counters string
		rows                   int
		events, missing        uint64
	}{{"core", c.CoreWork, c.CoreCounters, 269194661, 2363318327, 276033100}, {"all", c.AllWork, c.AllCounters, 404079112, 2385146215, 281487360}} {
		r, er := citationCorpus(ctx, spec.corpus, spec.work, spec.counters, spec.rows, min(c.Workers, 12), flags)
		if er != nil {
			return er
		}
		if r.Total.Events != spec.events || r.Total.Missing != spec.missing {
			return fmt.Errorf("%s citation baseline disagrees with immutable production run: events %d missing %d", spec.corpus, r.Total.Events, r.Total.Missing)
		}
		reports = append(reports, r)
	}
	return checkpoint.JSON(filepath.Join(c.Work, "citation-summary.json"), map[string]any{"status": "passed", "reports": reports, "graph_recomputed": false, "year_definition": "citing Work publication year, attributed to cited Work", "production_mutated": false, "completed_at": time.Now().UTC()})
}

func citationCorpus(ctx context.Context, corpus, work, counterDir string, expectedRows, workers int, flags map[uint64]byte) (CitationReport, error) {
	start := time.Now()
	var dm struct {
		Shape struct {
			Rows      int    `json:"rows"`
			Countries uint64 `json:"country_entries"`
		} `json:"shape"`
		Columns checkpoint.Artifact `json:"columns"`
	}
	var out CitationReport
	out.Corpus = corpus
	if er := checkpoint.ReadJSON(filepath.Join(work, "dimensions.json"), &dm); er != nil {
		return out, er
	}
	if dm.Shape.Rows != expectedRows {
		return out, fmt.Errorf("dimension shape mismatch")
	}
	if er := dm.Columns.Verify(); er != nil {
		return out, er
	}
	out.Dimensions = dm.Columns
	ids, missing, er := readDimensionCountryPresence(dm.Columns.Path, expectedRows, dm.Shape.Countries)
	if er != nil {
		return out, er
	}
	out.Rows = len(ids)
	log(map[string]any{"stage": "citation_dimensions_loaded", "corpus": corpus, "rows": len(ids)})
	type counterMeta struct {
		Partition, Rows, Years int
		Sum                    uint64
		Artifact               checkpoint.Artifact
	}
	metas := make([]counterMeta, 64)
	for p := range metas {
		path := filepath.Join(counterDir, fmt.Sprintf("target-%03d.counts.json", p))
		if er = checkpoint.ReadJSON(path, &metas[p]); er != nil {
			return out, er
		}
		m := metas[p]
		if m.Partition != p || m.Rows != len(ids) || m.Years != 27 {
			return out, fmt.Errorf("counter metadata shape mismatch %s", path)
		}
		out.Counters = append(out.Counters, m.Artifact)
	}
	results := make([][]CitationYear, 64)
	errs := make([]error, 64)
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				m := metas[p]
				results[p] = make([]CitationYear, 27)
				for y := range results[p] {
					results[p][y].Year = 2000 + y
				}
				f, e := os.Open(m.Artifact.Path)
				if e != nil {
					errs[p] = e
					continue
				}
				h := sha256.New()
				r := bufio.NewReaderSize(io.TeeReader(f, h), 1<<20)
				buf := make([]byte, 27*8*4096)
				var sum, readBytes uint64
				for row := p; row < len(ids); {
					if e = ctx.Err(); e != nil {
						break
					}
					take := min(4096, (len(ids)-1-row)/64+1)
					n := take * 27 * 8
					if _, e = io.ReadFull(r, buf[:n]); e != nil {
						break
					}
					readBytes += uint64(n)
					for k := 0; k < take; k++ {
						ordinal := row + k*64
						fl, found := flags[ids[ordinal]]
						isMissing := missing[ordinal>>3]&(1<<uint(ordinal&7)) != 0
						if found && ((fl&1 == 0) != isMissing) {
							e = fmt.Errorf("patch/dimension baseline mismatch for %d", ids[ordinal])
							break
						}
						for y := 0; y < 27; y++ {
							v := binary.LittleEndian.Uint64(buf[(k*27+y)*8:])
							if v == 0 {
								continue
							}
							s := &results[p][y]
							s.Events += v
							sum += v
							if isMissing {
								s.Missing += v
							}
							if fl&2 != 0 {
								s.Institution += v
							}
							if fl&4 != 0 {
								s.Text += v
							}
							if fl&6 == 6 {
								s.Both += v
							}
							if fl&8 != 0 {
								s.Combined += v
							}
							if fl&16 != 0 {
								s.Extended += v
							}
							if fl&32 != 0 {
								s.Review += v
							}
						}
					}
					if e != nil {
						break
					}
					row += take * 64
				}
				if e == nil {
					if _, ee := r.ReadByte(); ee != io.EOF {
						e = fmt.Errorf("counter trailing bytes")
					}
				}
				_ = f.Close()
				if e == nil && (sum != m.Sum || int64(readBytes) != m.Artifact.Bytes || hex.EncodeToString(h.Sum(nil)) != m.Artifact.SHA256) {
					e = fmt.Errorf("counter SHA/bytes/sum mismatch partition %d", p)
				}
				errs[p] = e
				if e == nil {
					log(map[string]any{"stage": "citation_counter_verified", "corpus": corpus, "partition": p, "sum": sum})
				}
			}
		}()
	}
	for p := range metas {
		jobs <- p
	}
	close(jobs)
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return out, e
		}
	}
	out.Years = make([]CitationYear, 27)
	for y := range out.Years {
		out.Years[y].Year = 2000 + y
		for p := range metas {
			out.Years[y].add(results[p][y])
		}
		out.Total.add(out.Years[y])
	}
	if out.Total.Combined != out.Total.Institution+out.Total.Text-out.Total.Both {
		return out, fmt.Errorf("citation enhancement overlap conservation")
	}
	out.BaselinePercent = 100 * float64(out.Total.Events-out.Total.Missing) / float64(out.Total.Events)
	out.EnhancedPercent = 100 * float64(out.Total.Events-out.Total.Missing+out.Total.Combined) / float64(out.Total.Events)
	out.Seconds = time.Since(start).Seconds()
	log(map[string]any{"stage": "citation_corpus_complete", "corpus": corpus, "totals": out.Total, "seconds": out.Seconds})
	return out, nil
}

func readDimensionCountryPresence(path string, rows int, expectedCountryEntries uint64) ([]uint64, []byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, nil, e
	}
	defer f.Close()
	var b [8]byte
	if _, e = io.ReadFull(f, b[:]); e != nil || string(b[:]) != "OACOLS01" {
		return nil, nil, fmt.Errorf("invalid dimension magic")
	}
	length := func() (uint64, error) { _, er := io.ReadFull(f, b[:]); return binary.LittleEndian.Uint64(b[:]), er }
	n, e := length()
	if e != nil || n > 16<<20 {
		return nil, nil, fmt.Errorf("invalid dimension header")
	}
	if _, e = f.Seek(int64(n), io.SeekCurrent); e != nil {
		return nil, nil, e
	}
	n, e = length()
	if e != nil || n != uint64(rows) {
		return nil, nil, fmt.Errorf("invalid dimension ids length")
	}
	ids := make([]uint64, rows)
	buf := make([]byte, 1<<20)
	for i := 0; i < rows; {
		take := min(len(buf)/8, rows-i)
		if _, e = io.ReadFull(f, buf[:take*8]); e != nil {
			return nil, nil, e
		}
		for k := 0; k < take; k++ {
			ids[i+k] = binary.LittleEndian.Uint64(buf[k*8:])
		}
		i += take
	}
	for _, size := range []uint64{4, 2, 2, 2, 1} {
		n, e = length()
		if e != nil || n != uint64(rows) {
			return nil, nil, fmt.Errorf("invalid dimension sibling array")
		}
		if _, e = f.Seek(int64(n*size), io.SeekCurrent); e != nil {
			return nil, nil, e
		}
	}
	n, e = length()
	if e != nil || n != uint64(rows+1) {
		return nil, nil, fmt.Errorf("invalid country offsets length")
	}
	missing := make([]byte, (rows+7)/8)
	var prior uint64
	for i := 0; i <= rows; {
		take := min(len(buf)/8, rows+1-i)
		if _, e = io.ReadFull(f, buf[:take*8]); e != nil {
			return nil, nil, e
		}
		for k := 0; k < take; k++ {
			ordinal := i + k
			value := binary.LittleEndian.Uint64(buf[k*8:])
			if ordinal == 0 && value != 0 || value < prior {
				return nil, nil, fmt.Errorf("nonmonotone country offsets")
			}
			if ordinal > 0 && value == prior {
				missing[(ordinal-1)>>3] |= 1 << uint((ordinal-1)&7)
			}
			prior = value
		}
		i += take
	}
	if prior != expectedCountryEntries {
		return nil, nil, fmt.Errorf("country entries mismatch")
	}
	return ids, missing, nil
}
