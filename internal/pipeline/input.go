package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Manifest struct {
	Date string `json:"date"`
	Meta struct {
		RecordCount   int64 `json:"record_count"`
		ContentLength int64 `json:"content_length"`
	} `json:"meta"`
	Entities []struct {
		Entity      string `json:"entity"`
		RecordCount int64  `json:"record_count"`
		Files       []struct {
			URL  string `json:"url"`
			Meta struct {
				ContentLength int64 `json:"content_length"`
				RecordCount   int64 `json:"record_count"`
			} `json:"meta"`
		} `json:"files"`
	} `json:"entities"`
}
type Input struct {
	Paths          []string `json:"paths"`
	SnapshotDate   string   `json:"snapshot_date,omitempty"`
	ManifestSHA256 string   `json:"manifest_sha256,omitempty"`
	ManifestWorks  int64    `json:"manifest_works,omitempty"`
	ManifestFiles  int      `json:"manifest_files,omitempty"`
	Truncated      bool     `json:"truncated"`
}

func Resolve(manifestPath string, inputs []string, fileLimit int) (Input, error) {
	var out Input
	if fileLimit < 0 {
		return out, fmt.Errorf("file limit cannot be negative")
	}
	if manifestPath != "" && len(inputs) > 0 {
		return out, fmt.Errorf("choose manifest or explicit input files")
	}
	if manifestPath != "" {
		b, err := os.ReadFile(manifestPath)
		if err != nil {
			return out, err
		}
		var m Manifest
		if err = json.Unmarshal(b, &m); err != nil {
			return out, err
		}
		h := sha256.Sum256(b)
		out.ManifestSHA256 = hex.EncodeToString(h[:])
		out.SnapshotDate = m.Date
		for _, entity := range m.Entities {
			if entity.Entity != "works" {
				continue
			}
			out.ManifestWorks = entity.RecordCount
			for _, file := range entity.Files {
				_, rel, ok := strings.Cut(file.URL, "/data/parquet/")
				if !ok {
					return out, fmt.Errorf("unsupported manifest URL %q", file.URL)
				}
				rel = filepath.FromSlash(rel)
				if filepath.IsAbs(rel) || !filepath.IsLocal(rel) {
					return out, fmt.Errorf("unsafe relative manifest path %q", rel)
				}
				out.Paths = append(out.Paths, filepath.Join(filepath.Dir(manifestPath), rel))
			}
		}
	} else {
		out.Paths = append(out.Paths, inputs...)
	}
	if len(out.Paths) == 0 {
		return out, fmt.Errorf("no input work files")
	}
	sort.Strings(out.Paths)
	for i := 1; i < len(out.Paths); i++ {
		if out.Paths[i] == out.Paths[i-1] {
			return out, fmt.Errorf("duplicate input file %q", out.Paths[i])
		}
	}
	out.ManifestFiles = len(out.Paths)
	if manifestPath == "" {
		out.ManifestFiles = 0
	}
	if fileLimit > 0 && len(out.Paths) > fileLimit {
		out.Truncated = true
		out.Paths = out.Paths[:fileLimit]
	}
	return out, nil
}
