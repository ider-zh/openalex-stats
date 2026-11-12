package cache

import (
	"fmt"
	"os"
	"path/filepath"
)

func lockDirectory(directory string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(directory, ".prepare.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockFile(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("cache directory is already in use: %w", err)
	}
	return func() { f.Close() }, nil
}
