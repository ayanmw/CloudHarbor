package taskstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"
)

// v41 -> v42 adds empty download history tables. Old tasks have no reliable
// manifest observation and are deliberately not reconstructed from local files.
func migrateV41(store *Store, scope string) error {
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
	if gotScope != scope || (version != "41" && version != "42") {
		return fmt.Errorf("SQLite store version or scope mismatch during v41 migration")
	}
	if version == "41" {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err = tx.ExecContext(ctx, downloadManifestSchema); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='42' WHERE key='schema_version' AND value='41'`); err != nil {
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	if err = validateDownloadManifestSchema(ctx, db); err != nil {
		return err
	}
	var integrity string
	if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("SQLite integrity check failed during v41 migration: %s", integrity)
	}
	body, _ := json.Marshal(manifest{Version: 42, Scope: scope})
	return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
}
