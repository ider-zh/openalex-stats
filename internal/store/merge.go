package store

import (
	"fmt"
	"math"
)

type Shape struct {
	Rows      int `json:"rows"`
	Countries int `json:"country_entries"`
	Concepts  int `json:"concept_entries"`
}

func (c *Columns) Shape() Shape { return Shape{c.Len(), len(c.countries), len(c.conceptIDs)} }
func (s Shape) EstimatedBytes() (uint64, error) {
	if s.Rows < 0 || uint64(s.Rows) > math.MaxUint32 || s.Countries < 0 || s.Concepts < 0 || uint64(s.Countries) > math.MaxUint64/32 || uint64(s.Concepts) > math.MaxUint64/32 {
		return 0, fmt.Errorf("invalid column shape")
	}
	return uint64(s.Rows)*35 + 16 + uint64(s.Countries)*2 + uint64(s.Concepts)*11, nil
}
func NewReserved(s Shape) (*Columns, error) {
	if _, e := s.EstimatedBytes(); e != nil {
		return nil, e
	}
	c := New()
	c.ids = make([]uint64, 0, s.Rows)
	c.years = make([]int32, 0, s.Rows)
	c.types = make([]uint16, 0, s.Rows)
	c.fields = make([]uint16, 0, s.Rows)
	c.subfields = make([]uint16, 0, s.Rows)
	c.flags = make([]uint8, 0, s.Rows)
	c.countryOffsets = make([]uint64, 1, s.Rows+1)
	c.conceptOffsets = make([]uint64, 1, s.Rows+1)
	c.countries = make([]uint16, 0, s.Countries)
	c.conceptIDs = make([]uint16, 0, s.Concepts)
	c.conceptLevels = make([]uint8, 0, s.Concepts)
	c.conceptScores = make([]float64, 0, s.Concepts)
	return c, nil
}

// AppendColumns concatenates integer/CSR arrays and remaps only the small
// per-shard dictionaries. It never reconstructs per-work strings or slices.
func (c *Columns) AppendColumns(other *Columns) error {
	if c == other {
		return fmt.Errorf("cannot append columns to themselves")
	}
	if uint64(c.Len())+uint64(other.Len()) > math.MaxUint32 {
		return fmt.Errorf("combined ordinal space exceeds uint32")
	}
	remap := func(dst *dictionary, src dictionary) ([]uint16, error) {
		codes := make([]uint16, len(src.values))
		for i, value := range src.values {
			n, e := dst.intern(value)
			if e != nil {
				return nil, e
			}
			codes[i] = n
		}
		return codes, nil
	}
	t, e := remap(&c.typeDict, other.typeDict)
	if e != nil {
		return e
	}
	f, e := remap(&c.fieldDict, other.fieldDict)
	if e != nil {
		return e
	}
	s, e := remap(&c.subfieldDict, other.subfieldDict)
	if e != nil {
		return e
	}
	countries, e := remap(&c.countryDict, other.countryDict)
	if e != nil {
		return e
	}
	concepts, e := remap(&c.conceptDict, other.conceptDict)
	if e != nil {
		return e
	}
	c.ids = append(c.ids, other.ids...)
	c.years = append(c.years, other.years...)
	c.flags = append(c.flags, other.flags...)
	for _, v := range other.types {
		c.types = append(c.types, t[v])
	}
	for _, v := range other.fields {
		c.fields = append(c.fields, f[v])
	}
	for _, v := range other.subfields {
		c.subfields = append(c.subfields, s[v])
	}
	countryBase, conceptBase := uint64(len(c.countries)), uint64(len(c.conceptIDs))
	for _, v := range other.countryOffsets[1:] {
		c.countryOffsets = append(c.countryOffsets, v+countryBase)
	}
	for _, v := range other.conceptOffsets[1:] {
		c.conceptOffsets = append(c.conceptOffsets, v+conceptBase)
	}
	for _, v := range other.countries {
		c.countries = append(c.countries, countries[v])
	}
	for _, v := range other.conceptIDs {
		c.conceptIDs = append(c.conceptIDs, concepts[v])
	}
	c.conceptLevels = append(c.conceptLevels, other.conceptLevels...)
	c.conceptScores = append(c.conceptScores, other.conceptScores...)
	return nil
}
