package results

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/aggregate"
	"openalexstats/internal/eci"
)

type Label struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	ParentIDs   []string `json:"parent_ids,omitempty"`
}
type Layer struct {
	Scheme string  `json:"scheme"`
	Level  string  `json:"level"`
	Labels []Label `json:"labels"`
}
type Dictionary struct {
	Countries      []Label         `json:"countries"`
	Layers         []Layer         `json:"layers"`
	UnlistedLabels []UnlistedLabel `json:"unlisted_labels,omitempty"`
}
type UnlistedLabel struct {
	Scheme string `json:"scheme"`
	Level  string `json:"level"`
	ID     string `json:"id"`
}

// Reconcile explicitly extends a fixed dictionary with observed, non-UNKNOWN
// labels missing from snapshot entity tables. It preserves every such addition
// in UnlistedLabels and uses the ID as its display-name fallback. Call this on
// the union of all years/scenarios before constructing any annual matrices.
func (d Dictionary) Reconcile(rows []aggregate.Row) (Dictionary, error) {
	out := Dictionary{Countries: append([]Label(nil), d.Countries...), UnlistedLabels: append([]UnlistedLabel(nil), d.UnlistedLabels...)}
	countries := map[string]bool{}
	for _, c := range out.Countries {
		countries[c.ID] = true
	}
	indices := map[string]int{}
	labels := map[string]map[string]bool{}
	for _, layer := range d.Layers {
		key := layer.Scheme + "/" + layer.Level
		indices[key] = len(out.Layers)
		out.Layers = append(out.Layers, Layer{layer.Scheme, layer.Level, append([]Label(nil), layer.Labels...)})
		labels[key] = map[string]bool{}
		for _, label := range layer.Labels {
			labels[key][label.ID] = true
		}
	}
	for _, r := range rows {
		if !validLayer(r.Scheme, r.Level) || r.Country == "" || r.DisciplineID == "" {
			return out, fmt.Errorf("invalid observed dictionary label")
		}
		if r.Country != aggregate.Unknown && !countries[r.Country] {
			countries[r.Country] = true
			out.Countries = append(out.Countries, Label{ID: r.Country, DisplayName: r.Country})
			out.UnlistedLabels = append(out.UnlistedLabels, UnlistedLabel{"country", "", r.Country})
		}
		key := r.Scheme + "/" + r.Level
		i, ok := indices[key]
		if !ok {
			return out, fmt.Errorf("missing taxonomy layer %s", key)
		}
		if r.DisciplineID != aggregate.Unknown && !labels[key][r.DisciplineID] {
			labels[key][r.DisciplineID] = true
			out.Layers[i].Labels = append(out.Layers[i].Labels, Label{ID: r.DisciplineID, DisplayName: r.DisciplineID})
			out.UnlistedLabels = append(out.UnlistedLabels, UnlistedLabel{r.Scheme, r.Level, r.DisciplineID})
		}
	}
	if err := normalizeLabels(out.Countries); err != nil {
		return out, err
	}
	for i := range out.Layers {
		if err := normalizeLabels(out.Layers[i].Labels); err != nil {
			return out, err
		}
	}
	sort.Slice(out.UnlistedLabels, func(i, j int) bool {
		a, b := out.UnlistedLabels[i], out.UnlistedLabels[j]
		if a.Scheme != b.Scheme {
			return a.Scheme < b.Scheme
		}
		if a.Level != b.Level {
			return a.Level < b.Level
		}
		return a.ID < b.ID
	})
	return out, nil
}

type dictionaryRow struct {
	ID   string `parquet:"id"`
	Name string `parquet:"display_name"`
}
type conceptDictionaryRow struct {
	ID    string `parquet:"id"`
	Name  string `parquet:"display_name"`
	Level int32  `parquet:"level"`
}

type dictionaryParent struct {
	ID string `parquet:"id"`
}
type subfieldDictionaryRow struct {
	ID    string            `parquet:"id"`
	Name  string            `parquet:"display_name"`
	Field *dictionaryParent `parquet:"field"`
}

// LoadDictionary reads only small taxonomy/country tables declared by the
// snapshot manifest. It never scans works. Country IDs are canonical codes.
func LoadDictionary(ctx context.Context, manifestPath string) (Dictionary, error) {
	var out Dictionary
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return out, err
	}
	var manifest struct {
		Entities []struct {
			Entity string `json:"entity"`
			Files  []struct {
				URL string `json:"url"`
			} `json:"files"`
		} `json:"entities"`
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		return out, err
	}
	root, err := filepath.Abs(filepath.Dir(manifestPath))
	if err != nil {
		return out, err
	}
	out.Layers = []Layer{{Scheme: "topics", Level: "field"}, {Scheme: "topics", Level: "subfield"}, {Scheme: "concepts", Level: "0"}, {Scheme: "concepts", Level: "1"}}
	seenEntities := map[string]bool{}
	for _, entity := range manifest.Entities {
		if entity.Entity != "fields" && entity.Entity != "subfields" && entity.Entity != "concepts" && entity.Entity != "countries" {
			continue
		}
		seenEntities[entity.Entity] = true
		for _, file := range entity.Files {
			if err = ctx.Err(); err != nil {
				return out, err
			}
			_, relative, ok := strings.Cut(file.URL, "/data/parquet/")
			if !ok {
				return out, fmt.Errorf("dictionary URL outside expected prefix: %s", file.URL)
			}
			path := filepath.Join(root, filepath.FromSlash(relative))
			rel, e := filepath.Rel(root, path)
			if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
				return out, fmt.Errorf("dictionary path escapes snapshot")
			}
			if entity.Entity == "concepts" {
				err = readRows[conceptDictionaryRow](ctx, path, func(r conceptDictionaryRow) error {
					if r.Level == 0 || r.Level == 1 {
						out.Layers[2+int(r.Level)].Labels = append(out.Layers[2+int(r.Level)].Labels, label(r.ID, r.Name))
					}
					return nil
				})
			} else if entity.Entity == "subfields" {
				err = readRows[subfieldDictionaryRow](ctx, path, func(r subfieldDictionaryRow) error {
					l := label(r.ID, r.Name)
					if r.Field != nil && r.Field.ID != "" {
						l.ParentIDs = []string{r.Field.ID}
					}
					out.Layers[1].Labels = append(out.Layers[1].Labels, l)
					return nil
				})
			} else {
				err = readRows[dictionaryRow](ctx, path, func(r dictionaryRow) error {
					l := label(r.ID, r.Name)
					switch entity.Entity {
					case "countries":
						code := r.ID
						if slash := strings.LastIndex(code, "/"); slash >= 0 {
							code = code[slash+1:]
						}
						if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
							return fmt.Errorf("invalid country code in dictionary %q", r.ID)
						}
						l.ID = code
						out.Countries = append(out.Countries, l)
					case "fields":
						out.Layers[0].Labels = append(out.Layers[0].Labels, l)
					case "subfields":
						out.Layers[1].Labels = append(out.Layers[1].Labels, l)
					}
					return nil
				})
			}
			if err != nil {
				return out, err
			}
		}
	}
	for _, entity := range []string{"fields", "subfields", "concepts", "countries"} {
		if !seenEntities[entity] {
			return out, fmt.Errorf("manifest lacks %s dictionary", entity)
		}
	}
	if err = normalizeLabels(out.Countries); err != nil {
		return out, err
	}
	for i := range out.Layers {
		if err = normalizeLabels(out.Layers[i].Labels); err != nil {
			return out, err
		}
	}
	return out, nil
}
func label(id, name string) Label {
	if name == "" {
		name = id
	}
	return Label{ID: id, DisplayName: name}
}
func normalizeLabels(labels []Label) error {
	sort.Slice(labels, func(i, j int) bool { return labels[i].ID < labels[j].ID })
	for i, l := range labels {
		if l.ID == "" || l.ID == aggregate.Unknown {
			return fmt.Errorf("invalid dictionary ID %q", l.ID)
		}
		if i > 0 && labels[i-1].ID == l.ID {
			return fmt.Errorf("duplicate dictionary ID %q", l.ID)
		}
	}
	return nil
}
func readRows[T any](ctx context.Context, path string, yield func(T) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := parquet.NewGenericReader[T](f)
	defer r.Close()
	buf := make([]T, 256)
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		n, e := r.Read(buf)
		for _, v := range buf[:n] {
			if err = yield(v); err != nil {
				return err
			}
		}
		if e == io.EOF {
			return nil
		}
		if e != nil {
			return e
		}
	}
}

// Matrix uses every dictionary label in every year, filling unobserved cells
// with zero. Unknown-quality rows are omitted. Unrecognized real labels are an
// error rather than silently disappearing from a supposedly complete matrix.
func (d Dictionary) Matrix(rows []aggregate.Row, scheme, level string, year int, metric string) (eci.Matrix, error) {
	var m eci.Matrix
	if !validLayer(scheme, level) || year <= 0 || (metric != "publications" && metric != "citations") {
		return m, fmt.Errorf("invalid matrix selection")
	}
	var labels []Label
	for _, l := range d.Layers {
		if l.Scheme == scheme && l.Level == level {
			labels = l.Labels
			break
		}
	}
	if labels == nil {
		return m, fmt.Errorf("dictionary lacks %s/%s", scheme, level)
	}
	ci, di := map[string]int{}, map[string]int{}
	for _, c := range d.Countries {
		if c.ID == aggregate.Unknown {
			continue
		}
		if _, ok := ci[c.ID]; ok {
			return m, fmt.Errorf("duplicate country dictionary ID")
		}
		ci[c.ID] = len(m.Countries)
		m.Countries = append(m.Countries, c.ID)
	}
	for _, l := range labels {
		if l.ID == aggregate.Unknown {
			continue
		}
		if _, ok := di[l.ID]; ok {
			return m, fmt.Errorf("duplicate discipline dictionary ID")
		}
		di[l.ID] = len(m.Disciplines)
		m.Disciplines = append(m.Disciplines, l.ID)
	}
	m.Values = make([][]float64, len(m.Countries))
	for i := range m.Values {
		m.Values[i] = make([]float64, len(m.Disciplines))
	}
	for _, r := range rows {
		if r.Year != year || r.Scheme != scheme || r.Level != level || r.Country == aggregate.Unknown || r.DisciplineID == aggregate.Unknown {
			continue
		}
		i, ok := ci[r.Country]
		if !ok {
			return m, fmt.Errorf("country %s not in snapshot dictionary", r.Country)
		}
		j, ok := di[r.DisciplineID]
		if !ok {
			return m, fmt.Errorf("discipline %s not in snapshot dictionary", r.DisciplineID)
		}
		v := r.Publications
		if metric == "citations" {
			v = r.Citations
		}
		if !finiteNonnegative(v) {
			return m, fmt.Errorf("invalid matrix value")
		}
		m.Values[i][j] += v
	}
	return m, nil
}
