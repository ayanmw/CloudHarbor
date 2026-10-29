package taskstore

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"web_browser_baidu_onekey_upload/internal/durablefs"
)

// migrateV2 adds durable, revocable administrator sessions. The database
// version is checked separately from the manifest so a crash after the SQLite
// commit can finish publishing the new manifest on the next startup.
func migrateV2(store *Store, scope string) error {
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(store.dir, "queue.sqlite")))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var version, gotScope string
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		return err
	}
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='scope'`).Scan(&gotScope); err != nil {
		return err
	}
	if gotScope != scope || (version != "2" && version != "3") {
		return fmt.Errorf("SQLite store version or scope mismatch during v2 migration")
	}
	if version == "2" {
		tx, beginErr := db.BeginTx(ctx, nil)
		if beginErr != nil {
			return beginErr
		}
		if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS admin_sessions (
			session_hash TEXT PRIMARY KEY CHECK(length(session_hash)=64),
			created_at_utc INTEGER NOT NULL,
			revoked_at_utc INTEGER
		)`); err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='3' WHERE key='schema_version' AND value='2'`)
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	var integrity string
	if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("SQLite integrity check failed during v2 migration: %s", integrity)
	}
	body, _ := json.Marshal(manifest{Version: 3, Scope: scope})
	return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
}

// migrateV3 adds the durable remote-operation journal. A prepared operation
// is deliberately converted to unknown because a crash cannot prove whether
// Baidu applied the mutation.
func migrateV3(store *Store, scope string) error {
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(store.dir, "queue.sqlite")))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var version, gotScope string
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		return err
	}
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='scope'`).Scan(&gotScope); err != nil {
		return err
	}
	if version != "3" || gotScope != scope {
		return fmt.Errorf("SQLite store version or scope mismatch during v3 migration")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS remote_operations (
		operation_id TEXT PRIMARY KEY CHECK(length(operation_id)=32), account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),
		operation_kind TEXT NOT NULL CHECK(length(operation_kind)<=32), object_id INTEGER NOT NULL CHECK(object_id>=0),
		source_path TEXT NOT NULL CHECK(length(source_path)<=4096), destination_path TEXT NOT NULL CHECK(length(destination_path)<=4096),
		status TEXT NOT NULL CHECK(status IN ('prepared','confirmed','unknown','failed')), created_at_utc INTEGER NOT NULL,
		response_observed_at_utc INTEGER, evidence_json BLOB NOT NULL CHECK(json_valid(evidence_json)))`)
	if err == nil {
		_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS remote_operation_events (
		sequence INTEGER PRIMARY KEY AUTOINCREMENT, operation_id TEXT NOT NULL REFERENCES remote_operations(operation_id) ON DELETE RESTRICT,
		status TEXT NOT NULL CHECK(status IN ('prepared','confirmed','unknown','failed')), recorded_at_utc INTEGER NOT NULL,
		response_observed_at_utc INTEGER, evidence_json BLOB NOT NULL CHECK(json_valid(evidence_json)))`)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS remote_operation_events_order ON remote_operation_events(operation_id, sequence)`)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS remote_operation_events_no_update BEFORE UPDATE ON remote_operation_events BEGIN SELECT RAISE(ABORT, 'remote operation events are append-only'); END`)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS remote_operation_events_no_delete BEFORE DELETE ON remote_operation_events BEGIN SELECT RAISE(ABORT, 'remote operation events are append-only'); END`)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='4' WHERE key='schema_version' AND value='3'`)
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	var integrity string
	if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("SQLite integrity check failed during v3 migration: %s", integrity)
	}
	body, _ := json.Marshal(manifest{Version: 4, Scope: scope})
	return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
}

func migrateV4(store *Store, scope string) error {
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(store.dir, "queue.sqlite")))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var version, gotScope string
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		return err
	}
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='scope'`).Scan(&gotScope); err != nil {
		return err
	}
	if version != "4" || gotScope != scope {
		return fmt.Errorf("SQLite store version or scope mismatch during v4 migration")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS cloud_inventory (account_fingerprint TEXT NOT NULL,fs_id INTEGER NOT NULL CHECK(fs_id>0),parent_path TEXT NOT NULL,path TEXT NOT NULL,name TEXT NOT NULL,is_dir INTEGER NOT NULL CHECK(is_dir IN (0,1)),size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),provider_mtime INTEGER NOT NULL,content_md5 TEXT NOT NULL,observed_at_utc INTEGER NOT NULL,last_seen_run_id TEXT NOT NULL,PRIMARY KEY(account_fingerprint,fs_id))`,
		`CREATE TABLE IF NOT EXISTS cloud_inventory_directories (account_fingerprint TEXT NOT NULL,path TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('scanning','complete','stale','unknown','failed')),observed_at_utc INTEGER,last_run_id TEXT NOT NULL,PRIMARY KEY(account_fingerprint,path))`,
		`CREATE TABLE IF NOT EXISTS cloud_inventory_scan_state (account_fingerprint TEXT PRIMARY KEY,run_id TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,pending_json BLOB NOT NULL CHECK(json_valid(pending_json)),current_directory TEXT NOT NULL,next_offset INTEGER NOT NULL CHECK(next_offset>=0),pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),items_processed INTEGER NOT NULL CHECK(items_processed>=0),started_at_utc INTEGER NOT NULL,updated_at_utc INTEGER NOT NULL,last_error TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS cloud_inventory_parent_path ON cloud_inventory(account_fingerprint,parent_path)`,
		`CREATE INDEX IF NOT EXISTS cloud_inventory_path ON cloud_inventory(account_fingerprint,path)`,
		`UPDATE store_meta SET value='5' WHERE key='schema_version' AND value='4'`,
	} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	var integrity string
	if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("SQLite integrity check failed during v4 migration: %s", integrity)
	}
	body, _ := json.Marshal(manifest{Version: 5, Scope: scope})
	return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
}

func migrateV5(store *Store, scope string) error {
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(store.dir, "queue.sqlite")))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var version, gotScope string
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		return err
	}
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='scope'`).Scan(&gotScope); err != nil {
		return err
	}
	if version != "5" || gotScope != scope {
		return fmt.Errorf("SQLite store version or scope mismatch during v5 migration")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS local_directory_migrations (migration_id TEXT PRIMARY KEY CHECK(length(migration_id)=32),source_path TEXT NOT NULL CHECK(length(source_path)<=4096),target_path TEXT NOT NULL CHECK(length(target_path)<=4096),desired_queue_path TEXT NOT NULL CHECK(length(desired_queue_path)<=4096),tombstone_path TEXT NOT NULL CHECK(length(tombstone_path)<=8192),status TEXT NOT NULL CHECK(status IN ('running','failed','completed')),phase TEXT NOT NULL CHECK(phase IN ('copying','verifying','retiring_source','source_retired','deleting_source','complete')),total_files INTEGER NOT NULL CHECK(total_files>=0),total_bytes INTEGER NOT NULL CHECK(total_bytes>=0),processed_files INTEGER NOT NULL CHECK(processed_files>=0),processed_bytes INTEGER NOT NULL CHECK(processed_bytes>=0),started_at_utc INTEGER NOT NULL,updated_at_utc INTEGER NOT NULL,finished_at_utc INTEGER,last_error TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS local_directory_migration_events (sequence INTEGER PRIMARY KEY AUTOINCREMENT,migration_id TEXT NOT NULL REFERENCES local_directory_migrations(migration_id) ON DELETE RESTRICT,status TEXT NOT NULL CHECK(status IN ('running','failed','completed')),phase TEXT NOT NULL,recorded_at_utc INTEGER NOT NULL,evidence_json BLOB NOT NULL CHECK(json_valid(evidence_json)))`,
		`CREATE INDEX IF NOT EXISTS local_directory_migration_events_order ON local_directory_migration_events(migration_id,sequence)`,
		`CREATE TRIGGER IF NOT EXISTS local_directory_migration_events_no_update BEFORE UPDATE ON local_directory_migration_events BEGIN SELECT RAISE(ABORT, 'local migration events are append-only'); END`,
		`CREATE TRIGGER IF NOT EXISTS local_directory_migration_events_no_delete BEFORE DELETE ON local_directory_migration_events BEGIN SELECT RAISE(ABORT, 'local migration events are append-only'); END`,
		`UPDATE store_meta SET value='6' WHERE key='schema_version' AND value='5'`,
	} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	var integrity string
	if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("SQLite integrity check failed during v5 migration: %s", integrity)
	}
	body, _ := json.Marshal(manifest{Version: 6, Scope: scope})
	return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
}

func migrateV6(store *Store, scope string) error {
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(store.dir, "queue.sqlite")))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var version, gotScope string
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		return err
	}
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='scope'`).Scan(&gotScope); err != nil {
		return err
	}
	if version != "6" || gotScope != scope {
		return fmt.Errorf("SQLite store version or scope mismatch during v6 migration")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS cloud_content_hashes (account_fingerprint TEXT NOT NULL,fs_id INTEGER NOT NULL,path TEXT NOT NULL,size_bytes INTEGER NOT NULL,provider_mtime INTEGER NOT NULL,sha256 TEXT NOT NULL CHECK(length(sha256)=64),verified_at_utc INTEGER NOT NULL,PRIMARY KEY(account_fingerprint,fs_id))`,
		`CREATE TABLE IF NOT EXISTS cloud_duplicate_scan_state (account_fingerprint TEXT PRIMARY KEY,run_id TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,next_fs_id INTEGER NOT NULL CHECK(next_fs_id>=0),total_files INTEGER NOT NULL CHECK(total_files>=0),processed_files INTEGER NOT NULL CHECK(processed_files>=0),started_at_utc INTEGER NOT NULL,updated_at_utc INTEGER NOT NULL,last_error TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS cloud_duplicate_scan_events (sequence INTEGER PRIMARY KEY AUTOINCREMENT,account_fingerprint TEXT NOT NULL,run_id TEXT NOT NULL,status TEXT NOT NULL,phase TEXT NOT NULL,recorded_at_utc INTEGER NOT NULL,evidence_json BLOB NOT NULL CHECK(json_valid(evidence_json)))`,
		`CREATE INDEX IF NOT EXISTS cloud_duplicate_scan_events_order ON cloud_duplicate_scan_events(account_fingerprint,sequence)`,
		`CREATE TRIGGER IF NOT EXISTS cloud_duplicate_scan_events_no_update BEFORE UPDATE ON cloud_duplicate_scan_events BEGIN SELECT RAISE(ABORT, 'duplicate scan events are append-only'); END`,
		`CREATE TRIGGER IF NOT EXISTS cloud_duplicate_scan_events_no_delete BEFORE DELETE ON cloud_duplicate_scan_events BEGIN SELECT RAISE(ABORT, 'duplicate scan events are append-only'); END`,
		`UPDATE store_meta SET value='7' WHERE key='schema_version' AND value='6'`,
	} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	var integrity string
	if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("SQLite integrity check failed during v6 migration: %s", integrity)
	}
	body, _ := json.Marshal(manifest{Version: 7, Scope: scope})
	return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
}

func migrateV7(store *Store, scope string) error {
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(store.dir, "queue.sqlite")))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var version, gotScope string
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		return err
	}
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='scope'`).Scan(&gotScope); err != nil {
		return err
	}
	if version != "7" || gotScope != scope {
		return fmt.Errorf("SQLite store version or scope mismatch during v7 migration")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(cloud_duplicate_scan_state)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var defaultValue sql.NullString
		if err = rows.Scan(&cid, &name, &typ, &notnull, &defaultValue, &pk); err != nil {
			rows.Close()
			_ = tx.Rollback()
			return err
		}
		columns[name] = true
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		_ = tx.Rollback()
		return err
	}
	rows.Close()
	if !columns["retry_delay_ms"] {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE cloud_duplicate_scan_state ADD COLUMN retry_delay_ms INTEGER NOT NULL DEFAULT 0 CHECK(retry_delay_ms>=0)`); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if !columns["retry_at_utc"] {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE cloud_duplicate_scan_state ADD COLUMN retry_at_utc INTEGER`); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='8' WHERE key='schema_version' AND value='7'`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	var integrity string
	if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("SQLite integrity check failed during v7 migration: %s", integrity)
	}
	body, _ := json.Marshal(manifest{Version: 8, Scope: scope})
	return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
}

func migrateV8(store *Store, scope string) error {
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(store.dir, "queue.sqlite")))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var version, gotScope string
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		return err
	}
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='scope'`).Scan(&gotScope); err != nil {
		return err
	}
	if gotScope != scope {
		return fmt.Errorf("SQLite store version or scope mismatch during v8 migration")
	}
	finishMigration := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v8 migration: %s", integrity)
		}
		body, _ := json.Marshal(manifest{Version: 9, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "9" {
		var newIndex, oldIndex int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='index' AND name='cloud_inventory_parent_name_id'`).Scan(&newIndex); err != nil {
			return err
		}
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='index' AND name='cloud_inventory_parent_path'`).Scan(&oldIndex); err != nil {
			return err
		}
		if newIndex != 1 || oldIndex != 0 {
			return fmt.Errorf("SQLite index state mismatch during interrupted v8 migration recovery")
		}
		return finishMigration()
	}
	if version != "8" {
		return fmt.Errorf("SQLite store version or scope mismatch during v8 migration")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DROP INDEX IF EXISTS cloud_inventory_parent_path`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS cloud_inventory_parent_name_id ON cloud_inventory(account_fingerprint,parent_path,name,fs_id)`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='9' WHERE key='schema_version' AND value='8'`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return finishMigration()
}

func migrateV9(store *Store, scope string) error {
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(store.dir, "queue.sqlite")))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var version, gotScope string
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		return err
	}
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='scope'`).Scan(&gotScope); err != nil {
		return err
	}
	if gotScope != scope {
		return fmt.Errorf("SQLite store version or scope mismatch during v9 migration")
	}
	finishMigration := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v9 migration: %s", integrity)
		}
		body, _ := json.Marshal(manifest{Version: 10, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "10" {
		var indexCount int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='index' AND name='cloud_inventory_duplicate_size'`).Scan(&indexCount); err != nil {
			return err
		}
		if indexCount != 1 {
			return fmt.Errorf("SQLite duplicate size index missing during interrupted v9 migration recovery")
		}
		return finishMigration()
	}
	if version != "9" {
		return fmt.Errorf("SQLite store version or scope mismatch during v9 migration")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS cloud_inventory_duplicate_size ON cloud_inventory(account_fingerprint,is_dir,size_bytes,fs_id)`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='10' WHERE key='schema_version' AND value='9'`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return finishMigration()
}

func migrateV10(store *Store, scope string) error {
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(store.dir, "queue.sqlite")))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var version, gotScope string
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		return err
	}
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='scope'`).Scan(&gotScope); err != nil {
		return err
	}
	if gotScope != scope {
		return fmt.Errorf("SQLite store version or scope mismatch during v10 migration")
	}
	finishMigration := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v10 migration: %s", integrity)
		}
		body, _ := json.Marshal(manifest{Version: 11, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "11" {
		var indexCount int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='index' AND name='cloud_content_hashes_duplicate_group'`).Scan(&indexCount); err != nil {
			return err
		}
		if indexCount != 1 {
			return fmt.Errorf("SQLite verified duplicate index missing during interrupted v10 migration recovery")
		}
		return finishMigration()
	}
	if version != "10" {
		return fmt.Errorf("SQLite store version or scope mismatch during v10 migration")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS cloud_content_hashes_duplicate_group ON cloud_content_hashes(account_fingerprint,sha256,size_bytes,fs_id)`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='11' WHERE key='schema_version' AND value='10'`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return finishMigration()
}

func migrateV11(store *Store, scope string) error {
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(store.dir, "queue.sqlite")))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var version, gotScope string
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		return err
	}
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='scope'`).Scan(&gotScope); err != nil {
		return err
	}
	if gotScope != scope {
		return fmt.Errorf("SQLite store version or scope mismatch during v11 migration")
	}
	finishMigration := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v11 migration: %s", integrity)
		}
		var objectCount int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE (type='table' AND name='cloud_inventory_file_sizes') OR (type='trigger' AND name IN ('cloud_inventory_file_size_insert','cloud_inventory_file_size_delete','cloud_inventory_file_size_update'))`).Scan(&objectCount); err != nil {
			return err
		}
		if objectCount != 4 {
			return fmt.Errorf("SQLite inventory file-size summary schema is incomplete during v11 migration recovery")
		}
		var consistent int
		if err = db.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT account_fingerprint,size_bytes,count(*) FROM cloud_inventory WHERE is_dir=0 GROUP BY account_fingerprint,size_bytes EXCEPT SELECT account_fingerprint,size_bytes,file_count FROM cloud_inventory_file_sizes) AND NOT EXISTS(SELECT account_fingerprint,size_bytes,file_count FROM cloud_inventory_file_sizes EXCEPT SELECT account_fingerprint,size_bytes,count(*) FROM cloud_inventory WHERE is_dir=0 GROUP BY account_fingerprint,size_bytes)`).Scan(&consistent); err != nil {
			return err
		}
		if consistent != 1 {
			return fmt.Errorf("SQLite inventory file-size summary mismatch during v11 migration recovery")
		}
		body, _ := json.Marshal(manifest{Version: 12, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "12" {
		return finishMigration()
	}
	if version != "11" {
		return fmt.Errorf("SQLite store version or scope mismatch during v11 migration")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS cloud_inventory_file_sizes (account_fingerprint TEXT NOT NULL,size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),file_count INTEGER NOT NULL CHECK(file_count>=0),PRIMARY KEY(account_fingerprint,size_bytes))`,
		`INSERT INTO cloud_inventory_file_sizes(account_fingerprint,size_bytes,file_count) SELECT account_fingerprint,size_bytes,count(*) FROM cloud_inventory WHERE is_dir=0 GROUP BY account_fingerprint,size_bytes`,
		`CREATE TRIGGER IF NOT EXISTS cloud_inventory_file_size_insert AFTER INSERT ON cloud_inventory WHEN NEW.is_dir=0 BEGIN INSERT INTO cloud_inventory_file_sizes(account_fingerprint,size_bytes,file_count) VALUES(NEW.account_fingerprint,NEW.size_bytes,1) ON CONFLICT(account_fingerprint,size_bytes) DO UPDATE SET file_count=file_count+1; END`,
		`CREATE TRIGGER IF NOT EXISTS cloud_inventory_file_size_delete AFTER DELETE ON cloud_inventory WHEN OLD.is_dir=0 BEGIN UPDATE cloud_inventory_file_sizes SET file_count=file_count-1 WHERE account_fingerprint=OLD.account_fingerprint AND size_bytes=OLD.size_bytes; DELETE FROM cloud_inventory_file_sizes WHERE account_fingerprint=OLD.account_fingerprint AND size_bytes=OLD.size_bytes AND file_count<=0; END`,
		`CREATE TRIGGER IF NOT EXISTS cloud_inventory_file_size_update AFTER UPDATE OF account_fingerprint,is_dir,size_bytes ON cloud_inventory WHEN OLD.account_fingerprint<>NEW.account_fingerprint OR OLD.is_dir<>NEW.is_dir OR OLD.size_bytes<>NEW.size_bytes BEGIN UPDATE cloud_inventory_file_sizes SET file_count=file_count-1 WHERE account_fingerprint=OLD.account_fingerprint AND size_bytes=OLD.size_bytes AND OLD.is_dir=0; DELETE FROM cloud_inventory_file_sizes WHERE account_fingerprint=OLD.account_fingerprint AND size_bytes=OLD.size_bytes AND file_count<=0; INSERT INTO cloud_inventory_file_sizes(account_fingerprint,size_bytes,file_count) SELECT NEW.account_fingerprint,NEW.size_bytes,1 WHERE NEW.is_dir=0 ON CONFLICT(account_fingerprint,size_bytes) DO UPDATE SET file_count=file_count+1; END`,
		`UPDATE store_meta SET value='12' WHERE key='schema_version' AND value='11'`,
	}
	for _, statement := range statements {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return finishMigration()
}

// migrateV12 replaces the serialized scan worklist with a durable FIFO table.
// The legacy JSON remains in place so a committed migration can be verified
// and its manifest safely published after a restart.
func migrateV12(store *Store, scope string) error {
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(store.dir, "queue.sqlite")))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var version, gotScope string
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		return err
	}
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='scope'`).Scan(&gotScope); err != nil {
		return err
	}
	if gotScope != scope || (version != "12" && version != "13") {
		return fmt.Errorf("SQLite store version or scope mismatch during v12 migration")
	}
	type pendingState struct {
		account string
		runID   string
		current string
		offset  int64
		paths   []string
	}
	readPending := func() ([]pendingState, error) {
		rows, queryErr := db.QueryContext(ctx, `SELECT account_fingerprint,run_id,current_directory,next_offset,pending_json FROM cloud_inventory_scan_state ORDER BY account_fingerprint`)
		if queryErr != nil {
			return nil, queryErr
		}
		defer rows.Close()
		states := make([]pendingState, 0)
		for rows.Next() {
			var state pendingState
			var body []byte
			if queryErr = rows.Scan(&state.account, &state.runID, &state.current, &state.offset, &body); queryErr != nil {
				return nil, queryErr
			}
			if queryErr = json.Unmarshal(body, &state.paths); queryErr != nil || state.paths == nil {
				return nil, fmt.Errorf("invalid cloud scan worklist during v12 migration")
			}
			seen := make(map[string]bool, len(state.paths))
			for _, path := range state.paths {
				if !validInventoryPath(path) || seen[path] {
					return nil, fmt.Errorf("invalid or duplicate cloud scan path during v12 migration")
				}
				seen[path] = true
			}
			if len(state.paths) > 0 && state.paths[0] != state.current && state.offset != 0 {
				return nil, fmt.Errorf("inconsistent cloud scan cursor during v12 migration")
			}
			states = append(states, state)
		}
		return states, rows.Err()
	}
	states, err := readPending()
	if err != nil {
		return err
	}
	finishMigration := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v12 migration: %s", integrity)
		}
		var objectCount int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='cloud_inventory_scan_queue'`).Scan(&objectCount); err != nil {
			return err
		}
		if objectCount != 1 {
			return fmt.Errorf("SQLite cloud scan queue schema is incomplete during v12 migration recovery")
		}
		var expectedQueued int
		for _, state := range states {
			expectedQueued += len(state.paths)
		}
		var actualQueued int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM cloud_inventory_scan_queue`).Scan(&actualQueued); err != nil {
			return err
		}
		if actualQueued != expectedQueued {
			return fmt.Errorf("SQLite cloud scan queue contains unexpected rows during v12 migration recovery")
		}
		for _, state := range states {
			rows, queryErr := db.QueryContext(ctx, `SELECT path FROM cloud_inventory_scan_queue WHERE account_fingerprint=? AND run_id=? ORDER BY sequence`, state.account, state.runID)
			if queryErr != nil {
				return queryErr
			}
			var queued []string
			for rows.Next() {
				var path string
				if queryErr = rows.Scan(&path); queryErr != nil {
					rows.Close()
					return queryErr
				}
				queued = append(queued, path)
			}
			queryErr = rows.Err()
			rows.Close()
			if queryErr != nil {
				return queryErr
			}
			if len(queued) != len(state.paths) {
				return fmt.Errorf("SQLite cloud scan queue mismatch during v12 migration recovery")
			}
			for i := range queued {
				if queued[i] != state.paths[i] {
					return fmt.Errorf("SQLite cloud scan queue order mismatch during v12 migration recovery")
				}
			}
		}
		body, _ := json.Marshal(manifest{Version: 13, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "13" {
		return finishMigration()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS cloud_inventory_scan_queue (account_fingerprint TEXT NOT NULL,run_id TEXT NOT NULL,sequence INTEGER NOT NULL CHECK(sequence>=0),path TEXT NOT NULL,PRIMARY KEY(account_fingerprint,run_id,sequence),UNIQUE(account_fingerprint,run_id,path))`); err == nil {
		// Rebuilding also makes legacy-version test fixtures with a newer physical
		// schema deterministic; version 12 never had authoritative queue rows.
		_, err = tx.ExecContext(ctx, `DELETE FROM cloud_inventory_scan_queue`)
	}
	for _, state := range states {
		for sequence, path := range state.paths {
			if err != nil {
				break
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO cloud_inventory_scan_queue(account_fingerprint,run_id,sequence,path) VALUES(?,?,?,?)`, state.account, state.runID, sequence, path)
		}
		if err == nil && len(state.paths) > 0 && state.paths[0] != state.current {
			_, err = tx.ExecContext(ctx, `UPDATE cloud_inventory_scan_state SET current_directory=?,next_offset=0 WHERE account_fingerprint=? AND run_id=?`, state.paths[0], state.account, state.runID)
		}
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='13' WHERE key='schema_version' AND value='12'`)
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return finishMigration()
}

// migrateV13 stores the pending queue size beside scan progress so status
// polling does not count every queued directory.
func migrateV13(store *Store, scope string) error {
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(store.dir, "queue.sqlite")))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var version, gotScope string
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		return err
	}
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='scope'`).Scan(&gotScope); err != nil {
		return err
	}
	if gotScope != scope || (version != "13" && version != "14") {
		return fmt.Errorf("SQLite store version or scope mismatch during v13 migration (version=%q, scope_matches=%t)", version, gotScope == scope)
	}
	verify := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v13 migration: %s", integrity)
		}
		var columnCount int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_scan_state') WHERE name='pending_count'`).Scan(&columnCount); err != nil {
			return err
		}
		if columnCount != 1 {
			return fmt.Errorf("SQLite cloud scan pending count column is missing")
		}
		var consistent int
		if err = db.QueryRowContext(ctx, `SELECT NOT EXISTS(SELECT 1 FROM cloud_inventory_scan_state s WHERE s.pending_count<>(SELECT count(*) FROM cloud_inventory_scan_queue q WHERE q.account_fingerprint=s.account_fingerprint AND q.run_id=s.run_id) UNION ALL SELECT 1 FROM cloud_inventory_scan_queue q WHERE NOT EXISTS(SELECT 1 FROM cloud_inventory_scan_state s WHERE s.account_fingerprint=q.account_fingerprint AND s.run_id=q.run_id))`).Scan(&consistent); err != nil {
			return err
		}
		if consistent != 1 {
			return fmt.Errorf("SQLite cloud scan pending count mismatch during v13 migration recovery")
		}
		body, _ := json.Marshal(manifest{Version: 14, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "14" {
		return verify()
	}
	var columnCount int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_scan_state') WHERE name='pending_count'`).Scan(&columnCount); err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if columnCount == 0 {
		_, err = tx.ExecContext(ctx, `ALTER TABLE cloud_inventory_scan_state ADD COLUMN pending_count INTEGER NOT NULL DEFAULT 0 CHECK(pending_count>=0)`)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE cloud_inventory_scan_state SET pending_count=(SELECT count(*) FROM cloud_inventory_scan_queue q WHERE q.account_fingerprint=cloud_inventory_scan_state.account_fingerprint AND q.run_id=cloud_inventory_scan_state.run_id)`)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='14' WHERE key='schema_version' AND value='13'`)
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return verify()
}

// migrateV14 adds generation-scoped persistent directory-size aggregates.
// The cache starts empty; existing cloud inventory remains authoritative.
func migrateV14(store *Store, scope string) error {
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(store.dir, "queue.sqlite")))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(1)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var version, gotScope string
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		return err
	}
	if err = db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='scope'`).Scan(&gotScope); err != nil {
		return err
	}
	if gotScope != scope || (version != "14" && version != "15") {
		return fmt.Errorf("SQLite store version or scope mismatch during v14 migration")
	}
	verify := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v14 migration: %s", integrity)
		}
		var tableCount int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('cloud_inventory_generations','cloud_directory_size_cache')`).Scan(&tableCount); err != nil {
			return err
		}
		if tableCount != 2 {
			return fmt.Errorf("SQLite directory-size cache schema is incomplete during v14 migration recovery")
		}
		body, _ := json.Marshal(manifest{Version: 15, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "15" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS cloud_inventory_generations (account_fingerprint TEXT PRIMARY KEY CHECK(length(account_fingerprint)<=128),generation INTEGER NOT NULL CHECK(generation>=0),updated_at_utc INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS cloud_directory_size_cache (account_fingerprint TEXT NOT NULL,path TEXT NOT NULL,generation INTEGER NOT NULL CHECK(generation>=0),bytes INTEGER NOT NULL CHECK(bytes>=0),files INTEGER NOT NULL CHECK(files>=0),directories INTEGER NOT NULL CHECK(directories>=0),complete INTEGER NOT NULL CHECK(complete IN (0,1)),observed_at_utc INTEGER,cached_at_utc INTEGER NOT NULL,PRIMARY KEY(account_fingerprint,path),FOREIGN KEY(account_fingerprint) REFERENCES cloud_inventory_generations(account_fingerprint) ON DELETE CASCADE)`,
		`UPDATE store_meta SET value='15' WHERE key='schema_version' AND value='14'`,
	}
	for _, statement := range statements {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return verify()
}

func migrateV1(store *Store, scope string) error {
	jobsPath := filepath.Join(store.dir, "jobs")
	if err := safePath(jobsPath); err != nil {
		return err
	}
	dir, err := openJobDirectory(jobsPath)
	if err != nil {
		return err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	tempPath := filepath.Join(store.dir, "queue.sqlite.migrating")
	finalPath := filepath.Join(store.dir, "queue.sqlite")
	for _, name := range []string{tempPath, finalPath} {
		if err := safePath(name); err != nil {
			return err
		}
		if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	db, err := openSQLite(tempPath, scope)
	if err != nil {
		return err
	}
	migrationID, err := newRunID()
	if err != nil {
		db.Close()
		return err
	}
	published := false
	defer func() {
		db.Close()
		if !published {
			os.Remove(tempPath)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		tx.Rollback()
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") && !entry.IsDir() {
			continue
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return rollback(fmt.Errorf("unexpected legacy state entry: %s", entry.Name()))
		}
		id, parseErr := strconv.ParseInt(strings.TrimSuffix(entry.Name(), ".json"), 10, 64)
		if parseErr != nil || id <= 0 || strconv.FormatInt(id, 10)+".json" != entry.Name() {
			return rollback(fmt.Errorf("invalid legacy job filename: %s", entry.Name()))
		}
		file, openErr := openJobFileAt(dir, entry.Name())
		if openErr != nil {
			return rollback(openErr)
		}
		body, readErr := io.ReadAll(io.LimitReader(file, 1024*1024+1))
		file.Close()
		if readErr != nil || len(body) > 1024*1024 || !json.Valid(body) {
			if readErr != nil {
				return rollback(readErr)
			}
			return rollback(fmt.Errorf("corrupt legacy job %d", id))
		}
		if err = validateLegacyJob(id, body); err != nil {
			return rollback(err)
		}
		var originalTimes struct {
			Created json.RawMessage `json:"created"`
			Updated json.RawMessage `json:"updated"`
		}
		if err = json.Unmarshal(body, &originalTimes); err != nil {
			return rollback(fmt.Errorf("invalid legacy timestamps for job %d: %w", id, err))
		}
		evidence, evidenceErr := json.Marshal(struct {
			SourceFormat  int             `json:"source_format"`
			LegacyCreated json.RawMessage `json:"legacy_created"`
			LegacyUpdated json.RawMessage `json:"legacy_updated"`
		}{1, originalTimes.Created, originalTimes.Updated})
		if evidenceErr != nil {
			return rollback(evidenceErr)
		}
		event := Event{Kind: "legacy_import", RunID: migrationID, EvidenceType: "legacy_import", Evidence: evidence}
		if err = saveJobTx(ctx, tx, migrationID, id, body, event); err != nil {
			return rollback(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if err = db.Close(); err != nil {
		return err
	}
	check, err := openSQLiteExisting(tempPath, scope)
	if err != nil {
		return err
	}
	var imported int
	if err = check.QueryRow(`SELECT count(*) FROM jobs`).Scan(&imported); err != nil {
		check.Close()
		return err
	}
	var sourceCount int
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			sourceCount++
		}
	}
	if imported != sourceCount {
		check.Close()
		return fmt.Errorf("legacy migration record count mismatch: source=%d target=%d", sourceCount, imported)
	}
	if err = verifyLegacyEquivalence(check, dir, entries); err != nil {
		check.Close()
		return err
	}
	if err = check.Close(); err != nil {
		return err
	}
	if err = durablefs.PublishNoReplace(tempPath, finalPath); err != nil {
		return err
	}
	if err = durablefs.SyncDirectory(store.dir); err != nil {
		return err
	}
	body, _ := json.Marshal(manifest{Version: sqliteSchemaVersion, Scope: scope})
	if err = atomicWrite(filepath.Join(store.dir, "manifest.json"), body); err != nil {
		return err
	}
	published = true
	return nil
}

func validateLegacyJob(id int64, body []byte) error {
	var job struct {
		Version int `json:"version"`
		File    struct {
			FsID           int64  `json:"fs_id"`
			Path           string `json:"path"`
			ServerFilename string `json:"server_filename"`
			Size           int64  `json:"size"`
			IsDir          int    `json:"isdir"`
			Mtime          int64  `json:"server_mtime"`
			MD5            string `json:"md5"`
		} `json:"file"`
		Mode              string    `json:"mode"`
		Status            string    `json:"status"`
		SourceState       string    `json:"source_state"`
		SourceOperationID string    `json:"source_operation_id"`
		SourceAttemptID   string    `json:"source_attempt_id"`
		AttemptID         string    `json:"attempt_id"`
		Attempts          int       `json:"attempts"`
		Failures          int       `json:"failures"`
		Error             string    `json:"error"`
		Created           time.Time `json:"created"`
		Updated           time.Time `json:"updated"`
		NextAttempt       time.Time `json:"next_attempt"`
	}
	if err := json.Unmarshal(body, &job); err != nil {
		return fmt.Errorf("invalid legacy job %d: %w", id, err)
	}
	if job.Version != 1 || job.File.FsID != id || (job.Mode != "keep" && job.Mode != "delete") || job.Attempts < 0 || job.Failures < 0 {
		return fmt.Errorf("invalid legacy job identity or counters: %d", id)
	}
	if (job.SourceOperationID == "") != (job.SourceAttemptID == "") {
		return fmt.Errorf("invalid legacy source operation IDs: %d", id)
	}
	for _, value := range []string{job.SourceOperationID, job.SourceAttemptID, job.AttemptID} {
		if value == "" {
			continue
		}
		decoded, err := hex.DecodeString(value)
		if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != value {
			return fmt.Errorf("invalid legacy attempt ID: %d", id)
		}
	}
	switch job.Status {
	case "pending", "downloading", "retry_wait", "auth_wait", "failed", "done", "cancelled", "source_unknown":
	default:
		return fmt.Errorf("invalid legacy job status: %d", id)
	}
	switch job.SourceState {
	case "":
		if job.Status == "source_unknown" {
			return fmt.Errorf("invalid legacy source state: %d", id)
		}
	case "sending", "unknown":
		if job.Status != "source_unknown" {
			return fmt.Errorf("invalid legacy source status: %d", id)
		}
	case "changed":
		if job.Status != "failed" {
			return fmt.Errorf("invalid legacy source status: %d", id)
		}
	case "confirmed":
		if job.Status != "done" {
			return fmt.Errorf("invalid legacy source status: %d", id)
		}
	default:
		return fmt.Errorf("invalid legacy source state: %d", id)
	}
	return nil
}

func verifyLegacyEquivalence(db *sql.DB, dir *os.File, entries []os.DirEntry) error {
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSuffix(entry.Name(), ".json"), 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10)+".json" != entry.Name() {
			return fmt.Errorf("invalid legacy job filename: %s", entry.Name())
		}
		file, err := openJobFileAt(dir, entry.Name())
		if err != nil {
			return err
		}
		want, readErr := io.ReadAll(io.LimitReader(file, 1024*1024+1))
		file.Close()
		if readErr != nil || len(want) > 1024*1024 {
			if readErr != nil {
				return readErr
			}
			return fmt.Errorf("oversized legacy job %d", id)
		}
		var got []byte
		if err = db.QueryRow(`SELECT payload_json FROM jobs WHERE id=?`, id).Scan(&got); err != nil {
			return err
		}
		if string(got) != string(want) {
			return fmt.Errorf("legacy job %d changed during migration", id)
		}
	}
	return nil
}
