//go:build windows

package downloader

import "golang.org/x/sys/windows"

var moveFileEx = windows.MoveFileEx

func publishFileNoReplace(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	// The write-through flag asks Windows to flush a successful move before
	// returning; omitting REPLACE_EXISTING preserves no-overwrite publication.
	return moveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}
