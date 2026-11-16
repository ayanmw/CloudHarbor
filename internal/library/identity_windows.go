//go:build windows

package library

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"syscall"
)

func identity(f *os.File) (string, error) {
	var i windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(f.Fd()), &i); err != nil {
		return "", err
	}
	return fmt.Sprintf("%08x:%08x%08x", i.VolumeSerialNumber, i.FileIndexHigh, i.FileIndexLow), nil
}
func reparse(i os.FileInfo) bool {
	a, ok := i.Sys().(*syscall.Win32FileAttributeData)
	return ok && a.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
