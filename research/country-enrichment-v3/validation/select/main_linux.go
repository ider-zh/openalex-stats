//go:build linux

// Probability sample from every quality-selected work with no original country.
package main

import (
	"container/heap"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"
)

type arr struct{ off, n, width uint64 }
type columns struct {
	data []byte
	dict [][]string
	a    map[string]arr
}

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func open(path string) *columns {
	f, e := os.Open(path)
	must(e)
	st, e := f.Stat()
	must(e)
	d, e := syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	must(e)
	f.Close()
	if string(d[:8]) != "OACOLS01" {
		panic("magic")
	}
	n := binary.LittleEndian.Uint64(d[8:16])
	var h struct {
		Version      int
		Dictionaries [][]string
	}
	must(json.Unmarshal(d[16:16+n], &h))
	if h.Version != 1 {
		panic("version")
	}
	c := &columns{d, h.Dictionaries, map[string]arr{}}
	p := 16 + n
	names := []string{"ids", "years", "types", "fields", "subfields", "flags", "countryOffsets", "conceptOffsets", "countries", "conceptIDs", "conceptLevels", "conceptScores"}
	width := []uint64{8, 4, 2, 2, 2, 1, 8, 8, 2, 2, 1, 8}
	for i, s := range names {
		n = binary.LittleEndian.Uint64(d[p : p+8])
		p += 8
		c.a[s] = arr{p, n, width[i]}
		p += n * width[i]
	}
	if p != uint64(len(d)) {
		panic("shape")
	}
	return c
}

type point struct {
	Ord, ID, Hash uint64
	Stratum       int
}
type maxheap []point

func (h maxheap) Len() int { return len(h) }
func (h maxheap) Less(i, j int) bool {
	return h[i].Hash > h[j].Hash || (h[i].Hash == h[j].Hash && h[i].ID > h[j].ID)
}
func (h maxheap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *maxheap) Push(x any)   { *h = append(*h, x.(point)) }
func (h *maxheap) Pop() any     { a := *h; x := a[len(a)-1]; *h = a[:len(a)-1]; return x }
func keep(h *maxheap, p point, k int) {
	if h.Len() < k {
		heap.Push(h, p)
	} else if p.Hash < (*h)[0].Hash || (p.Hash == (*h)[0].Hash && p.ID < (*h)[0].ID) {
		(*h)[0] = p
		heap.Fix(h, 0)
	}
}

type bucket struct {
	N uint64
	OriginalN uint64
	ExcludedN uint64
	H maxheap
}
type record struct {
	ID          uint64  `json:"id"`
	WorkID      string  `json:"work_id"`
	Ordinal     uint64  `json:"ordinal"`
	Year        int32   `json:"year"`
	Type        string  `json:"type"`
	IsXPAC      bool    `json:"is_xpac"`
	Stratum     string  `json:"stratum"`
	Hash        string  `json:"sample_hash_u64"`
	N           uint64  `json:"stratum_population"`
	Nsample     int     `json:"stratum_sample"`
	Pi          float64 `json:"inclusion_probability"`
	Weight      float64 `json:"design_weight"`
	RawPath     string  `json:"raw_path"`
	CachePath   string  `json:"cache_path"`
	SourceIndex int     `json:"source_index"`
}

var eras = []string{"unknown_year", "before_2000", "2000_2009", "2010_2019", "2020_2025", "2026_partial", "after_2026"}
var groups = []string{"article", "review", "book_chapter", "book", "other"}

func era(y int32) int {
	switch {
	case y <= 0:
		return 0
	case y < 2000:
		return 1
	case y < 2010:
		return 2
	case y < 2020:
		return 3
	case y < 2026:
		return 4
	case y == 2026:
		return 5
	default:
		return 6
	}
}
func typename(s string) int {
	switch s {
	case "article":
		return 0
	case "review":
		return 1
	case "book-chapter":
		return 2
	case "book":
		return 3
	default:
		return 4
	}
}
func key(s int) string {
	x := s / 35
	r := (s % 35) / 5
	t := s % 5
	cor := "core"
	if x == 1 {
		cor = "xpac"
	}
	return cor + "/" + eras[r] + "/" + groups[t]
}
func main() {
	if len(os.Args) != 6 {
		panic("select dimensions dimension_parts cache_manifest output_dir development_exclusions_json")
	}
	start := time.Now()
	out := os.Args[4]
	must(os.MkdirAll(out, 0755))
	c := open(os.Args[1])
	defer syscall.Munmap(c.data)
	const K = 256
	const Workers = 24
	const Strata = 70
	seed := "openalex-country-enrichment-v3-20261112-blind-holdout-01"
	excludeBytes, e := os.ReadFile(os.Args[5]); must(e)
	var exclusions struct{ IDs []uint64 `json:"ids"` }; must(json.Unmarshal(excludeBytes,&exclusions))
	exclude := make(map[uint64]bool,len(exclusions.IDs)); for _,id:=range exclusions.IDs{exclude[id]=true}
	excludeSHA:=sha256.Sum256(excludeBytes)
	seedhash := sha256.Sum256([]byte(seed))
	rows := c.a["ids"].n
	ids, ys, ts, fs, co := c.a["ids"], c.a["years"], c.a["types"], c.a["flags"], c.a["countryOffsets"]
	if co.n != rows+1 {
		panic("country offset shape")
	}
	typegroups := make([]int, len(c.dict[0]))
	for i, t := range c.dict[0] {
		typegroups[i] = typename(t)
	}
	results := make(chan []bucket, Workers)
	var wg sync.WaitGroup
	for w := 0; w < Workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			b := make([]bucket, Strata)
			var hashbuf [40]byte
			copy(hashbuf[:32], seedhash[:])
			lo, hi := rows*uint64(w)/Workers, rows*uint64(w+1)/Workers
			for i := lo; i < hi; i++ {
				p := co.off + i*8
				if binary.LittleEndian.Uint64(c.data[p:p+8]) != binary.LittleEndian.Uint64(c.data[p+8:p+16]) {
					continue
				}
				y := int32(binary.LittleEndian.Uint32(c.data[ys.off+i*4 : ys.off+i*4+4]))
				typ := binary.LittleEndian.Uint16(c.data[ts.off+i*2 : ts.off+i*2+2])
				xp := int(c.data[fs.off+i] & 1)
				s := (xp*7+era(y))*5 + typegroups[typ]
				id := binary.LittleEndian.Uint64(c.data[ids.off+i*8 : ids.off+i*8+8])
				b[s].OriginalN++
				if exclude[id] { b[s].ExcludedN++; continue }
				b[s].N++
				binary.LittleEndian.PutUint64(hashbuf[32:], id)
				hh := sha256.Sum256(hashbuf[:])
				keep(&b[s].H, point{i, id, binary.BigEndian.Uint64(hh[:8]), s}, K)
			}
			results <- b
		}(w)
	}
	wg.Wait()
	close(results)
	total := make([]bucket, Strata)
	for b := range results {
		for s, x := range b {
			total[s].N += x.N
			total[s].OriginalN += x.OriginalN
			total[s].ExcludedN += x.ExcludedN
			for _, p := range x.H {
				keep(&total[s].H, p, K)
			}
		}
	}
	var missing, originalMissing, excludedFound uint64
	for _, b := range total {
		missing += b.N
		originalMissing += b.OriginalN
		excludedFound += b.ExcludedN
	}
	if originalMissing != 277418468 || excludedFound < 6559 || missing+excludedFound != originalMissing {
		panic(fmt.Sprintf("baseline unknown/exclusion mismatch original=%d eligible=%d excluded=%d", originalMissing,missing,excludedFound))
	}
	metas, e := filepath.Glob(filepath.Join(os.Args[2], "part-*.json"))
	must(e)
	sort.Strings(metas)
	type part struct {
		Source string
		Shape  struct{ Rows uint64 }
	}
	var parts []part
	var ends []uint64
	var end uint64
	for _, p := range metas {
		data, e := os.ReadFile(p)
		must(e)
		var x part
		must(json.Unmarshal(data, &x))
		end += x.Shape.Rows
		parts = append(parts, x)
		ends = append(ends, end)
	}
	if end != rows {
		panic("dimension parts identity mismatch")
	}
	data, e := os.ReadFile(os.Args[3])
	must(e)
	var cm struct {
		Parts []struct {
			Path      string `json:"path"`
			InputPath string `json:"input_path"`
			Index     int    `json:"index"`
		}
	}
	must(json.Unmarshal(data, &cm))
	raw := map[string]string{}
	index := map[string]int{}
	for _, p := range cm.Parts {
		raw[p.Path] = p.InputPath
		index[p.Path] = p.Index
	}
	var works []record
	var strata []map[string]any
	var coren, xpacn uint64
	var originalCore,originalXPAC uint64
	for s, b := range total {
		if b.OriginalN == 0 {
			continue
		}
		if s/35==0{originalCore+=b.OriginalN}else{originalXPAC+=b.OriginalN}
		sort.Slice(b.H, func(i, j int) bool {
			return b.H[i].Hash < b.H[j].Hash || (b.H[i].Hash == b.H[j].Hash && b.H[i].ID < b.H[j].ID)
		})
		n := len(b.H)
		pi,weight:=0.,0.;if b.N>0{pi=float64(n)/float64(b.N);weight=1/pi}
		strata = append(strata, map[string]any{"stratum": key(s), "original_N":b.OriginalN,"excluded_development_N":b.ExcludedN,"N": b.N, "n": n, "inclusion_probability": pi, "design_weight":weight})
		if s/35 == 0 {
			coren += b.N
		} else {
			xpacn += b.N
		}
		for _, p := range b.H {
			i := p.Ord
			j := sort.Search(len(ends), func(j int) bool { return ends[j] > i })
			cache := parts[j].Source
			path := raw[cache]
			if path == "" {
				panic("raw source mapping missing")
			}
			works = append(works, record{p.ID, fmt.Sprintf("https://openalex.org/W%d", p.ID), i, int32(binary.LittleEndian.Uint32(c.data[ys.off+i*4 : ys.off+i*4+4])), c.dict[0][binary.LittleEndian.Uint16(c.data[ts.off+i*2:ts.off+i*2+2])], c.data[fs.off+i]&1 > 0, key(s), fmt.Sprintf("%016x", p.Hash), b.N, n, pi, 1 / pi, path, cache, index[cache]})
		}
	}
	if originalCore != 150377414 || originalXPAC != 127041054 {
		panic("core/xpac unknown totals mismatch")
	}
	result := map[string]any{"status": "frozen_blind_holdout_probability_ids", "version": "country-enrichment-holdout-v3-01", "frame": "Every original quality-selected UNKNOWN Work excluding all explicitly exposed development/QA IDs", "seed": seed, "seed_sha256": hex.EncodeToString(seedhash[:]), "hash": "first64bigendian(SHA256(SHA256(seed)||uint64LE(workID)))", "selection": "bottom256 hashes per original Core-vs-XPAC/era/type-group over the eligible nondevelopment population; pseudorandom hash SRS within stratum", "stratum_cap": K, "rows_examined": rows, "original_baseline_unknown":originalMissing,"development_exclusions_found_in_original_unknown":excludedFound,"development_exclusions_sha256":hex.EncodeToString(excludeSHA[:]),"baseline_unknown": missing, "core_unknown": coren, "xpac_unknown": xpacn, "sample_size": len(works), "strata": strata, "works": works, "elapsed_seconds": time.Since(start).Seconds(), "automatic_application": false}
	b, e := json.MarshalIndent(result, "", "  ")
	must(e)
	must(os.WriteFile(filepath.Join(out, "sample-ids.json"), b, 0644))
	fmt.Printf("sample=%d unknown=%d strata=%d seconds=%.2f\n", len(works), missing, len(strata), time.Since(start).Seconds())
}
