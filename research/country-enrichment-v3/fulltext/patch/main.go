// Emit an immutable, dual-gated, UNKNOWN-only review sidecar. Never apply it.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
)

const frozenSHA = "09312f219af3980328199a6cad05386a71ad8b957e505cb847ae6eefae1c28a1"

type gate struct {
	Status          string  `json:"status"`
	Route           string  `json:"route"`
	Clusters        int     `json:"audited_clusters"`
	IndependentPath string  `json:"independent_gate_path"`
	IndependentSHA  string  `json:"independent_gate_sha256"`
	Precision       float64 `json:"precision"`
	WilsonLower     float64 `json:"wilson_lower"`
	WorkPrecision   float64 `json:"work_precision"`
}
type candidate struct {
	ID              uint64   `json:"id"`
	Year            *int32   `json:"year"`
	Type            *string  `json:"type"`
	XPAC            *bool    `json:"is_xpac"`
	SourceIndex     *int32   `json:"source_index"`
	RawRowIndex     *int64   `json:"raw_row_index"`
	Countries       []string `json:"proposed_countries"`
	Original        []string `json:"original_countries"`
	Route           string   `json:"source_route"`
	Status          string   `json:"status"`
	Identity        bool     `json:"identity_pass"`
	RuleSHA         string   `json:"rule_sha256"`
	Evidence        string   `json:"evidence_path"`
	VisibleComplete string   `json:"source_visible_country_completeness"`
	Development     bool     `json:"development_member"`
	Source          struct {
		URL string `json:"url"`
		SHA string `json:"sha256"`
	} `json:"source"`
}
type row struct {
	ID              uint64   `parquet:"id"`
	Year            int32    `parquet:"year"`
	Type            string   `parquet:"type"`
	XPAC            bool     `parquet:"is_xpac"`
	SourceIndex     int32    `parquet:"source_index"`
	RawRowIndex     int64    `parquet:"raw_row_index"`
	Countries       []string `parquet:"proposed_countries,list"`
	Original        []string `parquet:"original_countries,list"`
	Route           string   `parquet:"source_route"`
	RuleSHA         string   `parquet:"rule_sha256"`
	Evidence        string   `parquet:"evidence_path"`
	EvidenceSHA     string   `parquet:"evidence_sha256"`
	GateSHA         string   `parquet:"gate_sha"`
	OverallGateSHA  string   `parquet:"overall_gate_sha256"`
	NetGateSHA      string   `parquet:"net_gate_sha256"`
	Complete        string   `parquet:"country_set_complete"`
	VisibleComplete string   `parquet:"source_visible_country_completeness"`
	Development     bool     `parquet:"development_member"`
	SourceURL       string   `parquet:"source_url"`
	SourceSHA       string   `parquet:"source_sha256"`
}

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func digest(p string) string {
	f, e := os.Open(p)
	must(e)
	defer f.Close()
	h := sha256.New()
	_, e = io.Copy(h, f)
	must(e)
	return hex.EncodeToString(h.Sum(nil))
}
func read(p string, v any) { b, e := os.ReadFile(p); must(e); must(json.Unmarshal(b, v)) }
func checkGate(p, route string) string {
	var g gate
	read(p, &g)
	if g.Status != "passed" || g.Route != route || g.Clusters < 128 || g.Precision < .99 || g.Precision > 1 || g.WilsonLower < .97 || g.WilsonLower > g.Precision || g.WorkPrecision < .99 || g.WorkPrecision > 1 || digest(g.IndependentPath) != g.IndependentSHA {
		panic("missing genuine independent passed gate: " + route)
	}
	return digest(p)
}
func main() {
	manifest := flag.String("manifest", "", "completed full application manifest")
	overall := flag.String("overall-gate", "", "Root overall passed wrapper")
	net := flag.String("net-gate", "", "Root net-domain passed wrapper")
	output := flag.String("out", "", "review-only output parquet")
	flag.Parse()
	if *manifest == "" || *overall == "" || *net == "" || *output == "" {
		panic("manifest, overall-gate, net-gate and out are required")
	}
	overallSHA, netSHA := checkGate(*overall, "pmc_jats"), checkGate(*net, "pmc_jats/net_new")
	var m struct {
		RuleSHA   string `json:"policy_sha256"`
		Scheduled int    `json:"scheduled_rows"`
		Batches   []struct {
			Path  string `json:"path"`
			Works int    `json:"works"`
		} `json:"batches"`
	}
	read(*manifest, &m)
	if m.RuleSHA != frozenSHA || len(m.Batches) == 0 {
		panic("wrong/empty application scope")
	}
	seen := map[uint64]bool{}
	rows := []row{}
	inputs := []map[string]any{}
	totalOutcomes, quarantined := 0, 0
	for _, batch := range m.Batches {
		var acceptance struct {
			Total      int    `json:"total_operational_outcomes"`
			Candidates int    `json:"automatic_positive_candidates"`
			SHA        string `json:"candidate_stream_sha256"`
		}
		read(filepath.Join(batch.Path, "export/export-acceptance.json"), &acceptance)
		if acceptance.Total != batch.Works {
			panic("partial operational batch cannot become full patch")
		}
		totalOutcomes += acceptance.Total
		p := filepath.Join(batch.Path, "export/automatic-candidates.jsonl")
		if digest(p) != acceptance.SHA {
			panic("candidate stream digest drift")
		}
		f, e := os.Open(p)
		must(e)
		decoder := json.NewDecoder(f)
		n := 0
		for {
			var c candidate
			e = decoder.Decode(&c)
			if e == io.EOF {
				break
			}
			must(e)
			n++
			if c.ID == 0 || seen[c.ID] || c.Route != "pmc_jats" || c.Status != "automatic_country_candidate" || !c.Identity || c.RuleSHA != frozenSHA || len(c.Countries) == 0 || len(c.Original) != 0 || c.Year == nil || c.Type == nil || c.XPAC == nil || c.SourceIndex == nil || c.RawRowIndex == nil || c.Evidence == "" {
				panic(fmt.Sprintf("invalid or duplicate auto candidate W%d", c.ID))
			}
			seen[c.ID] = true
			// Absolute, previously established original-Work identity quarantines.
			if c.ID == 19239315 || c.ID == 26184156 || c.ID == 34102155 {
				quarantined++
				continue
			}
			sort.Strings(c.Countries)
			for i, code := range c.Countries {
				if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' || (i > 0 && code == c.Countries[i-1]) {
					panic("invalid country token")
				}
			}
			rows = append(rows, row{c.ID, *c.Year, *c.Type, *c.XPAC, *c.SourceIndex, *c.RawRowIndex, c.Countries, []string{}, c.Route, c.RuleSHA, c.Evidence, digest(c.Evidence), netSHA, overallSHA, netSHA, "unknown", c.VisibleComplete, c.Development, c.Source.URL, c.Source.SHA})
		}
		must(f.Close())
		if n != acceptance.Candidates {
			panic("candidate count mismatch")
		}
		inputs = append(inputs, map[string]any{"path": p, "sha256": acceptance.SHA, "operational_rows": acceptance.Total, "automatic_rows": n})
	}
	if totalOutcomes != m.Scheduled {
		panic("full application scope incomplete")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	must(os.MkdirAll(filepath.Dir(*output), 0700))
	f, e := os.Create(*output + ".tmp")
	must(e)
	w := parquet.NewGenericWriter[row](f, parquet.Compression(&zstd.Codec{Level: zstd.SpeedFastest}), parquet.MaxRowsPerRowGroup(65536))
	_, e = w.Write(rows)
	must(e)
	must(w.Close())
	must(f.Close())
	must(os.Rename(*output+".tmp", *output))
	result := map[string]any{"status": "complete_dual_gated_review_sidecar_not_applied", "rows": len(rows), "operational_outcomes": totalOutcomes, "quarantined_auto_candidates": quarantined, "patch_path": *output, "patch_sha256": digest(*output), "application_manifest_sha256": digest(*manifest), "overall_gate_path": *overall, "overall_gate_sha256": overallSHA, "net_gate_path": *net, "net_gate_sha256": netSHA, "rule_sha256": frozenSHA, "country_set_complete": "unknown", "production_applied": false, "inputs": inputs}
	b, e := json.MarshalIndent(result, "", "  ")
	must(e)
	must(os.WriteFile(*output+".json", append(b, '\n'), 0600))
	fmt.Printf("Review PMC patch rows=%d from complete operational scope=%d; not applied\n", len(rows), totalOutcomes)
}
