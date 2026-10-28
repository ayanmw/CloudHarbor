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

	jobsDir, err := openJobDirectory(filepath.Dir(link))
	if err != nil {
		t.Fatal(err)
	}
	defer jobsDir.Close()
	file, err := openJobFileAt(jobsDir, filepath.Base(link))
	if err == nil {
		file.Close()
		t.Fatal("opened a symlinked job record")
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != string(want) {
		t.Fatalf("symlink target changed: data=%q err=%v", got, err)
	}
}

func TestOpenJobDirectoryRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real-jobs")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "jobs-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if dir, err := openJobDirectory(link); err == nil {
		dir.Close()
		t.Fatal("opened a symlinked jobs directory")
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

func TestOpenRejectsSymlinkDatabase(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(dir, "queue.sqlite")
	if err := os.Remove(database); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "outside.sqlite")
	if err := os.WriteFile(target, []byte("not a database"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, database); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if reopened, err := Open(dir, "/preview"); err == nil {
		reopened.Close()
		t.Fatal("opened a symlinked database")
	}
}

func BenchmarkOpenJobFile(b *testing.B) {
	name := filepath.Join(b.TempDir(), "1.json")
	if err := os.WriteFile(name, []byte(`{"version":1,"status":"done"}`), 0600); err != nil {
		b.Fatal(err)
	}
	jobsDir, err := openJobDirectory(filepath.Dir(name))
	if err != nil {
		b.Fatal(err)
	}
	defer jobsDir.Close()
	for _, mode := range []string{"lstat_then_open", "open_nofollow", "openat_nofollow"} {
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
					if mode == "openat_nofollow" {
						f, err = openJobFileAt(jobsDir, filepath.Base(name))
					} else {
						f, err = openJobFile(name)
					}
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
