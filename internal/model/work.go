// Package model contains the shared, projected representation of an OpenAlex work.
package model

// Work contains only the columns needed for corpus selection and statistics.
// IDs are the numeric suffix of OpenAlex W identifiers; zero is invalid.
// Scanner callbacks own their slices only until the next callback, so consumers
// that retain a Work must call Clone.
type Work struct {
	ID          uint64    `json:"id"`
	Year        int       `json:"year"`
	Type        string    `json:"type"`
	IsXPAC      bool      `json:"is_xpac"`
	IsParatext  bool      `json:"is_paratext"`
	IsRetracted bool      `json:"is_retracted"`
	Countries   []string  `json:"countries"`
	FieldID     string    `json:"field_id"`
	SubfieldID  string    `json:"subfield_id"`
	Concepts    []Concept `json:"concepts"`
	References  []uint64  `json:"references"`
}

type Concept struct {
	ID    string  `json:"id"`
	Level int     `json:"level"`
	Score float64 `json:"score"`
}

func (w Work) Clone() Work {
	w.Countries = append([]string(nil), w.Countries...)
	w.Concepts = append([]Concept(nil), w.Concepts...)
	w.References = append([]uint64(nil), w.References...)
	return w
}
