//go:build windows

package durablefs

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPublishNoReplaceRequestsWriteThroughWithoutReplacement(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "verified.tmp")
	destination := filepath.Join(dir, "queue.sqlite")
	if err := os.WriteFile(source, []byte("verified database"), 0600); err != nil {
		t.Fatal(err)
	}

	called := false
	original := publishMoveFile
	publishMoveFile = func(from, to *uint16, flags uint32) error {
		called = true
		if flags != windows.MOVEFILE_WRITE_THROUGH {
			t.Errorf("MoveFileEx flags = %#x, want MOVEFILE_WRITE_THROUGH (%#x)", flags, windows.MOVEFILE_WRITE_THROUGH)
		}
		return original(from, to, flags)
	}
	t.Cleanup(func() { publishMoveFile = original })

	if err := PublishNoReplace(source, destination); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("MoveFileEx was not called")
	}
}
