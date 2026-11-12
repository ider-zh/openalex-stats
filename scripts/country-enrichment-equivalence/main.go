package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/checkpoint"
)

type Report struct {
	Version       string              `json:"version"`
	Rules         string              `json:"rules_sha256"`
	Build         string              `json:"build_sha256"`
	Baseline      string              `json:"baseline_sha256"`
	CacheBaseline string              `json:"cache_baseline_sha256"`
	Rows          int64               `json:"raw_rows"`
	Source        checkpoint.Artifact `json:"source"`
	Patch         checkpoint.Artifact `json:"patch"`
	Anchors       checkpoint.Artifact `json:"known_affiliation_anchors"`
	Unresolved    checkpoint.Artifact `json:"unresolved_affiliations"`
	Groups        json.RawMessage     `json:"groups"`
}
type Identity struct {
	Schema string              `json:"output_schema_sha256"`
	Source checkpoint.Artifact `json:"source_manifest"`
	Build  string              `json:"build_sha256"`
	Rules  string              `json:"rules_sha256"`
}
type Result struct {
	Index                                 int    `json:"index"`
	SourceSHA                             string `json:"source_sha256"`
	BaselineSHA                           string `json:"baseline_sha256"`
	PatchEqual                            bool   `json:"patch_bytes_equal"`
	AnchorEqual                           bool   `json:"anchor_bytes_equal"`
	UnresolvedEqual                       bool   `json:"unresolved_bytes_equal"`
	SourceEqual                           bool   `json:"source_bytes_equal"`
	BaselineEqual                         bool   `json:"baseline_equal"`
	GroupsEqual                           bool   `json:"groups_equal"`
	Rows                                  int64  `json:"raw_rows"`
	PatchRows, AnchorRows, UnresolvedRows int64
	Old, New                              string
}

func main() {
	if er := run(); er != nil {
		fmt.Fprintln(os.Stderr, er)
		os.Exit(1)
	}
}
func run() error {
	old := flag.String("old-work", "", "running legacy evaluator directory")
	extra := flag.String("old-extra-work", "", "legacy index1956 benchmark directory")
	newWork := flag.String("new-work", "", "buffered evaluator benchmark")
	out := flag.String("output", "", "proof JSON")
	flag.Parse()
	if *old == "" || *extra == "" || *newWork == "" || *out == "" {
		return fmt.Errorf("all paths required")
	}
	var oi, ni Identity
	if er := checkpoint.ReadJSON(filepath.Join(*old, "identity.json"), &oi); er != nil {
		return er
	}
	if er := checkpoint.ReadJSON(filepath.Join(*newWork, "identity.json"), &ni); er != nil {
		return er
	}
	if oi.Rules != ni.Rules || oi.Source.SHA256 != ni.Source.SHA256 || len(ni.Schema) != 64 {
		return fmt.Errorf("rule/source/schema identity mismatch")
	}
	var results []Result
	for _, i := range []int{0, 268, 1956} {
		oldRoot := *old
		if i == 1956 {
			oldRoot = *extra
		}
		oldPath := filepath.Join(oldRoot, "parts", fmt.Sprintf("part-%06d.json", i))
		newPath := filepath.Join(*newWork, "parts", fmt.Sprintf("part-%06d.json", i))
		var a, b Report
		if er := checkpoint.ReadJSON(oldPath, &a); er != nil {
			return er
		}
		if er := checkpoint.ReadJSON(newPath, &b); er != nil {
			return er
		}
		r := Result{Index: i, SourceSHA: a.Source.SHA256, BaselineSHA: a.Baseline, SourceEqual: a.Source.SHA256 == b.Source.SHA256 && a.Source.Bytes == b.Source.Bytes, BaselineEqual: a.Baseline == b.Baseline && a.Baseline == a.CacheBaseline && b.Baseline == b.CacheBaseline, GroupsEqual: bytes.Equal(a.Groups, b.Groups), Rows: a.Rows, Old: oldPath, New: newPath}
		if a.Rows != b.Rows || a.Version != b.Version || a.Rules != b.Rules || a.Rules != ni.Rules || a.Build != oi.Build || b.Build != ni.Build {
			return fmt.Errorf("part identity mismatch %d", i)
		}
		for k, pair := range [][2]checkpoint.Artifact{{a.Patch, b.Patch}, {a.Anchors, b.Anchors}, {a.Unresolved, b.Unresolved}} {
			if er := pair[0].Verify(); er != nil {
				return er
			}
			if er := pair[1].Verify(); er != nil {
				return er
			}
			equal, er := equalFiles(pair[0].Path, pair[1].Path)
			if er != nil {
				return er
			}
			if pair[0].SHA256 != pair[1].SHA256 || pair[0].Bytes != pair[1].Bytes || !equal {
				return fmt.Errorf("data byte inequality part %d artifact %d", i, k)
			}
			rows, er := parquetRows(pair[0].Path)
			if er != nil {
				return er
			}
			if rows == 0 {
				return fmt.Errorf("representative artifact is empty %d/%d", i, k)
			}
			switch k {
			case 0:
				r.PatchEqual = equal
				r.PatchRows = rows
			case 1:
				r.AnchorEqual = equal
				r.AnchorRows = rows
			case 2:
				r.UnresolvedEqual = equal
				r.UnresolvedRows = rows
			}
		}
		if !r.SourceEqual || !r.BaselineEqual || !r.GroupsEqual {
			return fmt.Errorf("source/baseline/group inequality part %d", i)
		}
		results = append(results, r)
	}
	return checkpoint.JSON(*out, map[string]any{"status": "passed", "old_build_sha256": oi.Build, "new_build_sha256": ni.Build, "rules_sha256": ni.Rules, "output_schema_sha256": ni.Schema, "source_manifest_sha256": ni.Source.SHA256, "parts": results, "comparison": "Complete files streamed and compared byte for byte in Go, with independent SHA256/size verification; all three artifact types non-empty in each representative shard.", "completed_at": time.Now().UTC(), "performance_claim": "No cold-cache speedup claim: byte equivalence was checked while the old full scan was still running."})
}
func equalFiles(a, b string) (bool, error) {
	f, er := os.Open(a)
	if er != nil {
		return false, er
	}
	defer f.Close()
	g, er := os.Open(b)
	if er != nil {
		return false, er
	}
	defer g.Close()
	x, y := make([]byte, 1<<20), make([]byte, 1<<20)
	for {
		n, e := f.Read(x)
		m, d := g.Read(y)
		if n != m || !bytes.Equal(x[:n], y[:m]) {
			return false, nil
		}
		if e == io.EOF && d == io.EOF {
			return true, nil
		}
		if e != nil && e != io.EOF {
			return false, e
		}
		if d != nil && d != io.EOF {
			return false, d
		}
	}
}
func parquetRows(path string) (int64, error) {
	f, er := os.Open(path)
	if er != nil {
		return 0, er
	}
	defer f.Close()
	s, er := f.Stat()
	if er != nil {
		return 0, er
	}
	p, er := parquet.OpenFile(f, s.Size(), parquet.SkipPageIndex(true), parquet.SkipBloomFilters(true))
	if er != nil {
		return 0, er
	}
	return p.NumRows(), nil
}
