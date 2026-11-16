//go:build windows

package taskstore

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func openJobDirectory(name string) (*os.File, error) {
	if err := safePath(name); err != nil {
		return nil, err
	}
	return os.Open(name)
}

func openJobFileAt(dir *os.File, name string) (*os.File, error) {
	return openJobFile(filepath.Join(dir.Name(), name))
}

func openJobFile(name string) (*os.File, error) {
	// Open the path once without following a reparse point, then inspect the
	// opened handle. This keeps the symlink check tied to the file we read.
	h, err := syscall.Open(name, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(h), name)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || statePathReparse(info) {
		f.Close()
		return nil, fmt.Errorf("state path is a symlink: %s", name)
	}
	return f, nil
}
