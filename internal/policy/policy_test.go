package policy

import (
	"openalexstats/internal/model"
	"testing"
)

func TestBlacklistDoesNotBecomeWhitelist(t *testing.T) {
	s, _ := New("all", 0, DefaultBlacklist)
	for _, typ := range []string{"article", "letter", "other", "", "new-type", "preprint"} {
		if !s.Includes(model.Work{ID: 1, Year: 1995, Type: typ}) {
			t.Fatalf("unexpected exclusion %q", typ)
		}
	}
	for _, typ := range DefaultBlacklist {
		if s.Includes(model.Work{ID: 1, Year: 2005, Type: typ}) {
			t.Fatalf("blacklist missed %s", typ)
		}
	}
	if !s.Includes(model.Work{ID: 1, Year: 2005, Type: "article", IsRetracted: true}) {
		t.Fatal("retracted original is not a retraction notice")
	}
	s, _ = New("core", 2000, DefaultBlacklist)
	if s.Includes(model.Work{ID: 1, Year: 1999}) || s.Includes(model.Work{ID: 1, Year: 2005, IsXPAC: true}) {
		t.Fatal("scope boundary ignored")
	}
}
