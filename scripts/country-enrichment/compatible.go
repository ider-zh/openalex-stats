package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/checkpoint"
)

func outputSchemaSHA() string {
	s := parquet.SchemaOf(&Patch{}).String() + "\x00" + parquet.SchemaOf(&Anchor{}).String() + "\x00" + parquet.SchemaOf(&Unresolved{}).String()
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

type CompatibilityProof struct {
	Status         string `json:"status"`
	OldBuild       string `json:"old_build_sha256"`
	NewBuild       string `json:"new_build_sha256"`
	RuleSHA        string `json:"rules_sha256"`
	SchemaSHA      string `json:"output_schema_sha256"`
	SourceManifest string `json:"source_manifest_sha256"`
	Parts          []struct {
		Index           int    `json:"index"`
		SourceSHA       string `json:"source_sha256"`
		BaselineSHA     string `json:"baseline_sha256"`
		PatchEqual      bool   `json:"patch_bytes_equal"`
		AnchorsEqual    bool   `json:"anchor_bytes_equal"`
		UnresolvedEqual bool   `json:"unresolved_bytes_equal"`
		SourceEqual     bool   `json:"source_bytes_equal"`
		BaselineEqual   bool   `json:"baseline_equal"`
	} `json:"parts"`
}

func (e *Env) checkCompatibilityProof() error {
	if e.cfg.CompatibleProof == "" {
		return fmt.Errorf("compatible reuse requires explicit proof")
	}
	var p CompatibilityProof
	if er := checkpoint.ReadJSON(e.cfg.CompatibleProof, &p); er != nil {
		return er
	}
	if p.Status != "passed" || p.OldBuild != e.cfg.CompatibleBuild || p.NewBuild != e.buildSHA || p.RuleSHA != e.ruleSHA || p.SchemaSHA != e.schemaSHA || p.SourceManifest != e.manifestSHA || len(p.Parts) < 3 {
		return fmt.Errorf("compatibility proof identity/size mismatch")
	}
	seen := map[int]bool{}
	for _, v := range p.Parts {
		if seen[v.Index] || !v.PatchEqual || !v.AnchorsEqual || !v.UnresolvedEqual || !v.SourceEqual || !v.BaselineEqual || len(v.SourceSHA) != 64 || len(v.BaselineSHA) != 64 {
			return fmt.Errorf("incomplete byte-equivalence proof")
		}
		seen[v.Index] = true
	}
	var old struct {
		Build  string              `json:"build_sha256"`
		Rules  string              `json:"rules_sha256"`
		Source checkpoint.Artifact `json:"source_manifest"`
	}
	oldPath := filepath.Join(e.cfg.CompatibleWork, "identity.json")
	if er := checkpoint.ReadJSON(oldPath, &old); er != nil {
		return er
	}
	if old.Build != p.OldBuild || old.Rules != p.RuleSHA || old.Source.SHA256 != e.manifestSHA {
		return fmt.Errorf("legacy workspace identity mismatch")
	}
	for _, path := range []string{e.cfg.CompatibleProof, oldPath} {
		a, er := checkpoint.Hash(path)
		if er != nil {
			return er
		}
		e.compatibilityArtifacts = append(e.compatibilityArtifacts, a)
	}
	return nil
}
func (e *Env) acceptReportBuild(r FileReport) bool {
	if r.BuildSHA == e.buildSHA {
		return r.SchemaSHA == e.schemaSHA
	}
	return e.cfg.CompatibleWork != "" && r.BuildSHA == e.cfg.CompatibleBuild && r.SchemaSHA == e.schemaSHA && strings.HasPrefix(r.CompatibleOrigin.Path, filepath.Join(e.cfg.CompatibleWork, "parts")+string(os.PathSeparator)) && r.CompatibleOrigin.Verify() == nil
}
func verifySchema(path string, want *parquet.Schema) error {
	f, er := os.Open(path)
	if er != nil {
		return er
	}
	defer f.Close()
	s, er := f.Stat()
	if er != nil {
		return er
	}
	pf, er := parquet.OpenFile(f, s.Size(), parquet.SkipPageIndex(true), parquet.SkipBloomFilters(true))
	if er != nil {
		return er
	}
	if pf.Schema().String() != want.String() {
		return fmt.Errorf("incompatible sidecar schema %s", path)
	}
	return nil
}
func (e *Env) importCompatible(ctx context.Context, index int, info os.FileInfo) (out FileReport, reused bool, err error) {
	originPath := filepath.Join(e.cfg.CompatibleWork, "parts", fmt.Sprintf("part-%06d.json", index))
	if er := checkpoint.ReadJSON(originPath, &out); os.IsNotExist(er) {
		return FileReport{}, false, nil
	} else if er != nil {
		return out, false, er
	}
	if out.Version != ruleVersion || out.RuleSHA != e.ruleSHA || out.BuildSHA != e.cfg.CompatibleBuild || out.Source.Path != e.paths[index] || out.Source.Bytes != info.Size() || out.SourceModNS != info.ModTime().UnixNano() || out.RawRows != e.expected[index] || out.BaselineSHA != out.CacheBaselineSHA {
		return out, false, fmt.Errorf("legacy checkpoint semantic/source identity mismatch %d", index)
	}
	if e.cfg.RawReaders <= 0 {
		return out, false, fmt.Errorf("compatible import requires sequential source rehash")
	}
	_, actual, release, er := e.bufferSource(ctx, out.Source.Path, info.Size())
	if er != nil {
		return out, false, er
	}
	release()
	if actual.SHA256 != out.Source.SHA256 {
		return out, false, fmt.Errorf("legacy raw source SHA changed %d", index)
	}
	for _, item := range []struct {
		kind   string
		a      *checkpoint.Artifact
		schema *parquet.Schema
	}{{"patch", &out.Patch, parquet.SchemaOf(&Patch{})}, {"anchors", &out.Anchors, parquet.SchemaOf(&Anchor{})}, {"unresolved", &out.Unresolved, parquet.SchemaOf(&Unresolved{})}} {
		if item.a.Path != filepath.Join(e.cfg.CompatibleWork, item.kind, fmt.Sprintf("part-%06d.parquet", index)) {
			return out, false, fmt.Errorf("legacy sidecar path mismatch")
		}
		if er = item.a.Verify(); er != nil {
			return out, false, er
		}
		if er = verifySchema(item.a.Path, item.schema); er != nil {
			return out, false, er
		}
		newPath := filepath.Join(e.cfg.Work, item.kind, fmt.Sprintf("part-%06d.parquet", index))
		if er = os.MkdirAll(filepath.Dir(newPath), 0755); er != nil {
			return out, false, er
		}
		if _, er = os.Stat(newPath); os.IsNotExist(er) {
			if er = os.Link(item.a.Path, newPath); er != nil {
				return out, false, er
			}
		} else if er != nil {
			return out, false, er
		}
		copy := *item.a
		copy.Path = newPath
		if er = copy.Verify(); er != nil {
			return out, false, er
		}
		*item.a = copy
	}
	out.SchemaSHA = e.schemaSHA
	out.CompatibleOrigin, er = checkpoint.Hash(originPath)
	if er != nil {
		return out, false, er
	}
	if er = checkpoint.JSON(filepath.Join(e.cfg.Work, "parts", fmt.Sprintf("part-%06d.json", index)), out); er != nil {
		return out, false, er
	}
	return out, true, nil
}
