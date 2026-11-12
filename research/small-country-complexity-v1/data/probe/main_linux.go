//go:build linux

package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

type array struct {
	offset, length uint64
	width          uint64
}
type columns struct {
	data   []byte
	dict   [][]string
	arrays map[string]array
}

var names = []string{"ids", "years", "types", "fields", "subfields", "flags", "countryOffsets", "conceptOffsets", "countries", "conceptIDs", "conceptLevels", "conceptScores"}
var widths = []uint64{8, 4, 2, 2, 2, 1, 8, 8, 2, 2, 1, 8}

func openColumns(path string) *columns {
	f, e := os.Open(path)
	must(e)
	s, e := f.Stat()
	must(e)
	d, e := syscall.Mmap(int(f.Fd()), 0, int(s.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	must(e)
	f.Close()
	if string(d[:8]) != "OACOLS01" {
		panic("bad columns magic")
	}
	n := binary.LittleEndian.Uint64(d[8:16])
	var h struct {
		Version      int
		Dictionaries [][]string
	}
	must(json.Unmarshal(d[16:16+n], &h))
	if h.Version != 1 {
		panic("bad version")
	}
	c := &columns{data: d, dict: h.Dictionaries, arrays: map[string]array{}}
	p := 16 + n
	for i, name := range names {
		n = binary.LittleEndian.Uint64(d[p : p+8])
		p += 8
		c.arrays[name] = array{p, n, widths[i]}
		p += n * widths[i]
	}
	if p != uint64(len(d)) {
		panic("trailing columns bytes")
	}
	return c
}
func (c *columns) get(name string, i uint64) uint64 {
	a := c.arrays[name]
	if i >= a.length {
		panic(name + " out of bounds")
	}
	p := a.offset + i*a.width
	switch a.width {
	case 1:
		return uint64(c.data[p])
	case 2:
		return uint64(binary.LittleEndian.Uint16(c.data[p : p+2]))
	case 4:
		return uint64(binary.LittleEndian.Uint32(c.data[p : p+4]))
	default:
		return binary.LittleEndian.Uint64(c.data[p : p+8])
	}
}
func must(e error) {
	if e != nil {
		panic(e)
	}
}

type concept struct {
	ID    string  `json:"id"`
	Level uint64  `json:"level"`
	Score float64 `json:"score"`
}
type work struct {
	Ordinal     uint64    `json:"ordinal"`
	ID          uint64    `json:"id"`
	Year        int32     `json:"year"`
	Type        string    `json:"type"`
	Field       string    `json:"field"`
	Subfield    string    `json:"subfield"`
	Countries   []string  `json:"countries"`
	Concepts    []concept `json:"concepts"`
	Citations   []uint64  `json:"citations_2000_2026"`
	RawPath     string    `json:"raw_path"`
	IsXPAC      bool      `json:"is_xpac"`
	IsRetracted bool      `json:"is_retracted"`
}

func main() {
	if len(os.Args) != 6 {
		panic("usage: probe dimensions counts_dir dimension_parts output.json country")
	}
	country := os.Args[5]
	start := time.Now()
	c := openColumns(os.Args[1])
	defer syscall.Munmap(c.data)
	code := uint64(0)
	for i, s := range c.dict[3] {
		if s == country {
			code = uint64(i)
		}
	}
	if code == 0 {
		panic("Country absent: " + country)
	}
	ords := make([]uint64, 0, 20000)
	cs := c.arrays["countries"]
	offsets := c.arrays["countryOffsets"]
	var last uint64 = ^uint64(0)
	for j := uint64(0); j < cs.length; j++ {
		if c.get("countries", j) == code {
			i := uint64(sort.Search(int(offsets.length), func(i int) bool { return c.get("countryOffsets", uint64(i)) > j }) - 1)
			if i != last {
				ords = append(ords, i)
				last = i
			}
		}
	}
	fmt.Fprintf(os.Stderr, "%s selected works %d, country scan %s\n", country, len(ords), time.Since(start))
	metas, e := filepath.Glob(filepath.Join(os.Args[3], "part-*.json"))
	must(e)
	sort.Strings(metas)
	type part struct {
		Source    string
		Shape     struct{ Rows uint64 }
		Inventory struct{ Selected uint64 }
	}
	var rawparts []part
	var ends []uint64
	var end uint64
	for _, p := range metas {
		b, e := os.ReadFile(p)
		must(e)
		var m part
		must(json.Unmarshal(b, &m))
		end += m.Shape.Rows
		ends = append(ends, end)
		rawparts = append(rawparts, m)
	}
	if end != c.arrays["ids"].length {
		panic(fmt.Sprintf("part rows mismatch %d %d", end, c.arrays["ids"].length))
	}
	cb, e := os.ReadFile("/mnt/nvme0/public/openalex-stats/cache-20260626/manifest.json")
	must(e)
	var cm struct {
		Parts []struct {
			Path      string `json:"path"`
			InputPath string `json:"input_path"`
		} `json:"parts"`
	}
	must(json.Unmarshal(cb, &cm))
	rawMap := map[string]string{}
	for _, p := range cm.Parts {
		rawMap[p.Path] = p.InputPath
	}
	countFiles, e := filepath.Glob(filepath.Join(os.Args[2], "target-*.counts"))
	must(e)
	sort.Strings(countFiles)
	if len(countFiles) == 0 {
		panic("no counts")
	}
	files := make([]*os.File, len(countFiles))
	for i, p := range countFiles {
		files[i], e = os.Open(p)
		must(e)
		defer files[i].Close()
	}
	var out []work
	buf := make([]byte, 27*8)
	for _, ord := range ords {
		p := sort.Search(len(ends), func(i int) bool { return ends[i] > ord })
		w := work{Ordinal: ord, ID: c.get("ids", ord), Year: int32(c.get("years", ord)), Type: c.dict[0][c.get("types", ord)], Field: c.dict[1][c.get("fields", ord)], Subfield: c.dict[2][c.get("subfields", ord)], RawPath: rawMap[rawparts[p].Source], IsXPAC: c.get("flags", ord)&1 > 0, IsRetracted: c.get("flags", ord)&4 > 0}
		if w.RawPath == "" {
			panic("raw map missing")
		}
		for j := c.get("countryOffsets", ord); j < c.get("countryOffsets", ord+1); j++ {
			w.Countries = append(w.Countries, c.dict[3][c.get("countries", j)])
		}
		for j := c.get("conceptOffsets", ord); j < c.get("conceptOffsets", ord+1); j++ {
			w.Concepts = append(w.Concepts, concept{c.dict[4][c.get("conceptIDs", j)], c.get("conceptLevels", j), math.Float64frombits(c.get("conceptScores", j))})
		}
		n, e := files[ord%uint64(len(files))].ReadAt(buf, int64(ord/uint64(len(files))*27*8))
		must(e)
		if n != len(buf) {
			panic("short counts")
		}
		for y := 0; y < 27; y++ {
			w.Citations = append(w.Citations, binary.LittleEndian.Uint64(buf[y*8:y*8+8]))
		}
		out = append(out, w)
	}
	b, e := json.MarshalIndent(map[string]any{"version": "small-country-original-work-citation-audit-v1", "country": country, "source_dimensions": os.Args[1], "count_directory": os.Args[2], "count_partitions": len(files), "works": out, "elapsed_seconds": time.Since(start).Seconds()}, "", "  ")
	must(e)
	must(os.WriteFile(os.Args[4], b, 0644))
	fmt.Fprintf(os.Stderr, "done %d works %s\n", len(out), time.Since(start))
}
