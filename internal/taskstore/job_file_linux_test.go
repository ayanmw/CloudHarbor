//go:build linux

package taskstore

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenJobFileRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	link := filepath.Join(dir, "1.json")
	want := []byte(`{"version":1}`)
	if err := os.WriteFile(target, want, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	file, err := openJobFile(link)
	if err == nil {
		file.Close()
		t.Fatal("opened a symlinked job record")
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != string(want) {
		t.Fatalf("symlink target changed: data=%q err=%v", got, err)
	}
}

func TestOpenJobFileReadsRegularRecord(t *testing.T) {
	name := filepath.Join(t.TempDir(), "1.json")
	want := []byte(`{"version":1}`)
	if err := os.WriteFile(name, want, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := openJobFile(name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	got, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("read %q, want %q", got, want)
	}
}

func TestLoadRejectsSymlinkJobRecord(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Save(1, []byte(`{"status":"done"}`)); err != nil {
		t.Fatal(err)
	}
	job := filepath.Join(dir, "jobs", "1.json")
	if err := os.Remove(job); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(target, []byte(`{"status":"done"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, job); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("loaded a symlinked job record")
	}
}

func BenchmarkOpenJobFile(b *testing.B) {
	name := filepath.Join(b.TempDir(), "1.json")
	if err := os.WriteFile(name, []byte(`{"version":1,"status":"done"}`), 0600); err != nil {
		b.Fatal(err)
	}
	for _, mode := range []string{"lstat_then_open", "open_nofollow"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var f *os.File
				var err error
				if mode == "lstat_then_open" {
					if err = safePath(name); err == nil {
						f, err = os.Open(name)
					}
				} else {
					f, err = openJobFile(name)
				}
				if err != nil {
					b.Fatal(err)
				}
				if _, err = io.Copy(io.Discard, f); err != nil {
					f.Close()
					b.Fatal(err)
				}
				if err = f.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
