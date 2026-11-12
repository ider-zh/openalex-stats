// Package checkpoint writes and validates immutable stage artifacts.
package checkpoint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Artifact struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

func Hash(path string) (Artifact, error) {
	f, e := os.Open(path)
	if e != nil {
		return Artifact{}, e
	}
	defer f.Close()
	h := sha256.New()
	n, e := io.Copy(h, f)
	if e != nil {
		return Artifact{}, e
	}
	return Artifact{Path: path, Bytes: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}
func (a Artifact) Verify() error {
	got, e := Hash(a.Path)
	if e != nil {
		return e
	}
	if got.Bytes != a.Bytes || got.SHA256 != a.SHA256 {
		return fmt.Errorf("artifact checksum mismatch: %s", a.Path)
	}
	return nil
}

// Write publishes the data file only after a completed write and fsync. The
// caller publishes its keyed JSON checkpoint after this succeeds.
func Write(path string, write func(io.Writer) error) (artifact Artifact, err error) {
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return artifact, e
	}
	f, e := os.Create(path + ".tmp")
	if e != nil {
		return artifact, e
	}
	h := sha256.New()
	counter := &countingWriter{w: io.MultiWriter(f, h)}
	if err = write(counter); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return artifact, err
	}
	if closeErr != nil {
		return artifact, closeErr
	}
	if err = os.Rename(path+".tmp", path); err != nil {
		return artifact, err
	}
	syncDirectory(filepath.Dir(path))
	return Artifact{Path: path, Bytes: counter.n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}
func JSON(path string, value any) error {
	_, e := Write(path, func(w io.Writer) error {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	})
	return e
}
func ReadJSON(path string, value any) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(value)
}
func Key(value any) (string, error) {
	b, e := json.Marshal(value)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func syncDirectory(path string) {
	if d, e := os.Open(path); e == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, e := w.w.Write(p)
	w.n += int64(n)
	return n, e
}
