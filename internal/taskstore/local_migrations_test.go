package taskstore

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalDirectoryMigrationEvidencePersistsAndIsAppendOnly(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Truncate(time.Microsecond)
	migration := LocalDirectoryMigration{ID: "0123456789abcdef0123456789abcdef", SourcePath: filepath.Join(t.TempDir(), "old"), TargetPath: filepath.Join(t.TempDir(), "new"), DesiredQueuePath: "/next", TombstonePath: filepath.Join(t.TempDir(), "old.migrated"), Status: "running", Phase: "copying", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.CreateLocalDirectoryMigration(migration); err != nil {
		t.Fatal(err)
	}
	migration.Phase = "verifying"
	migration.TotalFiles, migration.TotalBytes = 2, 10
	migration.ProcessedFiles, migration.ProcessedBytes = 2, 10
	migration.UpdatedAtUTC = now.Add(time.Second)
	if err = store.UpdateLocalDirectoryMigration(migration, true); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadActiveLocalDirectoryMigration()
	if err != nil || loaded.Phase != "verifying" || loaded.TotalBytes != 10 || loaded.DesiredQueuePath != "/next" {
		t.Fatalf("active migration did not persist: %#v, %v", loaded, err)
	}
	if _, err = store.db.Exec(`UPDATE local_directory_migration_events SET phase='tampered' WHERE migration_id=?`, migration.ID); err == nil {
		t.Fatal("migration event history allowed update")
	}
	var events int
	if err = store.db.QueryRow(`SELECT count(*) FROM local_directory_migration_events WHERE migration_id=?`, migration.ID).Scan(&events); err != nil || events != 2 {
		t.Fatalf("migration event history: %d, %v", events, err)
	}
	migration.Status, migration.Phase = "completed", "complete"
	migration.FinishedAtUTC = now.Add(2 * time.Second)
	migration.UpdatedAtUTC = migration.FinishedAtUTC
	if err = store.UpdateLocalDirectoryMigration(migration, true); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LoadActiveLocalDirectoryMigration(); err != sql.ErrNoRows {
		t.Fatalf("completed migration stayed active: %v", err)
	}
	loaded, err = store.LoadLatestLocalDirectoryMigration()
	if err != nil || loaded.Status != "completed" || !loaded.FinishedAtUTC.Equal(migration.FinishedAtUTC) {
		t.Fatalf("completed migration evidence missing: %#v, %v", loaded, err)
	}
}
