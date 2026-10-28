package durablefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublishNoReplaceMovesSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "verified.tmp")
	destination := filepath.Join(dir, "queue.sqlite")
	if err := os.WriteFile(source, []byte("verified database"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PublishNoReplace(source, destination); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "verified database" {
		t.Fatalf("published content = %q, err=%v", got, err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source remains after publication: %v", err)
	}
}

func TestPublishNoReplacePreservesExistingDestination(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "verified.tmp")
	destination := filepath.Join(dir, "queue.sqlite")
	if err := os.WriteFile(source, []byte("new database"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("existing database"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PublishNoReplace(source, destination); err == nil {
		t.Fatal("publication replaced an existing destination")
	}
	if got, err := os.ReadFile(destination); err != nil || string(got) != "existing database" {
		t.Fatalf("existing destination changed: %q err=%v", got, err)
	}
	if got, err := os.ReadFile(source); err != nil || string(got) != "new database" {
		t.Fatalf("source was not preserved after collision: %q err=%v", got, err)
	}
}
