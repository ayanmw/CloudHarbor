package taskstore

import (
	"os"
	"syscall"
)

func statePathReparse(info os.FileInfo) bool {
	a, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && a.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
