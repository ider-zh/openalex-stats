package store

import (
	"openalexstats/internal/model"
	"reflect"
	"testing"
)

func TestMergeRemapsDictionaryAndPreservesOrdinal(t *testing.T) {
	first, second := New(), New()
	w1 := model.Work{ID: 1, Year: 1995, Type: "article", FieldID: "B", Countries: []string{"US"}, Concepts: []model.Concept{{ID: "CB", Level: 0, Score: .8}}}
	w2 := model.Work{ID: 2, Year: 2002, Type: "review", FieldID: "A", Countries: []string{"CN", "US"}, Concepts: []model.Concept{{ID: "CA", Level: 1, Score: .4}, {ID: "CB", Level: 0, Score: .6}}}
	_ = first.Append(w1)
	_ = second.Append(w2)
	shape := Shape{Rows: 2, Countries: 3, Concepts: 3}
	out, e := NewReserved(shape)
	if e != nil {
		t.Fatal(e)
	}
	if e = out.AppendColumns(first); e != nil {
		t.Fatal(e)
	}
	if e = out.AppendColumns(second); e != nil {
		t.Fatal(e)
	}
	for i, want := range []model.Work{w1, w2} {
		var got model.Work
		out.View(i, &got)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("row%d: %#v", i, got)
		}
	}
	if out.Shape() != shape {
		t.Fatalf("shape: %+v", out.Shape())
	}
}
