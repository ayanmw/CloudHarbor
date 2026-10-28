//go:build windows

package durablefs

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestReplaceFileRequestsWriteThroughAndReplacement(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "replacement.tmp")
	destination := filepath.Join(dir, "state.json")
	if err := os.WriteFile(source, []byte("new state"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old state"), 0600); err != nil {
		t.Fatal(err)
	}

	called := false
	original := replaceMoveFile
	replaceMoveFile = func(from, to *uint16, flags uint32) error {
		called = true
		want := uint32(windows.MOVEFILE_REPLACE_EXISTING | windows.MOVEFILE_WRITE_THROUGH)
		if flags != want {
			t.Errorf("MoveFileEx flags = %#x, want %#x", flags, want)
		}
		return original(from, to, flags)
	}
	t.Cleanup(func() { replaceMoveFile = original })

	if err := ReplaceFile(source, destination); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("MoveFileEx was not called")
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "new state" {
		t.Fatalf("replacement content = %q, err=%v", got, err)
	}
}
