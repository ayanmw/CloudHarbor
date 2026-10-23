//go:build windows

package taskstore

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenJobFileAtReadsRegularRecord(t *testing.T) {
	jobs := t.TempDir()
	name := filepath.Join(jobs, "1.json")
	want := []byte(`{"version":1}`)
	if err := os.WriteFile(name, want, 0600); err != nil {
		t.Fatal(err)
	}
	dir, err := openJobDirectory(jobs)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	f, err := openJobFileAt(dir, filepath.Base(name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil || string(got) != string(want) {
		t.Fatalf("read %q, err=%v; want %q", got, err, want)
	}
}

func TestOpenJobFileAtRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	jobs := filepath.Join(root, "jobs")
	if err := os.Mkdir(jobs, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "outside.json")
	if err := os.WriteFile(target, []byte(`{"outside":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(jobs, "1.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	dir, err := openJobDirectory(jobs)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if f, err := openJobFileAt(dir, filepath.Base(link)); err == nil {
		f.Close()
		t.Fatal("opened a symlinked job record")
	}
}

func BenchmarkOpenJobFileWindows(b *testing.B) {
	name := filepath.Join(b.TempDir(), "1.json")
	if err := os.WriteFile(name, []byte(`{"version":1,"status":"done"}`), 0600); err != nil {
		b.Fatal(err)
	}
	dir, err := openJobDirectory(filepath.Dir(name))
	if err != nil {
		b.Fatal(err)
	}
	defer dir.Close()
	for _, mode := range []string{"lstat_then_open", "open_job_file_at"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var f *os.File
				if mode == "lstat_then_open" {
					if err := safePath(name); err != nil {
						b.Fatal(err)
					}
					f, err = os.Open(name)
				} else {
					f, err = openJobFileAt(dir, filepath.Base(name))
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
