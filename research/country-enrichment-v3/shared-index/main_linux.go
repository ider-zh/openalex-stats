//go:build linux

// Export the immutable baseline UNKNOWN frame with exact original row positions.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"
)

type arr struct{ off, n uint64 }
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
		c.a[s] = arr{p, n}
		p += n * width[i]
	}
	if p != uint64(len(d)) {
		panic("shape")
	}
	return c
}

type row struct {
	ID          uint64 `parquet:"id"`
	Ordinal     uint64 `parquet:"ordinal"`
	Year        int32  `parquet:"year"`
	Type        string `parquet:"type,dict"`
	XPAC        bool   `parquet:"is_xpac"`
	SourceIndex int32  `parquet:"source_index"`
	RawRow      uint64 `parquet:"raw_row_index"`
}
type part struct {
	Index int    `json:"source_index"`
	Cache string `json:"cache_path"`
	Raw   string `json:"raw_path"`
	IDs   string `json:"raw_ids_path"`
	Begin uint64 `json:"selected_ordinal_begin"`
	End   uint64 `json:"selected_ordinal_end"`
	Path  string `json:"path"`
	SHA   string `json:"sha256"`
	Rows  uint64 `json:"rows"`
	Core  uint64 `json:"core"`
	XPAC  uint64 `json:"xpac"`
}

func jsonFile(path string, v any) {
	b, e := json.MarshalIndent(v, "", "  ")
	must(e)
	tmp := path + ".tmp"
	must(os.WriteFile(tmp, append(b, '\n'), 0600))
	must(os.Rename(tmp, path))
}
func work(c *columns, p part, out string) part {
	cp := filepath.Join(out, fmt.Sprintf("part-%05d.parquet", p.Index))
	mp := cp + ".meta.json"
	if b, e := os.ReadFile(mp); e == nil {
		var old part
		if json.Unmarshal(b, &old) == nil && old.Begin == p.Begin && old.End == p.End && old.Raw == p.Raw && old.Cache == p.Cache {
			f, e := os.Open(old.Path)
			must(e)
			h := sha256.New()
			_, e = io.Copy(h, f)
			must(e)
			f.Close()
			if hex.EncodeToString(h.Sum(nil)) != old.SHA {
				panic("checkpoint hash")
			}
			return old
		}
	}
	ids, ys, ts, fs, co := c.a["ids"], c.a["years"], c.a["types"], c.a["flags"], c.a["countryOffsets"]
	wanted := map[uint64]uint64{}
	for i := p.Begin; i < p.End; i++ {
		a := co.off + i*8
		if binary.LittleEndian.Uint64(c.data[a:a+8]) == binary.LittleEndian.Uint64(c.data[a+8:a+16]) {
			wanted[binary.LittleEndian.Uint64(c.data[ids.off+i*8:ids.off+i*8+8])] = i
		}
	}
	raw, e := os.ReadFile(p.IDs)
	must(e)
	n := binary.LittleEndian.Uint64(raw[:8])
	if uint64(len(raw)) != 8+8*n {
		panic("raw ids shape")
	}
	f, e := os.Create(cp + ".tmp")
	must(e)
	h := sha256.New()
	w := parquet.NewGenericWriter[row](io.MultiWriter(f, h), parquet.Compression(&zstd.Codec{Level: zstd.SpeedFastest, Concurrency: 1}), parquet.MaxRowsPerRowGroup(65536))
	batch := make([]row, 0, 4096)
	flush := func() {
		if len(batch) > 0 {
			n, e := w.Write(batch)
			must(e)
			if n != len(batch) {
				panic("short parquet write")
			}
			batch = batch[:0]
		}
	}
	for j := uint64(0); j < n; j++ {
		id := binary.LittleEndian.Uint64(raw[8+j*8 : 16+j*8])
		i, ok := wanted[id]
		if !ok {
			continue
		}
		delete(wanted, id)
		r := row{id, i, int32(binary.LittleEndian.Uint32(c.data[ys.off+i*4 : ys.off+i*4+4])), c.dict[0][binary.LittleEndian.Uint16(c.data[ts.off+i*2:ts.off+i*2+2])], c.data[fs.off+i]&1 > 0, int32(p.Index), j}
		batch = append(batch, r)
		p.Rows++
		if r.XPAC {
			p.XPAC++
		} else {
			p.Core++
		}
		if len(batch) == cap(batch) {
			flush()
		}
	}
	if len(wanted) != 0 {
		panic("original positions not found")
	}
	flush()
	must(w.Close())
	must(f.Sync())
	must(f.Close())
	must(os.Rename(cp+".tmp", cp))
	p.Path = cp
	p.SHA = hex.EncodeToString(h.Sum(nil))
	jsonFile(mp, p)
	return p
}
func main() {
	if len(os.Args) != 5 {
		panic("shared-index dimensions dimension-parts cache-manifest outdir")
	}
	t := time.Now()
	out := os.Args[4]
	must(os.MkdirAll(out, 0700))
	c := open(os.Args[1])
	defer syscall.Munmap(c.data)
	b, e := os.ReadFile(os.Args[3])
	must(e)
	var cm struct {
		Parts []struct {
			Path  string `json:"path"`
			Input string `json:"input_path"`
			Index int    `json:"index"`
		}
	}
	must(json.Unmarshal(b, &cm))
	lookup := map[string]part{}
	for _, p := range cm.Parts {
		lookup[p.Path] = part{Index: p.Index, Cache: p.Path, Raw: p.Input}
	}
	metas, e := filepath.Glob(filepath.Join(os.Args[2], "part-*.json"))
	must(e)
	sort.Strings(metas)
	ps := []part{}
	var end uint64
	for _, m := range metas {
		b, e := os.ReadFile(m)
		must(e)
		var x struct {
			Source string
			Shape  struct{ Rows uint64 }
			AllIDs struct{ Path string } `json:"all_ids"`
		}
		must(json.Unmarshal(b, &x))
		p, ok := lookup[x.Source]
		if !ok {
			panic("missing source map")
		}
		p.Begin = end
		p.End = end + x.Shape.Rows
		p.IDs = x.AllIDs.Path
		if p.IDs == "" {
			p.IDs = m[:len(m)-5] + ".ids"
		}
		end = p.End
		ps = append(ps, p)
	}
	if end != c.a["ids"].n || end != 404079112 {
		panic("full Q shape")
	}
	jobs := make(chan part)
	done := make(chan part)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				done <- work(c, p, out)
			}
		}()
	}
	go func() {
		for _, p := range ps {
			jobs <- p
		}
		close(jobs)
		wg.Wait()
		close(done)
	}()
	result := []part{}
	var rows, core, xpac uint64
	for p := range done {
		result = append(result, p)
		rows += p.Rows
		core += p.Core
		xpac += p.XPAC
		if len(result)%200 == 0 {
			fmt.Printf("parts=%d/%d unknown=%d elapsed=%.1fs\n", len(result), len(ps), rows, time.Since(t).Seconds())
		}
	}
	if rows != 277418468 || core != 150377414 || xpac != 127041054 {
		panic(fmt.Sprintf("UNKNOWN mismatch %d/%d/%d", rows, core, xpac))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Index < result[j].Index })
	jsonFile(filepath.Join(out, "manifest.json"), map[string]any{"status": "complete_immutable_unknown_frame", "source_manifest_sha256": "10c1785b4a6f9757823fc6dd005a052716e2c510ea751659cab4bcfba4c35e2a", "source_dimensions": os.Args[1], "rows": rows, "core": core, "xpac": xpac, "parts": result, "schema": "id:u64,ordinal:u64,year:i32,type:string,is_xpac:bool,source_index:i32,raw_row_index:u64", "elapsed_seconds": time.Since(t).Seconds(), "workers": 32, "production_applied": false})
	fmt.Printf("done unknown=%d core=%d xpac=%d seconds=%.2f\n", rows, core, xpac, time.Since(t).Seconds())
}
