//go:build !windows

package library

import (
	"fmt"
	"os"
	"syscall"
)

func identity(f *os.File) (string, error) {
	i, e := f.Stat()
	if e != nil {
		return "", e
	}
	s, ok := i.Sys().(*syscall.Stat_t)
	if !ok {
		return "", ErrUnsafe
	}
	return fmt.Sprintf("%d:%d", s.Dev, s.Ino), nil
}
func reparse(os.FileInfo) bool { return false }
