package downloader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/taskstore"

	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/config"
)

func TestCopyMigrationFileRemovesStalePartWhenTargetAlreadyMatches(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.bin")
	target := filepath.Join(root, "target.bin")
	content := []byte("already copied before restart")
	if err := os.WriteFile(source, content, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, content, 0644); err != nil {
		t.Fatal(err)
	}
	const migrationID = "migration-after-crash"
	nameHash := sha256.Sum256([]byte(target))
	part := filepath.Join(root, ".nas-migrate-"+migrationID+"-"+hex.EncodeToString(nameHash[:8])+".part")
	if err := os.WriteFile(part, content, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = copyMigrationFile(context.Background(), source, target, migrationID, info); err != nil {
		t.Fatalf("resume copy with identical target: %v", err)
	}
	if _, err = os.Lstat(part); !os.IsNotExist(err) {
		t.Fatalf("stale migration part must be removed after recognizing the completed target: %v", err)
	}
}

func TestCopyMigrationFileResumesPartialTargetFromVerifiedMigrationPart(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.bin")
	target := filepath.Join(root, "target.bin")
	content := []byte("complete staged contents after interrupted fallback copy")
	if err := os.WriteFile(source, content, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, content[:17], 0644); err != nil {
		t.Fatal(err)
	}
	const migrationID = "migration-partial-fallback"
	nameHash := sha256.Sum256([]byte(target))
	part := filepath.Join(root, ".nas-migrate-"+migrationID+"-"+hex.EncodeToString(nameHash[:8])+".part")
	if err := os.WriteFile(part, content, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = copyMigrationFile(context.Background(), source, target, migrationID, info); err != nil {
		t.Fatalf("resume migration from verified staged contents: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != string(content) {
		t.Fatalf("resumed target mismatch: content=%q err=%v", got, err)
	}
	if _, err = os.Lstat(part); !os.IsNotExist(err) {
		t.Fatalf("staged migration part must be removed after recovery: %v", err)
	}
}

func TestCopyMigrationFilePreservesNonPrefixTargetEvenWithVerifiedPart(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.bin")
	target := filepath.Join(root, "target.bin")
	content := []byte("verified staged contents")
	conflict := []byte("unrelated target data")
	if err := os.WriteFile(source, content, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, conflict, 0644); err != nil {
		t.Fatal(err)
	}
	const migrationID = "migration-target-conflict"
	nameHash := sha256.Sum256([]byte(target))
	part := filepath.Join(root, ".nas-migrate-"+migrationID+"-"+hex.EncodeToString(nameHash[:8])+".part")
	if err := os.WriteFile(part, content, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = copyMigrationFile(context.Background(), source, target, migrationID, info); err == nil {
		t.Fatal("expected unrelated target content to remain a conflict")
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != string(conflict) {
		t.Fatalf("conflicting target was changed: content=%q err=%v", got, err)
	}
}

func TestCopyMigrationFileResumesAfterTargetSyncFailure(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.bin")
	target := filepath.Join(root, "target.bin")
	content := []byte("verified staged bytes survive target sync failure")
	if err := os.WriteFile(source, content, 0644); err != nil {
		t.Fatal(err)
	}
	const migrationID = "migration-sync-failure"
	nameHash := sha256.Sum256([]byte(target))
	part := filepath.Join(root, ".nas-migrate-"+migrationID+"-"+hex.EncodeToString(nameHash[:8])+".part")
	if err := os.WriteFile(part, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := copyMigrationPartToExclusiveTarget(context.Background(), target, part, 0644, func(file *os.File) error {
		if err := file.Truncate(9); err != nil {
			return err
		}
		return errors.New("simulated target sync failure")
	}, func(file *os.File) error {
		return file.Close()
	}, os.Remove); err == nil {
		t.Fatal("expected simulated target sync failure")
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = copyMigrationFile(context.Background(), source, target, migrationID, info); err != nil {
		t.Fatalf("retry should restore the target from the verified staged part: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("retry did not restore complete target: got=%q err=%v", got, err)
	}
}

func TestCopyMigrationFileResumesAfterTargetCloseFailure(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.bin")
	target := filepath.Join(root, "target.bin")
	content := []byte("verified staged bytes survive target close failure")
	if err := os.WriteFile(source, content, 0644); err != nil {
		t.Fatal(err)
	}
	const migrationID = "migration-close-failure"
	nameHash := sha256.Sum256([]byte(target))
	part := filepath.Join(root, ".nas-migrate-"+migrationID+"-"+hex.EncodeToString(nameHash[:8])+".part")
	if err := os.WriteFile(part, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := copyMigrationPartToExclusiveTarget(context.Background(), target, part, 0644, func(file *os.File) error {
		return nil
	}, func(file *os.File) error {
		if err := file.Truncate(11); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		return errors.New("simulated target close failure")
	}, os.Remove); err == nil {
		t.Fatal("expected simulated target close failure")
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = copyMigrationFile(context.Background(), source, target, migrationID, info); err != nil {
		t.Fatalf("retry should restore the target from the verified staged part: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("retry did not restore complete target: got=%q err=%v", got, err)
	}
}

func TestCopyMigrationFileRetainsPartWhenCopyAndTargetCleanupFail(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source.bin")
	target := filepath.Join(root, "target.bin")
	content := bytes.Repeat([]byte("verified-copy-data"), 160000)
	if err := os.WriteFile(source, content, 0644); err != nil {
		t.Fatal(err)
	}
	const migrationID = "migration-copy-cleanup-failure"
	nameHash := sha256.Sum256([]byte(target))
	part := filepath.Join(root, ".nas-migrate-"+migrationID+"-"+hex.EncodeToString(nameHash[:8])+".part")
	if err := os.WriteFile(part, content, 0600); err != nil {
		t.Fatal(err)
	}
	copyCtx := &cancelAfterMigrationCopyChunk{Context: context.Background()}
	if err := copyMigrationPartToExclusiveTarget(copyCtx, target, part, 0644, func(file *os.File) error {
		return nil
	}, func(file *os.File) error {
		return file.Close()
	}, func(string) error {
		return errors.New("simulated target lock prevents cleanup")
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected interrupted target copy, got %v", err)
	}
	if _, err := os.Stat(part); err != nil {
		t.Fatalf("verified part must remain when incomplete target cannot be removed: %v", err)
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if err = copyMigrationFile(context.Background(), source, target, migrationID, info); err != nil {
		t.Fatalf("retry should repair locked partial target from the verified part: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("retry did not restore complete target: got=%d bytes err=%v", len(got), err)
	}
}

type cancelAfterMigrationCopyChunk struct {
	context.Context
	calls int
}

func (c *cancelAfterMigrationCopyChunk) Err() error {
	c.calls++
	if c.calls >= 2 {
		return context.Canceled
	}
	return nil
}

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
	queued := file(901, "queued.txt", 3)
	queuedJob := &queueJob{Version: 1, Info: queued, Mode: DownloadModeKeep, Status: "pending", Created: time.Now()}
	d.queue.mu.Lock()
	d.queue.jobs[queued.FsID] = queuedJob
	d.queue.eligible[queued.FsID] = retainFileInfo(queued)
	d.queue.enqueueCandidate(queuedJob)
	d.queue.mu.Unlock()
	impact, err := d.PreviewManagedPathChange("/new-queue")
	if err != nil {
		t.Fatal(err)
	}
	migration, err := d.SetManagedPathsConfirmed(context.Background(), ManagedPaths{QueueDir: "/new-queue", LocalDownDir: target}, true, impact.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if migration.ID == "" {
		t.Fatal("expected a durable migration record")
	}
	waitForLocalMigration(t, d, "completed")
	if snapshot := d.GetManagedPathSnapshot(); snapshot.QueueSource != "saved" || snapshot.LocalDownloadSource != "saved" {
		t.Fatalf("migration did not activate saved sources: %#v", snapshot)
	}
	d.queue.mu.Lock()
	staleCandidate := queuedJob.candidate != nil || d.queue.eligible[queued.FsID] != nil
	d.queue.mu.Unlock()
	if staleCandidate {
		t.Fatal("local migration retained queue eligibility from the old monitoring directory")
	}
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
	defer d.Shutdown(context.Background())
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

func TestLocalDirectoryMigrationRechecksTargetBeforeDeletingRetiredSource(t *testing.T) {
	root := t.TempDir()
	source, target, state := filepath.Join(root, "old"), filepath.Join(root, "new"), filepath.Join(root, "state")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	id, err := newSourceOperationID()
	if err != nil {
		t.Fatal(err)
	}
	tombstone := source + ".nas-migration-" + id
	if err = os.MkdirAll(tombstone, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(tombstone, "file.txt"), []byte("last verified source data"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(target, "file.txt"), []byte("last verified source data"), 0644); err != nil {
		t.Fatal(err)
	}
	verifiedTargetSHA256, err := hashLocalDirectory(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(target, "file.txt"), []byte("changed after shutdown"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: source, StateDir: state, MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	now := time.Now().UTC()
	migration := taskstore.LocalDirectoryMigration{
		ID: id, SourcePath: source, TargetPath: target, DesiredQueuePath: "/next-queue",
		TombstonePath: tombstone, Status: "running", Phase: "deleting_source",
		VerifiedTargetSHA256: verifiedTargetSHA256,
		StartedAtUTC:         now, UpdatedAtUTC: now,
	}
	if err = d.queue.store.CreateLocalDirectoryMigration(migration); err != nil {
		t.Fatal(err)
	}
	if err = d.ResumeLocalDirectoryMigration(); err != nil {
		t.Fatalf("resume deletion phase: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current, statusErr := d.LocalDirectoryMigrationStatus()
		if statusErr == nil && current.Status != "running" {
			if current.Status != "failed" {
				t.Fatalf("migration must fail safely after target changed, got status=%q", current.Status)
			}
			if _, err = os.Stat(filepath.Join(tombstone, "file.txt")); err != nil {
				t.Fatalf("retired source data was deleted despite target verification failure: %v", err)
			}
			if err = os.WriteFile(filepath.Join(target, "file.txt"), []byte("last verified source data"), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err = d.beginLocalDirectoryMigration(source, target, "/next-queue"); err != nil {
				t.Fatalf("retry deletion after repairing target contents: %v", err)
			}
			waitForLocalMigration(t, d, "completed")
			if _, err = os.Lstat(tombstone); !os.IsNotExist(err) {
				t.Fatalf("repaired migration did not remove its verified source tombstone: %v", err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	current, statusErr := d.LocalDirectoryMigrationStatus()
	t.Fatalf("migration did not finish its safety check: migration=%+v err=%v", current, statusErr)
}

func TestLocalDirectoryMigrationCanRetrySettingsAfterSourceCleanup(t *testing.T) {
	root := t.TempDir()
	source, target, state := filepath.Join(root, "old"), filepath.Join(root, "new"), filepath.Join(root, "state")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	verifiedTargetSHA256, err := hashLocalDirectory(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	d, err := OpenDownloader(&config.Config{QueueDir: "/queue", LocalDownDir: source, StateDir: state, MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	id, err := newSourceOperationID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	migration := taskstore.LocalDirectoryMigration{
		ID: id, SourcePath: source, TargetPath: target, DesiredQueuePath: "/next-queue",
		TombstonePath: source + ".nas-migration-" + id, Status: "running", Phase: "deleting_source",
		VerifiedTargetSHA256: verifiedTargetSHA256,
		StartedAtUTC:         now, UpdatedAtUTC: now,
	}
	if err = d.queue.store.CreateLocalDirectoryMigration(migration); err != nil {
		t.Fatal(err)
	}
	migration.Status, migration.LastError = "failed", "源目录已核验清理，但路径设置保存失败: simulated settings write failure"
	migration.FinishedAtUTC, migration.UpdatedAtUTC = now, now
	if err = d.queue.store.UpdateLocalDirectoryMigration(migration, true); err != nil {
		t.Fatal(err)
	}
	if _, err = d.beginLocalDirectoryMigration(source, target, "/next-queue"); err != nil {
		t.Fatalf("retry path activation after verified source cleanup: %v", err)
	}
	waitForLocalMigration(t, d, "completed")
	if paths := d.GetManagedPaths(); !sameLocalPath(paths.LocalDownDir, target) || paths.QueueDir != "/next-queue" {
		t.Fatalf("managed paths were not activated by retry: %+v", paths)
	}
}

func TestLocalDirectoryMigrationRetriesAfterRetirementCheckpointFailure(t *testing.T) {
	root := t.TempDir()
	source, target, state := filepath.Join(root, "old"), filepath.Join(root, "new"), filepath.Join(root, "state")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	content := []byte("recover after retirement checkpoint failure")
	if err := os.WriteFile(filepath.Join(source, "file.txt"), content, 0644); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDownloader(&config.Config{QueueDir: "/queue", LocalDownDir: source, StateDir: state, MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	db, err := sql.Open("sqlite", filepath.Join(state, "queue", "queue.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TRIGGER fail_source_retired_checkpoint BEFORE UPDATE ON local_directory_migrations WHEN NEW.phase='source_retired' AND NEW.status='running' BEGIN SELECT RAISE(ABORT, 'simulated checkpoint failure'); END`); err != nil {
		t.Fatal(err)
	}
	first, err := d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/next-queue", LocalDownDir: target}, true)
	if err != nil {
		t.Fatal(err)
	}
	waitForLocalMigration(t, d, "failed")
	failed, err := d.LocalDirectoryMigrationStatus()
	if err != nil {
		t.Fatal(err)
	}
	if failed.Phase != "source_retired" {
		t.Fatalf("expected retirement checkpoint to fail after source rename, got migration=%+v", failed)
	}
	tombstone := source + ".nas-migration-" + first.ID
	if _, err = os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source should have been retired before the injected checkpoint failure: %v", err)
	}
	if _, err = os.Stat(filepath.Join(tombstone, "file.txt")); err != nil {
		t.Fatalf("retired source data must remain recoverable: %v", err)
	}
	if _, err = db.Exec(`DROP TRIGGER fail_source_retired_checkpoint`); err != nil {
		t.Fatal(err)
	}
	if _, err = d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/next-queue", LocalDownDir: target}, true); err != nil {
		t.Fatalf("retry after retirement checkpoint failure: %v", err)
	}
	waitForLocalMigration(t, d, "completed")
	if _, err = os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source path unexpectedly reappeared after recovery: %v", err)
	}
	if _, err = os.Stat(tombstone); !os.IsNotExist(err) {
		t.Fatalf("verified tombstone was not cleaned after recovery: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(target, "file.txt"))
	if err != nil || string(got) != string(content) {
		t.Fatalf("target content changed during recovery: %q err=%v", got, err)
	}
}

func TestLegacyDeletingSourceWithoutTargetDigestFailsClosed(t *testing.T) {
	root := t.TempDir()
	source, target, state := filepath.Join(root, "old"), filepath.Join(root, "new"), filepath.Join(root, "state")
	id, err := newSourceOperationID()
	if err != nil {
		t.Fatal(err)
	}
	tombstone := expectedLocalMigrationTombstone(source, id)
	for _, directory := range []string{target, tombstone} {
		if err = os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(directory, "remaining.bin"), []byte("only remaining copy"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	d, err := OpenDownloader(&config.Config{QueueDir: "/queue", LocalDownDir: source, StateDir: state, MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	now := time.Now().UTC()
	migration := taskstore.LocalDirectoryMigration{
		ID: id, SourcePath: source, TargetPath: target, DesiredQueuePath: "/queue",
		TombstonePath: tombstone, Status: "running", Phase: "deleting_source",
		TotalFiles: 2, TotalBytes: int64(len("remaining copy") + len("already removed")),
		StartedAtUTC: now, UpdatedAtUTC: now,
	}
	if err = d.queue.store.CreateLocalDirectoryMigration(migration); err != nil {
		t.Fatal(err)
	}
	if err = d.ResumeLocalDirectoryMigration(); err != nil {
		t.Fatal(err)
	}
	waitForLocalMigration(t, d, "failed")
	status, err := d.LocalDirectoryMigrationStatus()
	if err != nil || status.Phase != "deleting_source" || !strings.Contains(status.LastError, "指纹") {
		t.Fatalf("legacy checkpoint without a target snapshot must fail closed: status=%+v err=%v", status, err)
	}
	if _, err = os.Stat(filepath.Join(tombstone, "remaining.bin")); err != nil {
		t.Fatalf("recovery deleted residual source data without a durable target snapshot: %v", err)
	}
}

func TestLocalDirectoryMigrationResumesAfterPartialTombstoneRemovalAndRestart(t *testing.T) {
	root := t.TempDir()
	source, target, state := filepath.Join(root, "old"), filepath.Join(root, "new"), filepath.Join(root, "state")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	contentA, contentB := []byte("verified file A"), []byte("verified file B")
	if err := os.WriteFile(filepath.Join(source, "a.bin"), contentA, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "b.bin"), contentB, 0644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: source, StateDir: state, MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	partialDeleteErr := errors.New("simulated partial tombstone cleanup")
	d.localMigrationRemoveAllHook = func(name string) error {
		if err := os.Remove(filepath.Join(name, "a.bin")); err != nil {
			return err
		}
		return partialDeleteErr
	}
	if _, err = d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/queue", LocalDownDir: target}, true); err != nil {
		t.Fatal(err)
	}
	waitForLocalMigration(t, d, "failed")
	failed, err := d.LocalDirectoryMigrationStatus()
	if err != nil || failed.Phase != "deleting_source" || failed.VerifiedTargetSHA256 == "" {
		t.Fatalf("partial cleanup must retain its deletion checkpoint proof: migration=%+v err=%v", failed, err)
	}
	tombstone := failed.TombstonePath
	if _, err = os.Stat(filepath.Join(tombstone, "b.bin")); err != nil {
		t.Fatalf("partial cleanup should leave residual source available: %v", err)
	}
	if err = d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	d, err = OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	if _, err = d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/queue", LocalDownDir: target}, true); err != nil {
		t.Fatalf("restart should retry verified partial cleanup: %v", err)
	}
	waitForLocalMigration(t, d, "completed")
	for name, want := range map[string][]byte{"a.bin": contentA, "b.bin": contentB} {
		got, readErr := os.ReadFile(filepath.Join(target, name))
		if readErr != nil || !bytes.Equal(got, want) {
			t.Fatalf("target %s changed during resumed cleanup: %q err=%v", name, got, readErr)
		}
	}
	if _, err = os.Stat(tombstone); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("verified residual tombstone was not cleaned: %v", err)
	}
}

func TestLocalDirectoryMigrationPreservesPartialTombstoneWhenTargetChanges(t *testing.T) {
	root := t.TempDir()
	source, target, state := filepath.Join(root, "old"), filepath.Join(root, "new"), filepath.Join(root, "state")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "a.bin"), []byte("file A"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "b.bin"), []byte("file B"), 0644); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDownloader(&config.Config{QueueDir: "/queue", LocalDownDir: source, StateDir: state, MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	d.localMigrationRemoveAllHook = func(name string) error {
		if err := os.Remove(filepath.Join(name, "a.bin")); err != nil {
			return err
		}
		return errors.New("simulated partial tombstone cleanup")
	}
	paths := ManagedPaths{QueueDir: "/queue", LocalDownDir: target}
	if _, err = d.SetManagedPaths(context.Background(), paths, true); err != nil {
		t.Fatal(err)
	}
	waitForLocalMigration(t, d, "failed")
	migration, err := d.LocalDirectoryMigrationStatus()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(target, "a.bin"), []byte("changed after partial deletion"), 0644); err != nil {
		t.Fatal(err)
	}
	d.localMigrationRemoveAllHook = nil
	if _, err = d.SetManagedPaths(context.Background(), paths, true); err != nil {
		t.Fatal(err)
	}
	waitForLocalMigration(t, d, "failed")
	if _, err = os.Stat(filepath.Join(migration.TombstonePath, "b.bin")); err != nil {
		t.Fatalf("target drift must not cause residual source deletion: %v", err)
	}
}

func TestResumeLocalDirectoryMigrationRejectsTargetConflictingWithExplicitConfig(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "old-downloads")
	target := filepath.Join(root, "migration-target")
	configured := filepath.Join(root, "configured-downloads")
	state := filepath.Join(root, "state")
	tombstone := source + ".nas-migration-0123456789abcdef0123456789abcdef"
	content := []byte("must remain available when startup configuration changed")
	for _, dir := range []string{target, configured, tombstone} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(target, "file.bin"), content, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tombstone, "file.bin"), content, 0644); err != nil {
		t.Fatal(err)
	}
	digest, err := hashLocalDirectory(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: source, StateDir: state, MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	id := "0123456789abcdef0123456789abcdef"
	now := time.Now().UTC()
	migration := taskstore.LocalDirectoryMigration{
		ID: id, SourcePath: source, TargetPath: target, DesiredQueuePath: "/queue",
		TombstonePath: tombstone, Status: "running", Phase: "deleting_source",
		VerifiedTargetSHA256: digest, StartedAtUTC: now, UpdatedAtUTC: now,
	}
	if err = d.queue.store.CreateLocalDirectoryMigration(migration); err != nil {
		t.Fatal(err)
	}
	if err = d.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg.LocalDownDir, cfg.LocalDownDirExplicit = configured, true
	d, err = OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Shutdown(context.Background()) }()
	if err = d.ResumeLocalDirectoryMigration(); err != nil {
		t.Fatalf("startup should record a safe refusal and remain available: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	var status taskstore.LocalDirectoryMigration
	for time.Now().Before(deadline) {
		status, err = d.LocalDirectoryMigrationStatus()
		if err != nil || status.Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || status.Status != "failed" || !strings.Contains(status.LastError, "启动") {
		t.Fatalf("migration conflicting with locked startup path was not rejected: migration=%+v err=%v", status, err)
	}
	if _, err = os.Stat(filepath.Join(tombstone, "file.bin")); err != nil {
		t.Fatalf("isolated source must remain intact: %v", err)
	}
	d.queue.mu.Lock()
	maintenance := d.queue.maintenance
	d.queue.mu.Unlock()
	if maintenance {
		t.Fatal("refused migration left the queue permanently in maintenance mode")
	}
}

func TestValidateLocalMigrationStartupConfigHonorsExplicitMonitorPath(t *testing.T) {
	migration := taskstore.LocalDirectoryMigration{TargetPath: `C:\downloads`, DesiredQueuePath: "/persisted-monitor"}
	if err := validateLocalMigrationStartupConfig(migration, &config.Config{QueueDir: "/env-monitor", QueueDirExplicit: true}); err == nil {
		t.Fatal("migration must not resume when its monitor path conflicts with the explicit startup path")
	}
	if err := validateLocalMigrationStartupConfig(migration, &config.Config{QueueDir: "/persisted-monitor", QueueDirExplicit: true}); err != nil {
		t.Fatalf("matching explicit monitor path should permit migration recovery: %v", err)
	}
}

func TestSetManagedPathsCanRetryFailedMigrationWhenPathsAlreadyMatch(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "old-downloads")
	target := filepath.Join(root, "migration-target")
	state := filepath.Join(root, "state")
	id := "abcdef0123456789abcdef0123456789"
	tombstone := source + ".nas-migration-" + id
	content := []byte("resume a failed cleanup without changing configured paths")
	for _, dir := range []string{target, tombstone} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{target, tombstone} {
		if err := os.WriteFile(filepath.Join(dir, "file.bin"), content, 0644); err != nil {
			t.Fatal(err)
		}
	}
	digest, err := hashLocalDirectory(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: target, LocalDownDirExplicit: true, StateDir: state, MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	now := time.Now().UTC()
	migration := taskstore.LocalDirectoryMigration{
		ID: id, SourcePath: source, TargetPath: target, DesiredQueuePath: "/queue", TombstonePath: tombstone,
		Status: "running", Phase: "deleting_source", VerifiedTargetSHA256: digest,
		StartedAtUTC: now, UpdatedAtUTC: now,
	}
	if err = d.queue.store.CreateLocalDirectoryMigration(migration); err != nil {
		t.Fatal(err)
	}
	migration.Status, migration.LastError = "failed", "模拟先前配置冲突后保留的迁移"
	migration.FinishedAtUTC, migration.UpdatedAtUTC = now, now
	if err = d.queue.store.UpdateLocalDirectoryMigration(migration, true); err != nil {
		t.Fatal(err)
	}
	started, err := d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/queue", LocalDownDir: target}, true)
	if err != nil {
		t.Fatalf("same-path save with migrate selected should retry the matching failed migration: %v", err)
	}
	if started.ID != id {
		t.Fatalf("retry must reuse durable migration identity: got %q want %q", started.ID, id)
	}
	waitForLocalMigration(t, d, "completed")
	if _, err = os.Lstat(tombstone); !os.IsNotExist(err) {
		t.Fatalf("successful retry must finish tombstone cleanup: %v", err)
	}
}

func TestLocalDirectoryMigrationCompletionCheckpointFailureCanRetryWithoutRestart(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "old-downloads")
	target := filepath.Join(root, "new-downloads")
	state := filepath.Join(root, "state")
	id, err := newSourceOperationID()
	if err != nil {
		t.Fatal(err)
	}
	tombstone := source + ".nas-migration-" + id
	content := []byte("verified data survives completion checkpoint failure")
	for _, dir := range []string{target, tombstone} {
		if err = os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "file.bin"), content, 0644); err != nil {
			t.Fatal(err)
		}
	}
	digest, err := hashLocalDirectory(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: source, StateDir: state, MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	now := time.Now().UTC()
	migration := taskstore.LocalDirectoryMigration{
		ID: id, SourcePath: source, TargetPath: target, DesiredQueuePath: "/queue", TombstonePath: tombstone,
		Status: "running", Phase: "deleting_source", VerifiedTargetSHA256: digest,
		StartedAtUTC: now, UpdatedAtUTC: now,
	}
	if err = d.queue.store.CreateLocalDirectoryMigration(migration); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(state, "queue", "queue.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TRIGGER fail_local_migration_complete BEFORE UPDATE ON local_directory_migrations WHEN NEW.status='completed' BEGIN SELECT RAISE(ABORT, 'simulated completion checkpoint failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = d.ResumeLocalDirectoryMigration(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		d.localMigrationMu.Lock()
		running := d.localMigrationRunning
		d.localMigrationMu.Unlock()
		if !running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	status, err := d.LocalDirectoryMigrationStatus()
	if err != nil || status.Status != "failed" || status.Phase != "deleting_source" || !strings.Contains(status.LastError, "完成记录保存失败") {
		t.Fatalf("failed completion checkpoint must leave a retryable failed record: migration=%+v err=%v", status, err)
	}
	if _, err = os.Lstat(tombstone); !os.IsNotExist(err) {
		t.Fatalf("verified source cleanup should remain completed: %v", err)
	}
	if _, err = db.Exec(`DROP TRIGGER fail_local_migration_complete`); err != nil {
		t.Fatal(err)
	}
	started, err := d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/queue", LocalDownDir: target}, true)
	if err != nil || started.ID != id {
		t.Fatalf("same-path retry should reuse failed completion record: migration=%+v err=%v", started, err)
	}
	waitForLocalMigration(t, d, "completed")
}

func TestResumeLocalDirectoryMigrationRejectsResolvedStateOverlap(t *testing.T) {
	root := t.TempDir()
	sourceParent := filepath.Join(root, "source-parent")
	source := filepath.Join(sourceParent, "missing-source")
	target := filepath.Join(root, "target")
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(state, 0755); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDownloader(&config.Config{QueueDir: "/queue", LocalDownDir: source, StateDir: state, MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	createMigrationTestAlias(t, sourceParent, state)
	id, err := newSourceOperationID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	migration := taskstore.LocalDirectoryMigration{
		ID: id, SourcePath: source, TargetPath: target, DesiredQueuePath: "/queue",
		TombstonePath: source + ".nas-migration-" + id, Status: "running", Phase: "copying",
		StartedAtUTC: now, UpdatedAtUTC: now,
	}
	if err = d.queue.store.CreateLocalDirectoryMigration(migration); err != nil {
		t.Fatal(err)
	}
	if err = d.ResumeLocalDirectoryMigration(); err != nil {
		t.Fatalf("unsafe recovery should be recorded without preventing management startup: %v", err)
	}
	status, err := d.LocalDirectoryMigrationStatus()
	if err != nil || status.Status != "failed" || !strings.Contains(status.LastError, "程序状态目录") {
		t.Fatalf("unsafe recovery evidence missing: migration=%+v err=%v", status, err)
	}
	d.queue.mu.Lock()
	maintenance := d.queue.maintenance
	d.queue.mu.Unlock()
	if maintenance {
		t.Fatal("unsafe recovery left queue maintenance enabled")
	}
}

func TestResumeLocalDirectoryMigrationRejectsTargetAsTombstone(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "old")
	target := filepath.Join(root, "new")
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	content := []byte("target data must survive an invalid tombstone record")
	if err := os.WriteFile(filepath.Join(target, "keep.bin"), content, 0644); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDownloader(&config.Config{QueueDir: "/queue", LocalDownDir: source, StateDir: state, MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	id, err := newSourceOperationID()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	migration := taskstore.LocalDirectoryMigration{
		ID: id, SourcePath: source, TargetPath: target, DesiredQueuePath: "/queue",
		TombstonePath: target, Status: "running", Phase: "deleting_source",
		StartedAtUTC: now, UpdatedAtUTC: now,
	}
	if err = d.queue.store.CreateLocalDirectoryMigration(migration); err != nil {
		t.Fatal(err)
	}
	if err = d.ResumeLocalDirectoryMigration(); err != nil {
		t.Fatalf("unsafe recovery should be recorded without preventing management startup: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	var status taskstore.LocalDirectoryMigration
	for time.Now().Before(deadline) {
		status, err = d.LocalDirectoryMigrationStatus()
		if err == nil && status.Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || status.Status != "failed" || !strings.Contains(status.LastError, "隔离目录") {
		t.Fatalf("invalid tombstone was not recorded as failed: migration=%+v err=%v", status, err)
	}
	if _, err = d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/queue", LocalDownDir: target}, true); err == nil || !strings.Contains(err.Error(), "隔离目录") {
		t.Fatalf("retry accepted a tombstone path that points at the target directory: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(target, "keep.bin"))
	if err != nil || string(got) != string(content) {
		t.Fatalf("target data was removed or changed: content=%q err=%v", got, err)
	}
}

func TestLocalDirectoryMigrationProgressIncludesFilesAddedAfterInitialScan(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "old")
	target := filepath.Join(root, "new")
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	initial := []byte("initial")
	added := []byte("added after scan")
	if err := os.WriteFile(filepath.Join(source, "initial.bin"), initial, 0644); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDownloader(&config.Config{QueueDir: "/queue", LocalDownDir: source, StateDir: state, MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	d.localMigrationCopyStartHook = func() {
		if hookErr := os.WriteFile(filepath.Join(source, "added.bin"), added, 0644); hookErr != nil {
			t.Errorf("add source file between scan and copy: %v", hookErr)
		}
	}
	if _, err = d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/queue", LocalDownDir: target}, true); err != nil {
		t.Fatal(err)
	}
	waitForLocalMigration(t, d, "completed")
	status, err := d.LocalDirectoryMigrationStatus()
	if err != nil {
		t.Fatal(err)
	}
	wantBytes := int64(len(initial) + len(added))
	if status.TotalFiles != 2 || status.ProcessedFiles != 2 || status.TotalBytes != wantBytes || status.ProcessedBytes != wantBytes {
		t.Fatalf("migration progress totals do not reflect copied files: %+v", status)
	}
}

type cancelOnMigrationContextCheck struct {
	context.Context
	cancel context.CancelFunc
	calls  int
}

func (c *cancelOnMigrationContextCheck) Err() error {
	c.calls++
	if c.calls >= 2 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestLocalDirectoryMigrationVerificationCancellationResumesSamePhase(t *testing.T) {
	for _, phase := range []string{"verifying", "source_retired", "deleting_source"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			source, target, state := filepath.Join(root, "old"), filepath.Join(root, "new"), filepath.Join(root, "state")
			if err := os.MkdirAll(target, 0755); err != nil {
				t.Fatal(err)
			}
			id, err := newSourceOperationID()
			if err != nil {
				t.Fatal(err)
			}
			tombstone := source + ".nas-migration-" + id
			verificationRoot := source
			if phase != "verifying" {
				verificationRoot = tombstone
			}
			if err = os.MkdirAll(verificationRoot, 0755); err != nil {
				t.Fatal(err)
			}
			content := []byte("resume verification at original phase")
			if err = os.WriteFile(filepath.Join(verificationRoot, "file.txt"), content, 0644); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(target, "file.txt"), content, 0644); err != nil {
				t.Fatal(err)
			}
			var verifiedTargetSHA256 string
			if phase == "deleting_source" {
				verifiedTargetSHA256, err = hashLocalDirectory(context.Background(), target)
				if err != nil {
					t.Fatal(err)
				}
			}
			cfg := &config.Config{QueueDir: "/queue", LocalDownDir: source, StateDir: state, MaxParallel: 1}
			d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Shutdown(context.Background())
			now := time.Now().UTC()
			migration := taskstore.LocalDirectoryMigration{
				ID: id, SourcePath: source, TargetPath: target, DesiredQueuePath: "/next-queue",
				TombstonePath: tombstone, Status: "running", Phase: phase,
				VerifiedTargetSHA256: verifiedTargetSHA256,
				TotalFiles:           1, TotalBytes: int64(len(content)), StartedAtUTC: now, UpdatedAtUTC: now,
			}
			if err = d.queue.store.CreateLocalDirectoryMigration(migration); err != nil {
				t.Fatal(err)
			}
			verifyCtx, cancel := context.WithCancel(context.Background())
			d.runLocalDirectoryMigration(&cancelOnMigrationContextCheck{Context: verifyCtx, cancel: cancel}, migration)
			cancel()
			paused, err := d.LocalDirectoryMigrationStatus()
			if err != nil || paused.Status != "running" || paused.Phase != phase {
				t.Fatalf("verification cancellation must preserve active phase %q: migration=%+v err=%v", phase, paused, err)
			}
			if err = d.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			d, err = OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Shutdown(context.Background())
			if err = d.ResumeLocalDirectoryMigration(); err != nil {
				t.Fatalf("resume after verification cancellation: %v", err)
			}
			waitForLocalMigration(t, d, "completed")
		})
	}
}

func TestShutdownWaitsForLocalMigrationRunner(t *testing.T) {
	root := t.TempDir()
	source, target := filepath.Join(root, "old-downloads"), filepath.Join(root, "new-downloads")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("migration shutdown"), 0644); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDownloader(&config.Config{QueueDir: "/queue", LocalDownDir: source, StateDir: filepath.Join(root, "state"), MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseRunner := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseRunner()
	d.SetLocalMigrationCompletionHandler(func() {
		close(started)
		<-release
	})
	if _, err = d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/queue", LocalDownDir: target}, true); err != nil {
		t.Fatalf("start migration: %v", err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("local migration did not enter its completion callback")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	shutdownErr := d.Shutdown(ctx)
	cancel()
	if !errors.Is(shutdownErr, context.DeadlineExceeded) {
		releaseRunner()
		t.Fatalf("shutdown returned before the local migration runner exited: %v", shutdownErr)
	}
	releaseRunner()
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = d.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown after migration runner exited: %v", err)
	}
}

func TestValidateLocalMigrationPathsRejectsResolvedSourceAndStateOverlap(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "downloads")
	state := filepath.Join(root, "state")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(state, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDownloader(&config.Config{QueueDir: "/queue", LocalDownDir: source, StateDir: state, MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	for _, test := range []struct {
		name   string
		target string
		alias  string
		to     string
	}{
		{name: "target aliases source", alias: filepath.Join(root, "source-alias"), to: filepath.Join(source, "nested"), target: filepath.Join(root, "source-alias", "new-target")},
		{name: "target aliases state", alias: filepath.Join(root, "state-alias"), to: filepath.Join(state, "nested"), target: filepath.Join(root, "state-alias", "new-target")},
	} {
		t.Run(test.name, func(t *testing.T) {
			createMigrationTestAlias(t, test.alias, test.to)
			if err := validateLocalMigrationPaths(source, test.target, state); err == nil {
				t.Fatalf("resolved path overlap accepted: source=%q target=%q state=%q", source, test.target, state)
			}
			if _, err := d.beginLocalDirectoryMigration(source, test.target, "/next-queue"); err == nil {
				t.Fatalf("migration accepted resolved path overlap: target=%q", test.target)
			}
			if strings.Contains(test.name, "state") {
				if _, err := d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/queue", LocalDownDir: test.target}, false); err == nil {
					t.Fatalf("non-migrating path update accepted resolved state overlap: target=%q", test.target)
				}
			}
			if _, err := os.Lstat(filepath.Join(test.to, "new-target")); !os.IsNotExist(err) {
				t.Fatalf("rejected migration created target inside protected path: %v", err)
			}
		})
	}
}

func createMigrationTestAlias(t *testing.T, link, target string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
		command := "New-Item -ItemType Junction -Path " + quote(link) + " -Target " + quote(target) + " -ErrorAction Stop | Out-Null"
		if output, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command).CombinedOutput(); err != nil {
			t.Skipf("directory junction unavailable on this platform: %v: %s", err, output)
		}
	} else if err := os.Symlink(target, link); err != nil {
		t.Skipf("directory symlink unavailable on this platform: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
}
