package library

import (
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"path"
	"unsafe"
)

type fileRenameInfo struct {
	ReplaceIfExists uint32
	RootDirectory   windows.Handle
	FileNameLength  uint32
	FileName        [1]uint16
}

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
	name, e := windows.NewNTUnicodeString(path.Base(source))
	if e != nil {
		return e
	}
	oa := &windows.OBJECT_ATTRIBUTES{Length: uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})), RootDirectory: windows.Handle(s.Fd()), ObjectName: name, Attributes: windows.OBJ_CASE_INSENSITIVE}
	var h windows.Handle
	var ios windows.IO_STATUS_BLOCK
	e = windows.NtCreateFile(&h, windows.DELETE|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE, oa, &ios, nil, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN, windows.FILE_OPEN_REPARSE_POINT|windows.FILE_OPEN_FOR_BACKUP_INTENT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0)
	if e != nil {
		return ntError(e)
	}
	f := os.NewFile(uintptr(h), source)
	defer f.Close()
	i, e := f.Stat()
	if e != nil {
		return e
	}
	if reparse(i) {
		return ErrUnsafe
	}
	id, e := identity(f)
	if e != nil || id != expected {
		return ErrChanged
	}
	text, e := windows.UTF16FromString(path.Base(target))
	if e != nil {
		return e
	}
	text = text[:len(text)-1]
	offset := int(unsafe.Offsetof(fileRenameInfo{}.FileName))
	buffer := make([]byte, offset+len(text)*2)
	info := (*fileRenameInfo)(unsafe.Pointer(&buffer[0]))
	info.RootDirectory = windows.Handle(d.Fd())
	info.FileNameLength = uint32(len(text) * 2)
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&buffer[offset])), len(text)), text)
	// ReplaceIfExists stays zero: a competing target creation must fail atomically.
	e = windows.NtSetInformationFile(h, &ios, &buffer[0], uint32(len(buffer)), windows.FileRenameInformation)
	return ntError(e)
}
func ntError(e error) error {
	if n, ok := e.(windows.NTStatus); ok {
		e = n.Errno()
	}
	if errors.Is(e, windows.ERROR_SHARING_VIOLATION) || errors.Is(e, windows.ERROR_LOCK_VIOLATION) {
		return fmt.Errorf("%w: 文件正被其他程序占用，请关闭占用后重试", ErrBusy)
	}
	if errors.Is(e, windows.ERROR_NOT_SAME_DEVICE) || errors.Is(e, windows.ERROR_NOT_SUPPORTED) {
		return fmt.Errorf("%w: 不支持跨文件系统或当前文件系统的无覆盖移动", errors.ErrUnsupported)
	}
	return e
}

// Match the project's durablefs Windows boundary: directory flush is not a
// portable power-loss guarantee. File data and SQLite intents are synced.
func syncParent(*os.Root, string) error { return nil }
