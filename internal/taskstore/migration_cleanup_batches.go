package taskstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"
)

// v42 -> v43 adds empty cleanup batches. Historical individual operations
// cannot prove a batch manifest and are deliberately not backfilled.
func migrateV42(store *Store, scope string) error {
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(store.dir, "queue.sqlite")))
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var version, gotScope string
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		return err
	}
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='scope'`).Scan(&gotScope); err != nil {
		return err
	}
	if gotScope != scope || (version != "42" && version != "43") {
		return fmt.Errorf("SQLite store version or scope mismatch during v42 migration")
	}
	if version == "42" {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, cleanupBatchSchema); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='43' WHERE key='schema_version' AND value='42'`); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	if err = validateCleanupBatchSchema(ctx, db); err != nil {
		return err
	}
	var integrity string
	if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("SQLite integrity check failed during v42 migration: %s", integrity)
	}
	body, _ := json.Marshal(manifest{Version: 43, Scope: scope})
	return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
}
