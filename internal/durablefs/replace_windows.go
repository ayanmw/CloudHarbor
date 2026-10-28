//go:build windows

package durablefs

import "golang.org/x/sys/windows"

var replaceMoveFile = windows.MoveFileEx

func platformReplaceFile(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	// Atomic state-file replacements need replacement semantics and Windows
	// write-through; this is separate from download publication, which forbids it.
	return replaceMoveFile(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
