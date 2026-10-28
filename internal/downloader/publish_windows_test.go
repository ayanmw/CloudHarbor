//go:build windows

package downloader

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPublishFileNoReplaceRequestsWriteThrough(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.tmp")
	destination := filepath.Join(dir, "result.bin")
	if err := os.WriteFile(source, []byte("durable result"), 0600); err != nil {
		t.Fatal(err)
	}

	called := false
	original := moveFileEx
	moveFileEx = func(from, to *uint16, flags uint32) error {
		called = true
		if flags != windows.MOVEFILE_WRITE_THROUGH {
			t.Errorf("MoveFileEx flags = %#x, want MOVEFILE_WRITE_THROUGH (%#x)", flags, windows.MOVEFILE_WRITE_THROUGH)
		}
		return original(from, to, flags)
	}
	t.Cleanup(func() { moveFileEx = original })

	if err := publishFileNoReplace(source, destination); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("MoveFileEx was not called")
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "durable result" {
		t.Fatalf("published content = %q, err=%v", got, err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists after move: %v", err)
	}
}
