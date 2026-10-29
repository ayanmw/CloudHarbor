package downloader

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/taskstore"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
)

func waitForLocalMigration(t *testing.T, d *Downloader, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		migration, err := d.LocalDirectoryMigrationStatus()
		if err == nil && migration.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	migration, err := d.LocalDirectoryMigrationStatus()
	t.Fatalf("local migration did not reach %s: migration=%+v err=%v", want, migration, err)
}

func TestLocalDirectoryMigrationVerifiesThenRemovesSource(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "downloads")
	target := filepath.Join(root, "new-downloads")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	content := []byte("persisted download content")
	if err := os.WriteFile(filepath.Join(source, "nested", "file.bin"), content, 0640); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDownloader(&config.Config{QueueDir: "/queue", LocalDownDir: source, StateDir: filepath.Join(root, "state"), MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	migration, err := d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/new-queue", LocalDownDir: target}, true)
	if err != nil {
		t.Fatal(err)
	}
	if migration.ID == "" {
		t.Fatal("expected a durable migration record")
	}
	waitForLocalMigration(t, d, "completed")
	if _, err = os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source directory remains after verified migration: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(target, "nested", "file.bin"))
	if err != nil || string(got) != string(content) {
		t.Fatalf("target file mismatch: content=%q err=%v", got, err)
	}
	if paths := d.GetManagedPaths(); paths.QueueDir != "/new-queue" || !sameLocalPath(paths.LocalDownDir, target) {
		t.Fatalf("completed migration did not activate paths: %+v", paths)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = d.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestLocalDirectoryMigrationConflictPreservesSource(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "old"), filepath.Join(root, "new")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "same.txt"), []byte("source"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "same.txt"), []byte("different"), 0644); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDownloader(&config.Config{QueueDir: "/queue", LocalDownDir: source, StateDir: filepath.Join(root, "state"), MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	if _, err = d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/new-queue", LocalDownDir: target}, true); err != nil {
		t.Fatal(err)
	}
	waitForLocalMigration(t, d, "failed")
	if _, err = os.Stat(source); err != nil {
		t.Fatalf("source directory must remain after conflict: %v", err)
	}
	migration, err := d.LocalDirectoryMigrationStatus()
	if err != nil || migration.LastError == "" {
		t.Fatalf("failure evidence missing: migration=%+v err=%v", migration, err)
	}
}

func TestLocalDirectoryMigrationResumesAfterRestart(t *testing.T) {
	root := t.TempDir()
	source, target, state := filepath.Join(root, "old"), filepath.Join(root, "new"), filepath.Join(root, "state")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("resume after restart"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: source, StateDir: state, MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := newSourceOperationID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	migration := taskstore.LocalDirectoryMigration{ID: id, SourcePath: source, TargetPath: target, DesiredQueuePath: "/resumed-queue", TombstonePath: source + ".nas-migration-" + id, Status: "running", Phase: "copying", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = d.queue.store.CreateLocalDirectoryMigration(migration); err != nil {
		t.Fatal(err)
	}
	if err = d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	d, err = OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	if err = d.ResumeLocalDirectoryMigration(); err != nil {
		t.Fatal(err)
	}
	waitForLocalMigration(t, d, "completed")
	if got, err := os.ReadFile(filepath.Join(target, "file.txt")); err != nil || string(got) != "resume after restart" {
		t.Fatalf("resumed migration did not preserve data: %q err=%v", got, err)
	}
	if _, err = os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("resumed migration did not retire source: %v", err)
	}
	if err = d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}
