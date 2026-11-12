package main

import (
	"fmt"
	"openalexstats/internal/model"
	"sort"
)

type patch struct {
	ID        uint64   `parquet:"id"`
	Year      int32    `parquet:"year"`
	XPAC      bool     `parquet:"is_xpac"`
	Countries []string `parquet:"proposed_countries,list"`
}

// Original Work storage and the shared patch are never modified.
func apply(w *model.Work, p patch, valid map[string]bool) error {
	if w.ID != p.ID || w.Year != int(p.Year) || w.IsXPAC != p.XPAC {
		return fmt.Errorf("patch identity mismatch W%d", p.ID)
	}
	if len(w.Countries) != 0 {
		return fmt.Errorf("patch may only recover original UNKNOWN W%d", p.ID)
	}
	if len(p.Countries) == 0 {
		return fmt.Errorf("empty proposed set W%d", p.ID)
	}
	seen := map[string]bool{}
	for _, c := range p.Countries {
		if c == "" || c == "UNKNOWN" || !valid[c] || seen[c] {
			return fmt.Errorf("invalid/duplicate country %q W%d", c, p.ID)
		}
		seen[c] = true
	}
	w.Countries = append(w.Countries[:0], p.Countries...)
	sort.Strings(w.Countries)
	return nil
}
