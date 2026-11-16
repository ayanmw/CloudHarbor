//go:build linux

package library

import (
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"path"
)

func renameNoReplace(r *os.Root, source, target, expected, expectedParent string) error {
	s, e := internalOpen(r, rootName(parentPath(source)))
	if e != nil {
		return e
	}
	defer s.Close()
	d, e := internalOpen(r, rootName(parentPath(target)))
	if e != nil {
		return e
	}
	defer d.Close()
	parentID, e := identity(d)
	if e != nil || parentID != expectedParent {
		return ErrChanged
	}
	f, e := internalOpen(r, source)
	if e != nil {
		return e
	}
	id, e := identity(f)
	f.Close()
	if e != nil || id != expected {
		return ErrChanged
	}
	e = unix.Renameat2(int(s.Fd()), path.Base(source), int(d.Fd()), path.Base(target), unix.RENAME_NOREPLACE)
	if errors.Is(e, unix.ENOSYS) || errors.Is(e, unix.EOPNOTSUPP) || errors.Is(e, unix.EXDEV) {
		return fmt.Errorf("%w: 不支持跨文件系统或当前文件系统的无覆盖移动", errors.ErrUnsupported)
	}
	return e
}
func syncParent(r *os.Root, p string) error {
	f, e := internalOpen(r, p)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
