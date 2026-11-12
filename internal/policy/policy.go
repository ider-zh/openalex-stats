// Package policy separates statistical choices from execution settings.
package policy

import (
	"encoding/json"
	"fmt"
	"os"

	"openalexstats/internal/model"
)

var DefaultBlacklist = []string{
	"editorial", "erratum", "retraction", "paratext", "peer-review",
	"reference-entry", "libguides", "supplementary-materials", "dataset",
	"database", "software", "grant", "standard", "book", "dissertation",
}

type Scope struct {
	Corpus          string   `json:"corpus"`
	CorpusYearStart int      `json:"corpus_year_start"`
	Blacklist       []string `json:"blacklist"`
	excluded        map[string]bool
}

// New validates explicit corpus choices. CorpusYearStart=0 retains older targets.
func New(corpus string, corpusYearStart int, blacklist []string) (Scope, error) {
	if corpus != "core" && corpus != "all" {
		return Scope{}, fmt.Errorf("corpus must be core or all")
	}
	if corpusYearStart < 0 || corpusYearStart > 9999 {
		return Scope{}, fmt.Errorf("invalid corpus year start %d", corpusYearStart)
	}
	s := Scope{Corpus: corpus, CorpusYearStart: corpusYearStart, Blacklist: append([]string(nil), blacklist...), excluded: make(map[string]bool)}
	for _, v := range blacklist {
		s.excluded[v] = true
	}
	return s, nil
}

func (s Scope) Includes(w model.Work) bool {
	return w.ID != 0 && !(s.Corpus == "core" && w.IsXPAC) &&
		!s.excluded[w.Type] && !w.IsParatext &&
		(s.CorpusYearStart == 0 || w.Year >= s.CorpusYearStart)
}

// LoadBlacklist reads the existing review document; unrelated fields are ignored.
func LoadBlacklist(path string) ([]string, error) {
	if path == "" {
		return append([]string(nil), DefaultBlacklist...), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p struct {
		TypeSelection struct {
			ExcludedTypes []string `json:"excluded_types"`
		} `json:"type_selection"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	if len(p.TypeSelection.ExcludedTypes) == 0 {
		return nil, fmt.Errorf("policy file contains no explicit blacklist")
	}
	return p.TypeSelection.ExcludedTypes, nil
}
