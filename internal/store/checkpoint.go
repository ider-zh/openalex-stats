package store

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
)

const columnMagic = "OACOLS01"

type columnHeader struct {
	Version      int
	Dictionaries [][]string
}

// WriteTo persists columns without materializing a second whole-table buffer.
func (c *Columns) WriteTo(w io.Writer) (int64, error) {
	count := &byteCountWriter{w: w}
	b := bufio.NewWriterSize(count, 1<<20)
	if _, e := io.WriteString(b, columnMagic); e != nil {
		return 0, e
	}
	header, e := json.Marshal(columnHeader{Version: 1, Dictionaries: [][]string{c.typeDict.values, c.fieldDict.values, c.subfieldDict.values, c.countryDict.values, c.conceptDict.values}})
	if e != nil {
		return 0, e
	}
	if e = writeArray(b, header); e != nil {
		return 0, e
	}
	for _, array := range []any{c.ids, c.years, c.types, c.fields, c.subfields, c.flags, c.countryOffsets, c.conceptOffsets, c.countries, c.conceptIDs, c.conceptLevels, c.conceptScores} {
		if e = writeArray(b, array); e != nil {
			return count.n, e
		}
	}
	e = b.Flush()
	return count.n, e
}

type byteCountWriter struct {
	w io.Writer
	n int64
}

func (w *byteCountWriter) Write(p []byte) (int, error) {
	n, e := w.w.Write(p)
	w.n += int64(n)
	return n, e
}

func writeArray(w io.Writer, array any) error {
	var length int
	switch a := array.(type) {
	case []byte:
		length = len(a)
	case []uint16:
		length = len(a)
	case []uint64:
		length = len(a)
	case []int32:
		length = len(a)
	case []float64:
		length = len(a)
	default:
		return fmt.Errorf("unsupported column array")
	}
	var n [8]byte
	binary.LittleEndian.PutUint64(n[:], uint64(length))
	if _, e := w.Write(n[:]); e != nil {
		return e
	}
	buf := make([]byte, 64<<10)
	write := func(size int, fill func([]byte, int)) error {
		for start := 0; start < length; {
			count := min(len(buf)/size, length-start)
			for i := 0; i < count; i++ {
				fill(buf[i*size:(i+1)*size], start+i)
			}
			if _, e := w.Write(buf[:count*size]); e != nil {
				return e
			}
			start += count
		}
		return nil
	}
	switch a := array.(type) {
	case []byte:
		_, e := w.Write(a)
		return e
	case []uint16:
		return write(2, func(b []byte, i int) { binary.LittleEndian.PutUint16(b, a[i]) })
	case []uint64:
		return write(8, func(b []byte, i int) { binary.LittleEndian.PutUint64(b, a[i]) })
	case []int32:
		return write(4, func(b []byte, i int) { binary.LittleEndian.PutUint32(b, uint32(a[i])) })
	case []float64:
		return write(8, func(b []byte, i int) { binary.LittleEndian.PutUint64(b, math.Float64bits(a[i])) })
	}
	return nil
}

// ReadFrom limits total numeric allocation before reading untrusted lengths.
func ReadFrom(reader io.Reader, maxBytes uint64) (*Columns, error) {
	r := bufio.NewReaderSize(reader, 1<<20)
	magic := make([]byte, len(columnMagic))
	if _, e := io.ReadFull(r, magic); e != nil {
		return nil, e
	}
	if string(magic) != columnMagic {
		return nil, fmt.Errorf("invalid column checkpoint magic")
	}
	allocated := uint64(0)
	read := func(size uint64) ([]byte, int, error) {
		var n [8]byte
		if _, e := io.ReadFull(r, n[:]); e != nil {
			return nil, 0, e
		}
		length := binary.LittleEndian.Uint64(n[:])
		if length > uint64(^uint(0)>>1) || size > 0 && length > (maxBytes-allocated)/size {
			return nil, 0, fmt.Errorf("column checkpoint exceeds memory budget")
		}
		allocated += length * size
		return nil, int(length), nil
	}
	_, n, e := read(1)
	if e != nil {
		return nil, e
	}
	if n > 16<<20 {
		return nil, fmt.Errorf("oversized column dictionary")
	}
	headerBytes := make([]byte, n)
	if _, e = io.ReadFull(r, headerBytes); e != nil {
		return nil, e
	}
	var header columnHeader
	if e = json.Unmarshal(headerBytes, &header); e != nil {
		return nil, e
	}
	if header.Version != 1 || len(header.Dictionaries) != 5 {
		return nil, fmt.Errorf("unsupported column checkpoint")
	}
	c := New()
	dicts := []*dictionary{&c.typeDict, &c.fieldDict, &c.subfieldDict, &c.countryDict, &c.conceptDict}
	for i, d := range dicts {
		if len(header.Dictionaries[i]) == 0 || header.Dictionaries[i][0] != "" || len(header.Dictionaries[i]) > math.MaxUint16+1 {
			return nil, fmt.Errorf("invalid column dictionary")
		}
		*d = newDictionary()
		for _, v := range header.Dictionaries[i][1:] {
			if _, ok := d.codes[v]; ok {
				return nil, fmt.Errorf("duplicate dictionary value")
			}
			if _, e = d.intern(v); e != nil {
				return nil, e
			}
		}
	}
	buf := make([]byte, 64<<10)
	readValues := func(size int, set func(int, []byte), length int) error {
		for start := 0; start < length; {
			count := min(len(buf)/size, length-start)
			if _, e := io.ReadFull(r, buf[:count*size]); e != nil {
				return e
			}
			for i := 0; i < count; i++ {
				set(start+i, buf[i*size:(i+1)*size])
			}
			start += count
		}
		return nil
	}
	u64 := func(dst *[]uint64) error {
		_, n, e := read(8)
		if e != nil {
			return e
		}
		*dst = make([]uint64, n)
		return readValues(8, func(i int, b []byte) { (*dst)[i] = binary.LittleEndian.Uint64(b) }, n)
	}
	u16 := func(dst *[]uint16) error {
		_, n, e := read(2)
		if e != nil {
			return e
		}
		*dst = make([]uint16, n)
		return readValues(2, func(i int, b []byte) { (*dst)[i] = binary.LittleEndian.Uint16(b) }, n)
	}
	u8 := func(dst *[]uint8) error {
		_, n, e := read(1)
		if e != nil {
			return e
		}
		*dst = make([]byte, n)
		_, e = io.ReadFull(r, *dst)
		return e
	}
	if e = u64(&c.ids); e != nil {
		return nil, e
	}
	_, n, e = read(4)
	if e != nil {
		return nil, e
	}
	c.years = make([]int32, n)
	if e = readValues(4, func(i int, b []byte) { c.years[i] = int32(binary.LittleEndian.Uint32(b)) }, n); e != nil {
		return nil, e
	}
	for _, dst := range []*[]uint16{&c.types, &c.fields, &c.subfields} {
		if e = u16(dst); e != nil {
			return nil, e
		}
	}
	if e = u8(&c.flags); e != nil {
		return nil, e
	}
	for _, dst := range []*[]uint64{&c.countryOffsets, &c.conceptOffsets} {
		if e = u64(dst); e != nil {
			return nil, e
		}
	}
	for _, dst := range []*[]uint16{&c.countries, &c.conceptIDs} {
		if e = u16(dst); e != nil {
			return nil, e
		}
	}
	if e = u8(&c.conceptLevels); e != nil {
		return nil, e
	}
	_, n, e = read(8)
	if e != nil {
		return nil, e
	}
	c.conceptScores = make([]float64, n)
	if e = readValues(8, func(i int, b []byte) { c.conceptScores[i] = math.Float64frombits(binary.LittleEndian.Uint64(b)) }, n); e != nil {
		return nil, e
	}
	rows := c.Len()
	if len(c.years) != rows || len(c.types) != rows || len(c.fields) != rows || len(c.subfields) != rows || len(c.flags) != rows || len(c.countryOffsets) != rows+1 || len(c.conceptOffsets) != rows+1 || len(c.conceptIDs) != len(c.conceptLevels) || len(c.conceptIDs) != len(c.conceptScores) {
		return nil, fmt.Errorf("column checkpoint shape mismatch")
	}
	for _, pair := range []struct {
		offsets []uint64
		length  int
	}{{c.countryOffsets, len(c.countries)}, {c.conceptOffsets, len(c.conceptIDs)}} {
		if pair.offsets[0] != 0 || pair.offsets[len(pair.offsets)-1] != uint64(pair.length) {
			return nil, fmt.Errorf("invalid CSR endpoints")
		}
		for i := 1; i < len(pair.offsets); i++ {
			if pair.offsets[i] < pair.offsets[i-1] {
				return nil, fmt.Errorf("invalid CSR offsets")
			}
		}
	}
	for _, pair := range []struct {
		codes  []uint16
		length int
	}{{c.types, len(c.typeDict.values)}, {c.fields, len(c.fieldDict.values)}, {c.subfields, len(c.subfieldDict.values)}, {c.countries, len(c.countryDict.values)}, {c.conceptIDs, len(c.conceptDict.values)}} {
		for _, code := range pair.codes {
			if int(code) >= pair.length {
				return nil, fmt.Errorf("invalid dimension code")
			}
		}
	}
	if _, e = r.ReadByte(); e != io.EOF {
		return nil, fmt.Errorf("trailing column checkpoint data")
	}
	return c, nil
}

func (c *Columns) YearAt(ordinal int) int { return int(c.years[ordinal]) }
