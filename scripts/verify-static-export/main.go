// Independent transport audit: stream original Parquet counts, decode OAQ1
// without using the exporter, and compare every model score and global rank.
package main

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/parquet-go/parquet-go"
)

type label struct {
	ID string `json:"id"`
}
type layer struct {
	Scheme string  `json:"scheme"`
	Level  string  `json:"level"`
	Labels []label `json:"labels"`
}
type metadata struct {
	Countries []label `json:"countries"`
	Layers    []layer `json:"layers"`
	Scenarios []label `json:"scenarios"`
	Start     int     `json:"year_start"`
	End       int     `json:"year_end"`
	Main      string  `json:"eci_scenario_id"`
}
type dataset struct {
	Prefix        string            `json:"prefix"`
	RunID         string            `json:"run_id"`
	CompletionSHA string            `json:"completion_sha256"`
	Meta          metadata          `json:"meta"`
	Models        map[string]string `json:"models"`
}
type site struct {
	Datasets map[string]dataset `json:"datasets"`
}
type artifact struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	SHA   string `json:"sha256"`
}
type sourceRow struct {
	Scenario     string  `parquet:"scenario_id"`
	Country      string  `parquet:"country"`
	Year         int     `parquet:"year"`
	Scheme       string  `parquet:"scheme"`
	Level        string  `parquet:"level"`
	Discipline   string  `parquet:"discipline_id"`
	Publications float64 `parquet:"publications"`
	Citations    float64 `parquet:"citations"`
}
type matrix struct {
	bytes                   []byte
	start, years, countries int
}
type summary struct {
	SourceRows        int `json:"source_rows"`
	NonzeroSource     int `json:"nonzero_source_cells"`
	NonzeroMatrix     int `json:"nonzero_matrix_cells"`
	CountDifferences  int `json:"count_bit_differences"`
	MarginDifferences int `json:"margin_bit_differences"`
	Models            int `json:"source_models"`
	ModelPoints       int `json:"model_points"`
	ModelDifferences  int `json:"model_score_status_rank_differences"`
}
type verifier struct {
	root     string
	matrices map[string]*matrix
	models   map[string]modelShard
}
type modelShard struct {
	Countries   [][][]json.RawMessage `json:"countries"`
	Disciplines [][][]json.RawMessage `json:"disciplines"`
}
type scores struct {
	Status            string    `json:"status"`
	Countries         []string  `json:"countries"`
	Disciplines       []string  `json:"disciplines"`
	ECI               []float64 `json:"eci"`
	PCI               []float64 `json:"pci"`
	ActiveCountries   []int     `json:"active_countries"`
	ActiveDisciplines []int     `json:"active_disciplines"`
	Unstable          bool      `json:"unstable"`
}
type sourceModel struct {
	Scenario string `json:"scenario_id"`
	Year     int    `json:"year"`
	Scheme   string `json:"scheme"`
	Level    string `json:"level"`
	Metric   string `json:"metric"`
	Result   scores `json:"result"`
}

func readJSON(path string, target any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, target)
}
func hashFile(path string) (string, int64, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", 0, e
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, e
}
func (v *verifier) path(name string) (string, error) {
	name = strings.TrimPrefix(name, "/")
	p := filepath.Join(v.root, filepath.FromSlash(name))
	rel, e := filepath.Rel(v.root, p)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe manifest path")
	}
	return p, nil
}
func (v *verifier) gunzip(name string) ([]byte, error) {
	p, e := v.path(name)
	if e != nil {
		return nil, e
	}
	f, e := os.Open(p)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	r, e := gzip.NewReader(f)
	if e != nil {
		return nil, e
	}
	defer r.Close()
	return io.ReadAll(r)
}
func (v *verifier) matrix(name string) (*matrix, error) {
	if m := v.matrices[name]; m != nil {
		return m, nil
	}
	b, e := v.gunzip(name)
	if e != nil {
		return nil, e
	}
	if len(b) < 16 || string(b[:4]) != "OAQ1" {
		return nil, fmt.Errorf("invalid matrix header: %s", name)
	}
	m := &matrix{b, int(binary.LittleEndian.Uint16(b[4:])), int(binary.LittleEndian.Uint16(b[6:])), int(binary.LittleEndian.Uint16(b[8:]))}
	if len(b) != 16+(m.countries+1)*m.years*16 {
		return nil, fmt.Errorf("matrix length mismatch")
	}
	v.matrices[name] = m
	return m, nil
}
func (m *matrix) value(country, year, basis int) float64 {
	return math.Float64frombits(binary.LittleEndian.Uint64(m.bytes[16+m.years*16+(country*m.years+year)*16+basis*8:]))
}
func (m *matrix) total(year, basis int) float64 {
	return math.Float64frombits(binary.LittleEndian.Uint64(m.bytes[16+year*16+basis*8:]))
}
func equal(a, b float64) bool { return math.Float64bits(a) == math.Float64bits(b) }

type dimension struct {
	scenario string
	layer    layer
	path     string
	ids      []string
	ord      map[string]int
	col      [][][2]float64
	country  [][][2]float64
	total    [][2]float64
}

func auditCounts(v *verifier, d dataset, input string, out *summary) error {
	years := d.Meta.End - d.Meta.Start + 1
	countries := map[string]int{}
	for i, c := range d.Meta.Countries {
		countries[c.ID] = i
	}
	countries["UNKNOWN"] = len(countries)
	dims := map[string]*dimension{}
	for si, s := range d.Meta.Scenarios {
		for li, l := range d.Meta.Layers {
			prefix := fmt.Sprintf("%s/counts/%d/%d", d.Prefix, si, li)
			g := &dimension{scenario: s.ID, layer: l, path: prefix, ord: map[string]int{}, country: make([][][2]float64, len(countries)), total: make([][2]float64, years)}
			for _, id := range l.Labels {
				g.ord[id.ID] = len(g.ids)
				g.ids = append(g.ids, id.ID)
			}
			g.ord["UNKNOWN"] = len(g.ids)
			g.ids = append(g.ids, "UNKNOWN")
			g.col = make([][][2]float64, len(g.ids))
			for i := range g.col {
				g.col[i] = make([][2]float64, years)
			}
			for i := range g.country {
				g.country[i] = make([][2]float64, years)
			}
			dims[s.ID+"/"+l.Scheme+"/"+l.Level] = g
		}
	}
	f, e := os.Open(filepath.Join(input, "statistics.parquet"))
	if e != nil {
		return e
	}
	defer f.Close()
	r := parquet.NewGenericReader[sourceRow](f)
	defer r.Close()
	batch := make([]sourceRow, 4096)
	for {
		n, readErr := r.Read(batch)
		for _, row := range batch[:n] {
			out.SourceRows++
			g := dims[row.Scenario+"/"+row.Scheme+"/"+row.Level]
			if g == nil {
				return fmt.Errorf("source dimension outside static catalog")
			}
			ci, ok := countries[row.Country]
			if !ok {
				return fmt.Errorf("source country outside static catalog")
			}
			di, ok := g.ord[row.Discipline]
			if !ok {
				return fmt.Errorf("source discipline outside static catalog")
			}
			yi := row.Year - d.Meta.Start
			if yi < 0 || yi >= years {
				return fmt.Errorf("source year outside static catalog")
			}
			m, e := v.matrix(fmt.Sprintf("%s/%d.bin.gz", g.path, di))
			if e != nil {
				return e
			}
			if m.start != d.Meta.Start || m.years != years || m.countries != len(countries) {
				return fmt.Errorf("matrix dimensions differ from source catalog")
			}
			if !equal(row.Publications, m.value(ci, yi, 0)) || !equal(row.Citations, m.value(ci, yi, 1)) {
				out.CountDifferences++
			}
			if row.Publications != 0 || row.Citations != 0 {
				out.NonzeroSource++
			}
			if row.Country != "UNKNOWN" && row.Discipline != "UNKNOWN" {
				g.col[di][yi][0] += row.Publications
				g.col[di][yi][1] += row.Citations
				g.country[ci][yi][0] += row.Publications
				g.country[ci][yi][1] += row.Citations
				g.total[yi][0] += row.Publications
				g.total[yi][1] += row.Citations
			}
		}
		clear(batch)
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	for _, g := range dims {
		margins, e := v.matrix(g.path + "/margins.bin.gz")
		if e != nil {
			return e
		}
		for yi := 0; yi < years; yi++ {
			for b := 0; b < 2; b++ {
				if !equal(margins.total(yi, b), g.total[yi][b]) {
					out.MarginDifferences++
				}
			}
		}
		for ci := 0; ci < len(countries); ci++ {
			for yi := 0; yi < years; yi++ {
				for b := 0; b < 2; b++ {
					if !equal(margins.value(ci, yi, b), g.country[ci][yi][b]) {
						out.MarginDifferences++
					}
				}
			}
		}
		for di := range g.ids {
			m, e := v.matrix(fmt.Sprintf("%s/%d.bin.gz", g.path, di))
			if e != nil {
				return e
			}
			for yi := 0; yi < years; yi++ {
				for b := 0; b < 2; b++ {
					if !equal(m.total(yi, b), g.col[di][yi][b]) {
						out.MarginDifferences++
					}
				}
			}
			for ci := 0; ci < len(countries); ci++ {
				for yi := 0; yi < years; yi++ {
					if m.value(ci, yi, 0) != 0 || m.value(ci, yi, 1) != 0 {
						out.NonzeroMatrix++
					}
				}
			}
		}
	}
	if out.CountDifferences != 0 || out.MarginDifferences != 0 || out.NonzeroSource != out.NonzeroMatrix {
		return fmt.Errorf("count/margin transport audit failed")
	}
	return nil
}

type rankedScore struct {
	value float64
	rank  int
}

func ranked(labels []string, active []int, values []float64) (map[string]rankedScore, int, error) {
	ids := make([]int, 0, len(active))
	for i, a := range active {
		if a < 0 || a >= len(labels) || i >= len(values) || math.IsNaN(values[i]) || math.IsInf(values[i], 0) {
			return nil, 0, fmt.Errorf("invalid source score index")
		}
		ids = append(ids, i)
	}
	sort.Slice(ids, func(i, j int) bool { return values[ids[i]] > values[ids[j]] })
	result := map[string]rankedScore{}
	rank := 1
	for i, index := range ids {
		if i > 0 && values[index] != values[ids[i-1]] {
			rank = i + 1
		}
		result[labels[active[index]]] = rankedScore{values[index], rank}
	}
	return result, len(ids), nil
}
func checkPoint(point []json.RawMessage, status string, value *float64, rank *int, universe int) bool {
	if len(point) != 4 {
		return false
	}
	var gotStatus string
	var gotValue *float64
	var gotRank *int
	var gotUniverse int
	if json.Unmarshal(point[0], &gotValue) != nil || json.Unmarshal(point[1], &gotStatus) != nil || json.Unmarshal(point[2], &gotRank) != nil || json.Unmarshal(point[3], &gotUniverse) != nil {
		return false
	}
	if gotStatus != status || gotUniverse != universe || (gotValue == nil) != (value == nil) || (gotRank == nil) != (rank == nil) {
		return false
	}
	return (value == nil || equal(*gotValue, *value)) && (rank == nil || *gotRank == *rank)
}
func auditModels(v *verifier, d dataset, method, input string, out *summary) error {
	var rows []sourceModel
	if e := readJSON(input, &rows); e != nil {
		return e
	}
	layers := map[string]layer{}
	for _, l := range d.Meta.Layers {
		layers[l.Scheme+"/"+l.Level] = l
	}
	for _, row := range rows {
		out.Models++
		scenario := row.Scenario
		if scenario == "" {
			scenario = d.Meta.Main
		}
		key := strings.Join([]string{method, scenario, row.Scheme, row.Level, row.Metric}, "/")
		name, ok := d.Models[key]
		if !ok {
			return fmt.Errorf("missing method/layer static model: %s", key)
		}
		shard, ok := v.models[name]
		if !ok {
			b, e := v.gunzip(name)
			if e != nil {
				return e
			}
			if e = json.Unmarshal(b, &shard); e != nil {
				return e
			}
			v.models[name] = shard
		}
		yi := row.Year - d.Meta.Start
		for _, side := range []struct {
			labels       []label
			points       [][][]json.RawMessage
			sourceLabels []string
			active       []int
			values       []float64
		}{{d.Meta.Countries, shard.Countries, row.Result.Countries, row.Result.ActiveCountries, row.Result.ECI}, {layers[row.Scheme+"/"+row.Level].Labels, shard.Disciplines, row.Result.Disciplines, row.Result.ActiveDisciplines, row.Result.PCI}} {
			if len(side.points) != len(side.labels) {
				return fmt.Errorf("static model dictionary dimension mismatch")
			}
			var ranks map[string]rankedScore
			universe := 0
			var e error
			if row.Result.Status == "ok" {
				ranks, universe, e = ranked(side.sourceLabels, side.active, side.values)
				if e != nil {
					return e
				}
			}
			for i, label := range side.labels {
				out.ModelPoints++
				status := row.Result.Status
				var value *float64
				var rank *int
				if status == "ok" {
					if score, ok := ranks[label.ID]; ok {
						value = &score.value
						rank = &score.rank
						if row.Result.Unstable {
							status = "unstable"
						}
					} else {
						status = "inactive"
					}
				}
				if yi < 0 || yi >= len(side.points[i]) || !checkPoint(side.points[i][yi], status, value, rank, universe) {
					out.ModelDifferences++
				}
			}
		}
	}
	if out.ModelDifferences != 0 {
		return fmt.Errorf("model transport audit failed")
	}
	return nil
}

func run() error {
	root := flag.String("export", "", "static export directory")
	core := flag.String("data-core", "", "immutable core result")
	all := flag.String("data-all", "", "immutable all result")
	mc := flag.String("methods-core", "", "core sidecar")
	ma := flag.String("methods-all", "", "all sidecar")
	output := flag.String("output", "", "audit JSON")
	flag.Parse()
	if *root == "" || *core == "" || *output == "" {
		return fmt.Errorf("--export --data-core --output required")
	}
	abs, e := filepath.Abs(*root)
	if e != nil {
		return e
	}
	var control site
	if e = readJSON(filepath.Join(abs, "data/site.json"), &control); e != nil {
		return e
	}
	var manifest struct {
		Artifacts []artifact `json:"artifacts"`
	}
	if e = readJSON(filepath.Join(abs, "export-manifest.json"), &manifest); e != nil {
		return e
	}
	v := &verifier{root: abs, matrices: map[string]*matrix{}, models: map[string]modelShard{}}
	for _, a := range manifest.Artifacts {
		p, e := v.path(a.Path)
		if e != nil {
			return e
		}
		sha, n, e := hashFile(p)
		if e != nil {
			return e
		}
		if n != a.Bytes || sha != a.SHA {
			return fmt.Errorf("export hash mismatch: %s", a.Path)
		}
	}
	reports := map[string]summary{}
	for _, item := range [][3]string{{"core", *core, *mc}, {"all", *all, *ma}} {
		if item[1] == "" {
			continue
		}
		d, ok := control.Datasets[item[0]]
		if !ok {
			return fmt.Errorf("static corpus missing")
		}
		sha, _, e := hashFile(filepath.Join(item[1], "completion.json"))
		if e != nil {
			return e
		}
		if sha != d.CompletionSHA {
			return fmt.Errorf("source completion identity mismatch")
		}
		var report summary
		if e = auditCounts(v, d, item[1], &report); e != nil {
			return e
		}
		if e = auditModels(v, d, "spectral-v1", filepath.Join(item[1], "eci.json"), &report); e != nil {
			return e
		}
		if item[2] != "" {
			if e = auditModels(v, d, "java-reflections-v1", filepath.Join(item[2], "java-reflections-v1.json"), &report); e != nil {
				return e
			}
		}
		reports[item[0]] = report
		v.matrices = map[string]*matrix{}
		v.models = map[string]modelShard{}
		runtime.GC()
		fmt.Fprintf(os.Stderr, "verified %s: %d source cells / %d model points\n", item[0], report.SourceRows, report.ModelPoints)
	}
	result := map[string]any{"status": "passed", "files_sha256_verified": len(manifest.Artifacts), "datasets": reports, "comparison": "Exact float64 bits for all source count cells, all matrix margins, every published model score/status and independently sorted global competition rank"}
	b, e := json.MarshalIndent(result, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(*output), 0755); e != nil {
		return e
	}
	return os.WriteFile(*output, append(b, '\n'), 0644)
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
