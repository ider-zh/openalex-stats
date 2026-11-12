package store

import (
	"bytes"
	"openalexstats/internal/model"
	"reflect"
	"testing"
)

func TestCheckpointRoundTripBoundsAndTruncation(t *testing.T) {
	c := New()
	want := []model.Work{{ID: 1, Year: 1995, Type: "article", Countries: []string{"CN", "US"}, FieldID: "F", SubfieldID: "S", Concepts: []model.Concept{{ID: "C", Level: 1, Score: .3}}}, {ID: 2, Year: 2000, IsXPAC: true}}
	for _, w := range want {
		if e := c.Append(w); e != nil {
			t.Fatal(e)
		}
	}
	var b bytes.Buffer
	if _, e := c.WriteTo(&b); e != nil {
		t.Fatal(e)
	}
	got, e := ReadFrom(bytes.NewReader(b.Bytes()), 1<<20)
	if e != nil {
		t.Fatal(e)
	}
	var w model.Work
	for i := range want {
		got.View(i, &w)
		a, b := want[i], w
		if len(a.Countries) == 0 {
			a.Countries = nil
			b.Countries = nil
		}
		if len(a.Concepts) == 0 {
			a.Concepts = nil
			b.Concepts = nil
		}
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("row %d: %#v", i, w)
		}
	}
	if _, e = ReadFrom(bytes.NewReader(b.Bytes()), 8); e == nil {
		t.Fatal("unbounded allocation accepted")
	}
	if _, e = ReadFrom(bytes.NewReader(b.Bytes()[:b.Len()-1]), 1<<20); e == nil {
		t.Fatal("truncated checkpoint accepted")
	}
}
