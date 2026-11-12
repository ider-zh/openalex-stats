package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
	"openalexstats/internal/checkpoint"
)

type Anchor struct {
	ID             uint64   `parquet:"id"`
	Year           int32    `parquet:"year"`
	XPAC           bool     `parquet:"is_xpac"`
	Country        string   `parquet:"country"`
	RawHash        string   `parquet:"raw_normalized_sha256"`
	Raw            string   `parquet:"raw_affiliation"`
	InstitutionIDs []string `parquet:"institution_ids,list"`
	Group          string   `parquet:"single_institution_group"`
}
type Unresolved struct {
	ID             uint64   `parquet:"id"`
	Year           int32    `parquet:"year"`
	Type           string   `parquet:"type"`
	XPAC           bool     `parquet:"is_xpac"`
	Raw            []string `parquet:"raw_affiliations,list"`
	InstitutionIDs []string `parquet:"institution_ids,list"`
	GeoCountries   []string `parquet:"institution_geo_country_candidates,list"`
	GeoEvidence    string   `parquet:"geo_evidence_json"`
	NameCountries  []string `parquet:"exact_institution_name_candidates,list"`
	NameEvidence   string   `parquet:"name_evidence_json"`
}
type NameRef struct{ ID, Country string }
type hashWriter struct {
	w io.Writer
	h hash.Hash
	n int64
}

func (w *hashWriter) Write(p []byte) (int, error) {
	n, e := w.w.Write(p)
	if n > 0 {
		_, _ = w.h.Write(p[:n])
		w.n += int64(n)
	}
	return n, e
}

type sidecar[T any] struct {
	path string
	f    *os.File
	h    *hashWriter
	p    *parquet.GenericWriter[T]
	rows int64
	done bool
}

func newSidecar[T any](path string) (*sidecar[T], error) {
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return nil, e
	}
	f, e := os.Create(path + ".tmp")
	if e != nil {
		return nil, e
	}
	h := &hashWriter{w: f, h: sha256.New()}
	return &sidecar[T]{path: path, f: f, h: h, p: parquet.NewGenericWriter[T](h, parquet.Compression(&zstd.Codec{}), parquet.MaxRowsPerRowGroup(16384))}, nil
}
func (w *sidecar[T]) write(rows []T) error {
	if len(rows) == 0 {
		return nil
	}
	n, e := w.p.Write(rows)
	w.rows += int64(n)
	return e
}
func (w *sidecar[T]) finish() (checkpoint.Artifact, error) {
	if w.done {
		return checkpoint.Artifact{}, fmt.Errorf("sidecar already finished")
	}
	if e := w.p.Close(); e != nil {
		return checkpoint.Artifact{}, e
	}
	if e := w.f.Sync(); e != nil {
		return checkpoint.Artifact{}, e
	}
	if e := w.f.Close(); e != nil {
		return checkpoint.Artifact{}, e
	}
	if e := os.Rename(w.path+".tmp", w.path); e != nil {
		return checkpoint.Artifact{}, e
	}
	w.done = true
	return checkpoint.Artifact{Path: w.path, Bytes: w.h.n, SHA256: hex.EncodeToString(w.h.h.Sum(nil))}, nil
}
func (w *sidecar[T]) abort() {
	if !w.done {
		_ = w.f.Close()
	}
}
func normalizedRaw(raw string) string { return strings.ToLower(strings.Join(strings.Fields(raw), " ")) }

func (e *Env) extraEvidence(r RawWork, id uint64, year int32, original []string, newlyKnown bool) (anchors []Anchor, unresolved *Unresolved) {
	if len(r.Authors) == 0 {
		return nil, nil
	}
	var raws, ids, geoCountries, nameCountries []string
	var geoEvidence, nameEvidence []Evidence
	seen := map[string]int{}
	for ai, a := range r.Authors {
		if a == nil {
			continue
		}
		var ac, ar, inst []string
		for _, c := range a.Countries {
			ac = addUnique(ac, str(c))
		}
		for _, s := range a.Raw {
			if hasText(str(s)) {
				ar = addUnique(ar, str(s))
			}
		}
		for _, in := range a.Institutions {
			if in != nil && str(in.ID) != "" {
				inst = addUnique(inst, str(in.ID))
			}
		}
		for _, af := range a.Affiliations {
			if af == nil {
				continue
			}
			if hasText(str(af.Raw)) {
				ar = addUnique(ar, str(af.Raw))
			}
			for _, ip := range af.IDs {
				inst = addUnique(inst, str(ip))
			}
		}
		inst = uniqueSorted(inst)
		for _, v := range inst {
			ids = addUnique(ids, v)
			for _, country := range e.institutionGeo[v] {
				geoCountries = addUnique(geoCountries, country)
				geoEvidence = append(geoEvidence, Evidence{Author: ai, Source: "institution_geo_country_code_review_v1b", Country: country, InstitutionID: v, Decision: "review_only", Reason: "same_snapshot_institution_geo_country_code_not_applied_in_v1"})
			}
		}
		for _, raw := range ar {
			raws = addUnique(raws, raw)
			norm := normalizedRaw(raw)
			if len(original) == 0 && !newlyKnown {
				for _, ref := range e.institutionNames[norm] {
					nameCountries = addUnique(nameCountries, ref.Country)
					nameEvidence = append(nameEvidence, Evidence{Author: ai, Source: "exact_snapshot_institution_name_review_v1b", Country: ref.Country, InstitutionID: ref.ID, Raw: raw, Decision: "review_only", Reason: "institution_brand_country_may_differ_from_campus_country"})
				}
			}
			if len(ac) == 1 && e.rules.valid[ac[0]] {
				sum := sha256.Sum256([]byte(norm))
				key := hex.EncodeToString(sum[:])
				dedup := ac[0] + "/" + key
				if prior, ok := seen[dedup]; ok {
					for _, iid := range inst {
						anchors[prior].InstitutionIDs = addUnique(anchors[prior].InstitutionIDs, iid)
					}
					anchors[prior].InstitutionIDs = uniqueSorted(anchors[prior].InstitutionIDs)
					anchors[prior].Group = ""
					if len(anchors[prior].InstitutionIDs) == 1 {
						anchors[prior].Group = anchors[prior].InstitutionIDs[0]
					}
					continue
				}
				seen[dedup] = len(anchors)
				group := ""
				if len(inst) == 1 {
					group = inst[0]
				}
				anchors = append(anchors, Anchor{ID: id, Year: year, XPAC: truth(r.XPAC), Country: ac[0], RawHash: key, Raw: raw, InstitutionIDs: inst, Group: group})
			}
		}
	}
	if len(original) == 0 && !newlyKnown && (len(raws) > 0 || len(ids) > 0) {
		b, _ := json.Marshal(geoEvidence)
		nb, _ := json.Marshal(nameEvidence)
		unresolved = &Unresolved{ID: id, Year: year, Type: str(r.Type), XPAC: truth(r.XPAC), Raw: raws, InstitutionIDs: uniqueSorted(ids), GeoCountries: uniqueSorted(geoCountries), GeoEvidence: string(b), NameCountries: uniqueSorted(nameCountries), NameEvidence: string(nb)}
	}
	return
}
