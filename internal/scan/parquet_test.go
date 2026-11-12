package scan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
	"openalexstats/internal/model"
)

func ptr[T any](v T) *T { return &v }

// Flat on purpose: anonymous embeddings in parquet-go v0.32.0 can erase
// nullable scalar values before they reach the reader.
type fixtureWork struct {
	ID          *string                 `parquet:"id"`
	Year        *int32                  `parquet:"publication_year"`
	Type        *string                 `parquet:"type"`
	IsXPAC      *bool                   `parquet:"is_xpac"`
	IsParatext  *bool                   `parquet:"is_paratext"`
	IsRetracted *bool                   `parquet:"is_retracted"`
	Authorships []*authorshipProjection `parquet:"authorships,list"`
	Topic       *topicProjection        `parquet:"primary_topic"`
	Concepts    []*conceptProjection    `parquet:"concepts,list"`
	References  []*string               `parquet:"referenced_works,list"`
	Title       string                  `parquet:"display_name"`
	Abstract    string                  `parquet:"abstract_inverted_index"`
}

func fixture(t *testing.T, rows []completeRow) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "works.parquet")
	records := make([]fixtureWork, len(rows))
	for i, r := range rows {
		records[i] = fixtureWork{r.ID, r.Year, r.Type, r.IsXPAC, r.IsParatext, r.IsRetracted, r.Authorships, r.Topic, r.Concepts, r.References, "omitted", strings.Repeat("ignored", 10000)}
	}
	if err := parquet.WriteFile(path, records); err != nil {
		t.Fatal(err)
	}
	return path
}
func base(id string, year int32) baseProjection {
	return baseProjection{ID: ptr(id), Year: ptr(year), Type: ptr("article"), IsXPAC: ptr(false), IsParatext: ptr(false), IsRetracted: ptr(false)}
}
func row(b baseProjection) completeRow {
	return completeRow{ID: b.ID, Year: b.Year, Type: b.Type, IsXPAC: b.IsXPAC, IsParatext: b.IsParatext, IsRetracted: b.IsRetracted}
}

func TestProjectedNestedDataAndNulls(t *testing.T) {
	rows := []completeRow{row(base("https://openalex.org/W1", 2001)), row(base("W2", 2000))}
	rows[0].Authorships = []*authorshipProjection{nil, {Countries: []*string{ptr("US"), nil, ptr("CN"), ptr("US")}}, {Countries: []*string{ptr("CN")}}}
	rows[0].Topic = &topicProjection{Subfield: &hierarchyID{ID: ptr("https://openalex.org/subfields/1702")}}
	rows[0].Concepts = []*conceptProjection{{ID: ptr("C1"), Level: ptr(int32(0)), Score: ptr(0.2)}, {ID: ptr("C2"), Level: ptr(int32(1)), Score: ptr(0.0)}, {ID: ptr("C3"), Level: ptr(int32(2)), Score: ptr(0.8)}, {ID: ptr("C4"), Level: nil, Score: ptr(0.8)}, {ID: ptr("C5"), Level: ptr(int32(0)), Score: nil}, nil}
	rows[0].References = []*string{ptr("W2"), ptr("https://openalex.org/W3"), nil, ptr("W0"), ptr("bad")}
	path := fixture(t, rows)
	var got []model.Work
	stats, err := Parquet(context.Background(), []string{path}, Options{ReadReferences: true, ReadDimensions: true}, func(w model.Work) error { got = append(got, w.Clone()); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if stats.Rows != 2 || stats.FilesOpened != 1 || stats.InvalidReferenceIDs != 3 || stats.InvalidConcepts != 3 {
		t.Fatalf("unexpected quality counters: %+v", stats)
	}
	if !reflect.DeepEqual(got[0].Countries, []string{"CN", "US"}) || !reflect.DeepEqual(got[0].References, []uint64{2, 3}) {
		t.Fatalf("incorrect countries/references: %+v", got[0])
	}
	if got[0].FieldID != "" || got[0].SubfieldID != "https://openalex.org/subfields/1702" || len(got[0].Concepts) != 2 || got[0].Concepts[1].Score != 0 {
		t.Fatalf("incorrect dimension projection: %+v", got[0])
	}
	if len(got[1].Countries) != 0 || len(got[1].References) != 0 || len(got[1].Concepts) != 0 {
		t.Fatalf("null lists inherited previous row: %+v", got[1])
	}
	for _, opts := range []Options{{}, {ReadReferences: true}, {ReadDimensions: true}} {
		_, err := Parquet(context.Background(), []string{path}, opts, func(w model.Work) error {
			if !opts.ReadReferences && len(w.References) != 0 {
				t.Fatal("references decoded despite projection")
			}
			if !opts.ReadDimensions && (len(w.Countries) != 0 || len(w.Concepts) != 0 || w.SubfieldID != "") {
				t.Fatal("dimensions decoded despite projection")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestProjectionSchemaOmitsUnrequestedColumns(t *testing.T) {
	path := fixture(t, []completeRow{row(base("W1", 2000))})
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r := parquet.NewGenericReader[baseRow](f)
	defer r.Close()
	if len(r.Schema().Columns()) != 6 {
		t.Fatalf("unwanted columns: %v", r.Schema().Columns())
	}
	for _, column := range r.Schema().Columns() {
		if column[0] == "referenced_works" || column[0] == "authorships" || column[0] == "abstract_inverted_index" {
			t.Fatalf("unrequested column in projection: %v", column)
		}
	}
}

func TestOrderingLimitCancellationAndCallbackErrors(t *testing.T) {
	var paths []string
	for _, ids := range [][]string{{"W1", "W2"}, {"W3", "W4"}, {"W5"}} {
		var rows []completeRow
		for _, id := range ids {
			rows = append(rows, row(base(id, 2000)))
		}
		paths = append(paths, fixture(t, rows))
	}
	var got []uint64
	stats, err := Parquet(context.Background(), paths, Options{ReaderWorkers: 3, MaxRows: 3}, func(w model.Work) error { got = append(got, w.ID); return nil })
	if err != nil || stats.Rows != 3 || !reflect.DeepEqual(got, []uint64{1, 2, 3}) {
		t.Fatalf("ordering/limit: %+v %v %v", stats, got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	seen := 0
	_, err = Parquet(ctx, paths, Options{ReaderWorkers: 3}, func(w model.Work) error { seen++; cancel(); return nil })
	if !errors.Is(err, context.Canceled) || seen != 1 {
		t.Fatalf("cancellation: calls=%d err=%v", seen, err)
	}
	sentinel := errors.New("callback stopped")
	_, err = Parquet(context.Background(), paths, Options{ReaderWorkers: 3}, func(model.Work) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("callback error not preserved: %v", err)
	}
}

func TestInvalidMainIDAndNullNumericFields(t *testing.T) {
	for _, id := range []*string{nil, ptr("W0"), ptr("https://example.com/W1")} {
		r := base("W1", 2000)
		r.ID = id
		_, err := Parquet(context.Background(), []string{fixture(t, []completeRow{row(r)})}, Options{}, func(model.Work) error { t.Fatal("invalid ID reached consumer"); return nil })
		if err == nil {
			t.Fatal("invalid main ID accepted")
		}
	}
	r := base("W2", 2000)
	r.Year, r.IsXPAC = nil, nil
	path := fixture(t, []completeRow{row(r)})
	stats, err := Parquet(context.Background(), []string{path}, Options{}, func(w model.Work) error {
		if w.Year != 0 {
			t.Fatal("null year became a valid year")
		}
		return nil
	})
	if err != nil || stats.MissingYears != 1 || stats.NullXPACFlags != 1 {
		t.Fatalf("null metadata: %+v %v", stats, err)
	}
}

func TestParseWorkID(t *testing.T) {
	for _, s := range []string{"W1", "https://openalex.org/W18446744073709551615"} {
		if _, err := ParseWorkID(s); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []string{"", "1", "W", "W0", "W01", "W+1", "W-1", " W1", "W1 ", "http://openalex.org/W1", "https://evil.test/W1", "W18446744073709551616", "W1.0"} {
		if _, err := ParseWorkID(s); err == nil {
			t.Fatalf("accepted %q", s)
		}
	}
}
