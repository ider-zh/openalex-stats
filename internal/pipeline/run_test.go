package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/aggregate"
	"openalexstats/internal/policy"
)

type fixtureID struct {
	ID string `parquet:"id"`
}
type fixtureTopic struct {
	Field    fixtureID `parquet:"field"`
	Subfield fixtureID `parquet:"subfield"`
}
type fixtureAuthor struct {
	Countries []string `parquet:"countries,list"`
}
type fixtureConcept struct {
	ID    string  `parquet:"id"`
	Level int32   `parquet:"level"`
	Score float32 `parquet:"score"`
}
type fixtureWork struct {
	ID          string           `parquet:"id"`
	Year        int32            `parquet:"publication_year"`
	Type        string           `parquet:"type"`
	XPAC        bool             `parquet:"is_xpac"`
	Paratext    bool             `parquet:"is_paratext"`
	Retracted   bool             `parquet:"is_retracted"`
	Authorships []fixtureAuthor  `parquet:"authorships,list"`
	Topic       fixtureTopic     `parquet:"primary_topic"`
	Concepts    []fixtureConcept `parquet:"concepts,list"`
	References  []string         `parquet:"referenced_works,list"`
}

func writeFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "works.parquet")
	row := func(id string, year int32, typ, country, field string, refs ...string) fixtureWork {
		return fixtureWork{ID: id, Year: year, Type: typ, Authorships: []fixtureAuthor{{Countries: []string{country}}}, Topic: fixtureTopic{Field: fixtureID{field}, Subfield: fixtureID{field + "-sub"}}, Concepts: []fixtureConcept{{ID: field + "-c0", Level: 0, Score: .9}, {ID: field + "-c1", Level: 1, Score: .8}}, References: refs}
	}
	rows := []fixtureWork{row("W1", 1995, "article", "US", "BIO"), row("W2", 2001, "article", "CN", "COMP", "W1", "W1", "W3", "W99"), row("W3", 2001, "dataset", "US", "BIO"), row("W4", 2002, "article", "GB", "BIO", "W1", "W2"), row("W5", 2003, "preprint", "CN", "COMP", "W2")}
	if e := parquet.WriteFile(path, rows); e != nil {
		t.Fatal(e)
	}
	return path
}
func TestInducedGraphAnnualAttributionAndYearBoundary(t *testing.T) {
	path := writeFixture(t)
	for _, start := range []int{0, 2000} {
		t.Run(string(rune('A'+start/2000)), func(t *testing.T) {
			scope, _ := policy.New("all", start, policy.DefaultBlacklist)
			// Exercise configuration JSON round-trip: the private blacklist lookup
			// must be reconstructed by Run rather than silently becoming empty.
			b, _ := json.Marshal(scope)
			_ = json.Unmarshal(b, &scope)
			out := filepath.Join(t.TempDir(), "result")
			r, e := Run(context.Background(), Config{Input: Input{Paths: []string{path}}, Scope: scope, Weighting: aggregate.Config{CountryMode: "fractional", ConceptMode: "fractional"}, YearStart: 2000, YearEnd: 2003, Workers: 4, Readers: 1, MaxMemoryBytes: 128 << 20, OutputDir: out}, nil)
			if e != nil {
				t.Fatal(e)
			}
			wantEdges := uint64(4)
			wantSelected := 4
			if start == 2000 {
				wantEdges = 2
				wantSelected = 3
			}
			if r.Graph.RetainedEdges != wantEdges || r.SelectedWorks != wantSelected || r.Models != 32 || r.Status != "pilot_complete" {
				t.Fatalf("unexpected report: %+v", r)
			}
			rows, e := parquet.ReadFile[aggregate.Row](filepath.Join(out, "cube.parquet"))
			if e != nil {
				t.Fatal(e)
			}
			pub, cit := 0.0, 0.0
			foundOldTarget := false
			for _, v := range rows {
				if v.Scheme != "topics" || v.Level != "field" {
					continue
				}
				pub += v.Publications
				cit += v.Citations
				if v.Country == "US" && v.Year == 2001 && v.DisciplineID == "BIO" && v.Citations == 1 {
					foundOldTarget = true
				}
				if v.Country == "CN" && v.Year == 2002 && v.DisciplineID == "COMP" && v.Citations != 1 {
					t.Fatal("citation not attributed to target discipline")
				}
			}
			if pub != 3 || cit != float64(wantEdges) || foundOldTarget != (start == 0) {
				t.Fatalf("incorrect annual totals: pub=%v cit=%v old=%v", pub, cit, foundOldTarget)
			}
			if r.InputMetadataSHA256 == "" {
				t.Fatal("missing two-pass digest")
			}
			if _, e = os.Stat(filepath.Join(out, "run.json")); e != nil {
				t.Fatal(e)
			}
			if _, e = Run(context.Background(), r.Config, nil); e == nil {
				t.Fatal("run overwrote immutable output directory")
			}
		})
	}
}
