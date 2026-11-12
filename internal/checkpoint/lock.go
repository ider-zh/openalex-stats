package checkpoint

import (
	"fmt"
	"os"
	"path/filepath"
)

// LockDirectory is released by the kernel even after an ungraceful process exit.
func LockDirectory(directory string) (func(), error) {
	if e := os.MkdirAll(directory, 0755); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(directory, ".run.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = lockFile(f); e != nil {
		_ = f.Close()
		return nil, fmt.Errorf("run directory already in use: %w", e)
	}
	return func() { _ = f.Close() }, nil
}
