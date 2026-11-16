package taskstore

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestSchemaInitializationDoesNotLeavePartialTablesOnDDLFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.sqlite")
	db, err := sql.Open("sqlite", sqliteDataSourceName(path))
	if err != nil {
		t.Fatal(err)
	}
	// Force a late index creation failure after the earlier tables were created.
	if _, err = db.Exec(`CREATE TABLE cloud_inventory(incompatible TEXT)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	opened, err := openSQLite(path, "/queue")
	if err == nil {
		opened.Close()
		t.Fatal("incompatible schema was accepted")
	}
	db, err = sql.Open("sqlite", sqliteDataSourceName(path))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var partial, original int
	if err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='jobs'`).Scan(&partial); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='cloud_inventory'`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if partial != 0 || original != 1 {
		t.Fatalf("partial initialization persisted or existing schema lost: jobs=%d original=%d", partial, original)
	}
}
