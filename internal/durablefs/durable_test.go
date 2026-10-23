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

func TestWriteFilesAtomicPublishesBatchWithOneDirectorySync(t *testing.T) {
	dir := t.TempDir()
	var synced []string
	original := syncDirectory
	syncDirectory = func(path string) error {
		synced = append(synced, path)
		return nil
	}
	t.Cleanup(func() { syncDirectory = original })

	files := []File{
		{Path: filepath.Join(dir, "1.json"), Data: []byte(`{"id":1}`)},
		{Path: filepath.Join(dir, "2.json"), Data: []byte(`{"id":2}`)},
	}
	if err := WriteFilesAtomic(files); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(synced, []string{dir}) {
		t.Fatalf("directory sync calls = %q, want one sync of %q", synced, dir)
	}
	for _, file := range files {
		got, err := os.ReadFile(file.Path)
		if err != nil || !reflect.DeepEqual(got, file.Data) {
			t.Fatalf("published %q = %q, err=%v", file.Path, got, err)
		}
	}
}

func TestWriteFilesAtomicValidatesBatchBeforePublishing(t *testing.T) {
	dir := t.TempDir()
	err := WriteFilesAtomic([]File{
		{Path: filepath.Join(dir, "1.json"), Data: []byte(`{"id":1}`)},
		{Path: filepath.Join(dir, "2.json"), Data: []byte(`{"id":2}`)},
		{Path: filepath.Join(dir, "1.json"), Data: []byte(`{"id":3}`)},
	})
	if err == nil {
		t.Fatal("duplicate destination paths were accepted")
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("validation failure published files: %v", entries)
	}
}

func TestWriteFilesAtomicReturnsDirectorySyncFailureAfterRenames(t *testing.T) {
	dir := t.TempDir()
	wantErr := os.ErrPermission
	original := syncDirectory
	syncDirectory = func(string) error { return wantErr }
	t.Cleanup(func() { syncDirectory = original })

	err := WriteFilesAtomic([]File{
		{Path: filepath.Join(dir, "1.json"), Data: []byte(`{"id":1}`)},
		{Path: filepath.Join(dir, "2.json"), Data: []byte(`{"id":2}`)},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("directory sync error = %v, want %v", err, wantErr)
	}
	for _, name := range []string{"1.json", "2.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("record %s was not renamed before sync error: %v", name, err)
		}
	}
}
