//go:build windows

package durablefs

import "golang.org/x/sys/windows"

var publishMoveFile = windows.MoveFileEx

func platformPublishNoReplace(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	// Omitting REPLACE_EXISTING preserves no-overwrite publication; write-through
	// makes a successful migration or download move wait for Windows to flush it.
	return publishMoveFile(from, to, windows.MOVEFILE_WRITE_THROUGH)
}
