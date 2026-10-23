//go:build !linux

package taskstore

import (
	"os"
	"path/filepath"
)

func openJobDirectory(name string) (*os.File, error) {
	if err := safePath(name); err != nil {
		return nil, err
	}
	return os.Open(name)
}

func openJobFileAt(dir *os.File, name string) (*os.File, error) {
	path := filepath.Join(dir.Name(), name)
	if err := safePath(path); err != nil {
		return nil, err
	}
	return os.Open(path)
}

func openJobFile(name string) (*os.File, error) {
	if err := safePath(name); err != nil {
		return nil, err
	}
	return os.Open(name)
}
