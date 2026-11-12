// Independent-paper audit sampler. Identity keys only; never inspect countries or gold.
package main

import (
	"bufio"
	"container/heap"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

const seed = "openalex-country-enrichment-v3-20261112-route-candidate-audit-01"

func must(e error) {
	if e != nil {
		panic(e)
	}
}
func rawID(row map[string]json.RawMessage) uint64 {
	for _, k := range []string{"id", "work_id"} {
		v := row[k]
		var id uint64
		if json.Unmarshal(v, &id) == nil && id > 0 {
			return id
		}
		var s string
		if json.Unmarshal(v, &s) == nil {
			s = strings.TrimPrefix(strings.TrimPrefix(s, "https://openalex.org/"), "W")
			id, e := strconv.ParseUint(s, 10, 64)
			if e == nil && id > 0 {
				return id
			}
		}
	}
	panic("missing WorkID")
}
func stringValue(v json.RawMessage) string {
	var s string
	if json.Unmarshal(v, &s) == nil {
		return strings.TrimSpace(s)
	}
	var n uint64
	if json.Unmarshal(v, &n) == nil {
		return strconv.FormatUint(n, 10)
	}
	return ""
}
func normalized(k, v string) string {
	v = strings.TrimSpace(v)
	switch k {
	case "doi":
		v = strings.ToLower(v)
		for _, p := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/", "doi:"} {
			v = strings.TrimPrefix(v, p)
		}
		if !strings.HasPrefix(v, "10.") || !strings.Contains(v, "/") {
			return ""
		}
	case "pmid":
		for _, r := range v {
			if r < '0' || r > '9' {
				return ""
			}
		}
	case "pmcid":
		v = strings.ToUpper(v)
		if !strings.HasPrefix(v, "PMC") {
			return ""
		}
		for _, r := range strings.TrimPrefix(v, "PMC") {
			if r < '0' || r > '9' {
				return ""
			}
		}
		if v == "PMC" {
			return ""
		}
	}
	if v == "" {
		return ""
	}
	return k + ":" + v
}
func keysOf(row map[string]json.RawMessage) []string {
	ks := map[string]bool{}
	add := func(m map[string]json.RawMessage, uppercase bool) {
		for _, k := range []string{"doi", "pmid", "pmcid"} {
			name := k
			if uppercase {
				name = strings.ToUpper(k)
			}
			v := normalized(k, stringValue(m[name]))
			if v != "" {
				ks[v] = true
			}
		}
	}
	add(row, false)
	if b := row["original_work"]; len(b) > 0 {
		var ow map[string]json.RawMessage
		must(json.Unmarshal(b, &ow))
		add(ow, false)
	}
	// Metadata engine retains accepted PubMed source keys in private raw records.
	// Only identity-passed, eligible records contribute keys. Countries are ignored.
	if b := row["evidence_records"]; len(b) > 0 {
		var records []struct {
			Eligible bool `json:"eligible"`
			Identity struct {
				Pass bool `json:"pass"`
			} `json:"identity"`
			Evidence struct {
				Private string `json:"private_record_json"`
			} `json:"evidence"`
		}
		must(json.Unmarshal(b, &records))
		for _, r := range records {
			if !r.Eligible || !r.Identity.Pass || r.Evidence.Private == "" {
				continue
			}
			var pm map[string]json.RawMessage
			must(json.Unmarshal([]byte(r.Evidence.Private), &pm))
			add(pm, true)
		}
	}
	out := make([]string, 0, len(ks))
	for k := range ks {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

type node struct {
	ID     uint64
	Parent int
	Keys   []string
}
type dsu struct {
	Nodes    []node
	KeyOwner map[string]int
}

func (d *dsu) find(x int) int {
	if d.Nodes[x].Parent != x {
		d.Nodes[x].Parent = d.find(d.Nodes[x].Parent)
	}
	return d.Nodes[x].Parent
}
func (d *dsu) join(a, b int) {
	a = d.find(a)
	b = d.find(b)
	if a == b {
		return
	}
	if d.Nodes[a].ID > d.Nodes[b].ID {
		a, b = b, a
	}
	d.Nodes[b].Parent = a
}
func (d *dsu) add(id uint64, keys []string) {
	i := len(d.Nodes)
	d.Nodes = append(d.Nodes, node{ID: id, Parent: i, Keys: keys})
	for _, k := range keys {
		if old, ok := d.KeyOwner[k]; ok {
			d.join(i, old)
		} else {
			d.KeyOwner[k] = i
		}
	}
}

type chosen struct {
	Root int
	ID   uint64
	Hash [32]byte
}
type maximum []chosen

func (h maximum) Len() int { return len(h) }
func (h maximum) Less(i, j int) bool {
	return string(h[i].Hash[:]) > string(h[j].Hash[:]) || (h[i].Hash == h[j].Hash && h[i].ID > h[j].ID)
}
func (h maximum) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *maximum) Push(x any)   { *h = append(*h, x.(chosen)) }
func (h *maximum) Pop() any     { a := *h; x := a[len(a)-1]; *h = a[:len(a)-1]; return x }
func loadIDs(path string) (map[uint64]bool, string) {
	out := map[uint64]bool{}
	if path == "-" {
		return out, ""
	}
	b, e := os.ReadFile(path)
	must(e)
	var x struct {
		IDs []uint64 `json:"ids"`
	}
	must(json.Unmarshal(b, &x))
	for _, id := range x.IDs {
		out[id] = true
	}
	s := sha256.Sum256(b)
	return out, hex.EncodeToString(s[:])
}
func scan(path string, fn func([]byte)) string {
	f, e := os.Open(path)
	must(e)
	defer f.Close()
	h := sha256.New()
	s := bufio.NewScanner(io.TeeReader(f, h))
	s.Buffer(make([]byte, 1<<20), 64<<20)
	for s.Scan() {
		b := s.Bytes()
		if strings.TrimSpace(string(b)) != "" {
			fn(b)
		}
	}
	must(s.Err())
	return hex.EncodeToString(h.Sum(nil))
}
func main() {
	if len(os.Args) != 7 {
		panic("usage: select_clusters candidates.jsonl route work-exclusions.json development-paper-keys.json recovered-ids-or-dash output.json")
	}
	input, route, excludedPath, paperPath, recoveredPath, output := os.Args[1], os.Args[2], os.Args[3], os.Args[4], os.Args[5], os.Args[6]
	excluded, excludedSHA := loadIDs(excludedPath)
	recovered, recoveredSHA := loadIDs(recoveredPath)
	if strings.HasSuffix(route, "/net_new") && recoveredPath == "-" {
		panic("net_new requires frozen accepted-recovery IDs")
	}
	pb, e := os.ReadFile(paperPath)
	must(e)
	var ledger struct {
		DOIs   []string `json:"dois"`
		PMIDs  []string `json:"pmids"`
		PMCIDs []string `json:"pmcids"`
	}
	must(json.Unmarshal(pb, &ledger))
	paper := map[string]bool{}
	for _, v := range ledger.DOIs {
		paper[normalized("doi", v)] = true
	}
	for _, v := range ledger.PMIDs {
		paper[normalized("pmid", v)] = true
	}
	for _, v := range ledger.PMCIDs {
		paper[normalized("pmcid", v)] = true
	}
	delete(paper, "")
	d := dsu{KeyOwner: map[string]int{}}
	seen := map[uint64][32]byte{}
	lines, dups, workEx, paperEx, recoveredEx, noKeys := 0, 0, 0, 0, 0, 0
	inputSHA := scan(input, func(raw []byte) {
		lines++
		var row map[string]json.RawMessage
		must(json.Unmarshal(raw, &row))
		id := rawID(row)
		canonical, e := json.Marshal(row)
		must(e)
		sh := sha256.Sum256(canonical)
		if old, ok := seen[id]; ok {
			if old != sh {
				panic(fmt.Sprintf("conflicting duplicate Work %d", id))
			}
			dups++
			return
		}
		seen[id] = sh
		if excluded[id] {
			workEx++
			return
		}
		keys := keysOf(row)
		for _, k := range keys {
			if paper[k] {
				paperEx++
				return
			}
		}
		if recovered[id] {
			recoveredEx++
			return
		}
		if len(keys) == 0 {
			noKeys++
		}
		d.add(id, keys)
	})
	clusters := map[int][]int{}
	for i := range d.Nodes {
		r := d.find(i)
		clusters[r] = append(clusters[r], i)
	}
	h := maximum{}
	multiplicity := map[int]int{}
	for root, members := range clusters {
		multiplicity[len(members)]++
		id := d.Nodes[root].ID
		hh := sha256.Sum256([]byte(seed + "\n" + route + "\n" + strconv.FormatUint(id, 10)))
		x := chosen{root, id, hh}
		if len(h) < 128 {
			heap.Push(&h, x)
		} else if string(hh[:]) < string(h[0].Hash[:]) || (hh == h[0].Hash && id < h[0].ID) {
			h[0] = x
			heap.Fix(&h, 0)
		}
	}
	sort.Slice(h, func(i, j int) bool {
		return string(h[i].Hash[:]) < string(h[j].Hash[:]) || (h[i].Hash == h[j].Hash && h[i].ID < h[j].ID)
	})
	memberToCluster := map[uint64]uint64{}
	clusterRows := map[uint64][]map[string]any{}
	for _, x := range h {
		for _, i := range clusters[x.Root] {
			memberToCluster[d.Nodes[i].ID] = x.ID
		}
	}
	secondSHA := scan(input, func(raw []byte) {
		var row map[string]json.RawMessage
		must(json.Unmarshal(raw, &row))
		id := rawID(row)
		cid, ok := memberToCluster[id]
		if !ok {
			return
		}
		for _, old := range clusterRows[cid] {
			if old["id"] == id {
				return
			}
		}
		clusterRows[cid] = append(clusterRows[cid], map[string]any{"id": id, "candidate": json.RawMessage(append([]byte(nil), raw...))})
	})
	if secondSHA != inputSHA {
		panic("candidate stream changed between passes")
	}
	records := []map[string]any{}
	selectedWorks := 0
	for _, x := range h {
		members := clusterRows[x.ID]
		sort.Slice(members, func(i, j int) bool { return members[i]["id"].(uint64) < members[j]["id"].(uint64) })
		allKeys := map[string]bool{}
		for _, i := range clusters[x.Root] {
			for _, k := range d.Nodes[i].Keys {
				allKeys[k] = true
			}
		}
		keys := make([]string, 0, len(allKeys))
		for k := range allKeys {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		selectedWorks += len(members)
		records = append(records, map[string]any{"cluster_id": x.ID, "route": route, "audit_hash_sha256": hex.EncodeToString(x.Hash[:]), "paper_keys": keys, "member_n": len(members), "members": members})
	}
	pi := 0.
	if len(clusters) > 0 {
		pi = float64(len(h)) / float64(len(clusters))
	}
	out := map[string]any{"status": "frozen_independent_paper_cluster_audit_sample", "protocol": "protocol-r3.json", "route": route, "seed": seed, "hash": "SHA256(seed+newline+route+newline+decimal minimum WorkID per connected component)", "input_jsonl": input, "input_sha256": inputSHA, "input_lines": lines, "unique_input_work_ids": len(seen), "identical_duplicate_rows_ignored": dups, "excluded_development_work_ids": workEx, "excluded_development_paper_keys": paperEx, "excluded_prior_recovered_work_ids": recoveredEx, "eligible_work_frame_N": len(d.Nodes), "eligible_independent_cluster_N": len(clusters), "cluster_member_count_distribution": multiplicity, "works_without_available_paper_keys": noKeys, "sample_cluster_n": len(h), "sample_member_work_n": selectedWorks, "cluster_inclusion_probability": pi, "insufficient_n": len(h) < 128, "records": records, "exclusion_work_sha256": excludedSHA, "paper_ledger_sha256": fmt.Sprintf("%x", sha256.Sum256(pb)), "recovered_ids_sha256": recoveredSHA, "selection_uses_country_or_gold": false, "no_failed_review_replacement": true, "all_selected_component_members_must_be_reviewed": true, "production_applied": false}
	b, e := json.MarshalIndent(out, "", "  ")
	must(e)
	must(os.WriteFile(output, append(b, '\n'), 0600))
	fmt.Printf("route=%s eligibleWork=%d eligibleCluster=%d selectedCluster=%d selectedWork=%d\n", route, len(d.Nodes), len(clusters), len(h), selectedWorks)
}
