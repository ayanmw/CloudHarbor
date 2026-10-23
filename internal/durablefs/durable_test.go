package durablefs

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMkdirAllSyncsCreatedParentEntriesFromRootToLeaf(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "state", "queue", "jobs")
	var synced []string
	original := syncDirectory
	syncDirectory = func(path string) error {
		synced = append(synced, path)
		return nil
	}
	t.Cleanup(func() { syncDirectory = original })
	if err := MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	want := []string{root, filepath.Join(root, "state"), filepath.Join(root, "state", "queue")}
	want = append([]string{filepath.Dir(root)}, want...)
	if !reflect.DeepEqual(synced, want) {
		t.Fatalf("synced parent directories %q, want %q", synced, want)
	}
}

func TestMkdirAllSyncsParentOfExistingDirectory(t *testing.T) {
	target := t.TempDir()
	var synced []string
	original := syncDirectory
	syncDirectory = func(path string) error { synced = append(synced, path); return nil }
	t.Cleanup(func() { syncDirectory = original })
	if err := MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(synced, []string{filepath.Dir(target)}) {
		t.Fatalf("synced directories %q, want parent %q", synced, filepath.Dir(target))
	}
}

func TestMkdirAllReturnsDirectorySyncFailure(t *testing.T) {
	root := t.TempDir()
	original := syncDirectory
	wantErr := os.ErrPermission
	syncDirectory = func(string) error { return wantErr }
	t.Cleanup(func() { syncDirectory = original })
	if err := MkdirAll(filepath.Join(root, "state"), 0700); !errors.Is(err, wantErr) {
		t.Fatalf("directory sync error lost: %v", err)
	}
}
