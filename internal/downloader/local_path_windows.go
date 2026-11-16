//go:build windows

package downloader

import (
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func localPathReparse(info os.FileInfo) bool {
	a, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && a.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

// Go 1.23 changed junction FileMode classification. EvalSymlinks consequently
// no longer resolves junctions with the new module defaults. Resolve the actual
// opened directory instead, so migration overlap checks retain their boundary.
func resolveExistingLocalPath(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buffer := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buffer[0], uint32(len(buffer)), 0)
		if err != nil {
			return "", err
		}
		if int(n) >= len(buffer) {
			buffer = make([]uint16, int(n)+1)
			continue
		}
		resolved := windows.UTF16ToString(buffer[:n])
		if strings.HasPrefix(resolved, `\\?\UNC\`) {
			return `\\` + strings.TrimPrefix(resolved, `\\?\UNC\`), nil
		}
		return strings.TrimPrefix(resolved, `\\?\`), nil
	}
}
