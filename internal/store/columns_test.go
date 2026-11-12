package store

import (
	"openalexstats/internal/model"
	"reflect"
	"testing"
)

func TestColumnRoundTripAndScratchReuse(t *testing.T) {
	c := New()
	a := model.Work{ID: 10, Year: 1995, Type: "article", Countries: []string{"CN", "US"}, FieldID: "F", SubfieldID: "S", IsRetracted: true, Concepts: []model.Concept{{ID: "C1", Level: 1, Score: .75}}, References: []uint64{99}}
	if err := c.Append(a); err != nil {
		t.Fatal(err)
	}
	if err := c.Append(model.Work{ID: 20, Year: 2005, Type: "review"}); err != nil {
		t.Fatal(err)
	}
	var scratch model.Work
	c.View(0, &scratch)
	a.References = nil
	if !reflect.DeepEqual(a, scratch) {
		t.Fatalf("round trip: %#v", scratch)
	}
	c.View(1, &scratch)
	if len(scratch.Countries) != 0 || len(scratch.Concepts) != 0 || scratch.IsRetracted || scratch.FieldID != "" {
		t.Fatalf("scratch retained previous row: %#v", scratch)
	}
	if c.EstimatedBytes() == 0 {
		t.Fatal("missing memory estimate")
	}
}
