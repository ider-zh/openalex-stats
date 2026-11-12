// Package store holds projected work dimensions in pointer-free column arrays.
package store

import (
	"fmt"
	"math"

	"openalexstats/internal/model"
)

type dictionary struct {
	values []string
	codes  map[string]uint16
}

func newDictionary() dictionary {
	return dictionary{values: []string{""}, codes: map[string]uint16{"": 0}}
}
func (d *dictionary) intern(value string) (uint16, error) {
	if n, ok := d.codes[value]; ok {
		return n, nil
	}
	if len(d.values) > math.MaxUint16 {
		return 0, fmt.Errorf("dimension dictionary exceeds uint16 capacity")
	}
	n := uint16(len(d.values))
	d.values = append(d.values, value)
	d.codes[value] = n
	return n, nil
}

// Columns stores country/concept lists as CSR arrays rather than one heap object
// per work. It is append-only during scan and read-only during parallel reduction.
type Columns struct {
	ids                                                         []uint64
	years                                                       []int32
	types, fields, subfields                                    []uint16
	flags                                                       []uint8
	countryOffsets, conceptOffsets                              []uint64
	countries, conceptIDs                                       []uint16
	conceptLevels                                               []uint8
	conceptScores                                               []float64
	typeDict, fieldDict, subfieldDict, countryDict, conceptDict dictionary
}

func New() *Columns {
	return &Columns{countryOffsets: []uint64{0}, conceptOffsets: []uint64{0},
		typeDict: newDictionary(), fieldDict: newDictionary(), subfieldDict: newDictionary(), countryDict: newDictionary(), conceptDict: newDictionary()}
}
func (c *Columns) Len() int { return len(c.ids) }

// IDs is a borrowed read-only view in stable work ordinal order.
func (c *Columns) IDs() []uint64 { return c.ids }
func (c *Columns) Append(w model.Work) error {
	if w.ID == 0 || w.Year < math.MinInt32 || w.Year > math.MaxInt32 {
		return fmt.Errorf("invalid work ID/year")
	}
	t, err := c.typeDict.intern(w.Type)
	if err != nil {
		return err
	}
	f, err := c.fieldDict.intern(w.FieldID)
	if err != nil {
		return err
	}
	s, err := c.subfieldDict.intern(w.SubfieldID)
	if err != nil {
		return err
	}
	// Resolve dictionary entries before committing row offsets.
	for _, v := range w.Countries {
		if _, err = c.countryDict.intern(v); err != nil {
			return err
		}
	}
	for _, v := range w.Concepts {
		if v.Level < 0 || v.Level > 1 {
			continue
		}
		if _, err = c.conceptDict.intern(v.ID); err != nil {
			return err
		}
	}
	c.ids = append(c.ids, w.ID)
	c.years = append(c.years, int32(w.Year))
	c.types = append(c.types, t)
	c.fields = append(c.fields, f)
	c.subfields = append(c.subfields, s)
	var flags uint8
	if w.IsXPAC {
		flags |= 1
	}
	if w.IsParatext {
		flags |= 2
	}
	if w.IsRetracted {
		flags |= 4
	}
	c.flags = append(c.flags, flags)
	for _, v := range w.Countries {
		c.countries = append(c.countries, c.countryDict.codes[v])
	}
	c.countryOffsets = append(c.countryOffsets, uint64(len(c.countries)))
	for _, v := range w.Concepts {
		if v.Level < 0 || v.Level > 1 {
			continue
		}
		c.conceptIDs = append(c.conceptIDs, c.conceptDict.codes[v.ID])
		c.conceptLevels = append(c.conceptLevels, uint8(v.Level))
		c.conceptScores = append(c.conceptScores, v.Score)
	}
	c.conceptOffsets = append(c.conceptOffsets, uint64(len(c.conceptIDs)))
	return nil
}

// View fills caller-owned reusable scratch. Its slices are invalidated by the
// next View with the same scratch, and References is always empty.
func (c *Columns) View(i int, scratch *model.Work) {
	scratch.ID = c.ids[i]
	scratch.Year = int(c.years[i])
	scratch.Type = c.typeDict.values[c.types[i]]
	scratch.FieldID = c.fieldDict.values[c.fields[i]]
	scratch.SubfieldID = c.subfieldDict.values[c.subfields[i]]
	scratch.IsXPAC = c.flags[i]&1 != 0
	scratch.IsParatext = c.flags[i]&2 != 0
	scratch.IsRetracted = c.flags[i]&4 != 0
	scratch.Countries = scratch.Countries[:0]
	scratch.Concepts = scratch.Concepts[:0]
	scratch.References = nil
	for _, v := range c.countries[c.countryOffsets[i]:c.countryOffsets[i+1]] {
		scratch.Countries = append(scratch.Countries, c.countryDict.values[v])
	}
	for j := c.conceptOffsets[i]; j < c.conceptOffsets[i+1]; j++ {
		scratch.Concepts = append(scratch.Concepts, model.Concept{ID: c.conceptDict.values[c.conceptIDs[j]], Level: int(c.conceptLevels[j]), Score: c.conceptScores[j]})
	}
}

// EstimatedBytes includes allocated slice capacities; small dictionary/runtime
// overhead needs additional headroom in the enclosing resource budget.
func (c *Columns) EstimatedBytes() uint64 {
	return uint64(cap(c.ids))*8 + uint64(cap(c.years))*4 + uint64(cap(c.types)+cap(c.fields)+cap(c.subfields))*2 + uint64(cap(c.flags)) +
		uint64(cap(c.countryOffsets)+cap(c.conceptOffsets))*8 + uint64(cap(c.countries)+cap(c.conceptIDs))*2 + uint64(cap(c.conceptLevels)) + uint64(cap(c.conceptScores))*8
}

type Vocabulary struct {
	Countries, Fields, Subfields []string
	Concepts                     [2][]string
}

// Vocabulary enumerates small dimension dictionaries, scanning only compact
// concept codes to retain their per-level membership.
func (c *Columns) Vocabulary() Vocabulary {
	v := Vocabulary{Countries: append([]string(nil), c.countryDict.values[1:]...), Fields: append([]string(nil), c.fieldDict.values[1:]...), Subfields: append([]string(nil), c.subfieldDict.values[1:]...)}
	mask := make([]uint8, len(c.conceptDict.values))
	for i, code := range c.conceptIDs {
		level := c.conceptLevels[i]
		if level < 2 {
			mask[code] |= 1 << level
		}
	}
	for code, m := range mask {
		if code == 0 {
			continue
		}
		for level := 0; level < 2; level++ {
			if m&(1<<level) != 0 {
				v.Concepts[level] = append(v.Concepts[level], c.conceptDict.values[code])
			}
		}
	}
	return v
}
