package pipeline

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"os"

	"openalexstats/internal/model"
)

type passDigest struct {
	h     hash.Hash
	types map[string]uint32
}

func newPassDigest() *passDigest { return &passDigest{h: sha256.New(), types: map[string]uint32{}} }
func (d *passDigest) add(w model.Work, selected bool) {
	code, ok := d.types[w.Type]
	if !ok {
		code = uint32(len(d.types) + 1)
		d.types[w.Type] = code
		var size [8]byte
		binary.LittleEndian.PutUint64(size[:], uint64(len(w.Type)))
		_, _ = d.h.Write(size[:])
		_, _ = d.h.Write([]byte(w.Type))
	}
	var b [22]byte
	binary.LittleEndian.PutUint64(b[:8], w.ID)
	binary.LittleEndian.PutUint64(b[8:16], uint64(w.Year))
	binary.LittleEndian.PutUint32(b[16:20], code)
	if w.IsXPAC {
		b[20] |= 1
	}
	if w.IsParatext {
		b[20] |= 2
	}
	if w.IsRetracted {
		b[20] |= 4
	}
	if selected {
		b[21] = 1
	}
	_, _ = d.h.Write(b[:])
}
func (d *passDigest) sum() string { return hex.EncodeToString(d.h.Sum(nil)) }

type fileStamp struct{ size, modified int64 }

func fileStamps(paths []string) ([]fileStamp, error) {
	out := make([]fileStamp, len(paths))
	for i, path := range paths {
		info, e := os.Stat(path)
		if e != nil {
			return nil, e
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("input is not a regular file: %s", path)
		}
		out[i] = fileStamp{info.Size(), info.ModTime().UnixNano()}
	}
	return out, nil
}
func verifyStamps(paths []string, before []fileStamp) error {
	after, e := fileStamps(paths)
	if e != nil {
		return e
	}
	for i := range before {
		if before[i] != after[i] {
			return fmt.Errorf("input file changed during run: %s", paths[i])
		}
	}
	return nil
}
