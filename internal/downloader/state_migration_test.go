package downloader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareStateDirectoryCopiesVerifiesAndRetiresLegacy(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "downloads", ".nas-downloader", "queue")
	destination := filepath.Join(root, "app", ".nas-downloader", "queue")
	if err := os.MkdirAll(legacy, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "queue.sqlite"), []byte("sqlite bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareStateDirectory(destination, legacy); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(destination, "queue.sqlite"))
	if err != nil || string(body) != "sqlite bytes" {
		t.Fatalf("state copy: %q, %v", body, err)
	}
	if _, err = os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("verified legacy state should be removed so it cannot diverge: %v", err)
	}
	if err := os.WriteFile(filepath.Join(destination, "queue.sqlite"), []byte("updated state"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareStateDirectory(destination, legacy); err != nil {
		t.Fatalf("later startup should use the stable store after legacy retirement: %v", err)
	}
}

func TestPrepareStateDirectoryRejectsDifferentExistingHistories(t *testing.T) {
	root := t.TempDir()
	legacy, destination := filepath.Join(root, "legacy"), filepath.Join(root, "stable")
	for dir, data := range map[string]string{legacy: "old", destination: "new"} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "queue.sqlite"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := PrepareStateDirectory(destination, legacy); err == nil {
		t.Fatal("expected conflicting histories to fail closed")
	}
}
