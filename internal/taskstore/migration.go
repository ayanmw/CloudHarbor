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

	"github.com/ayanmw/CloudHarbor/internal/durablefs"
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
		`CREATE TABLE IF NOT EXISTS cloud_inventory_scan_state (account_fingerprint TEXT PRIMARY KEY,run_id TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,pending_json BLOB NOT NULL CHECK(json_valid(pending_json)),current_directory TEXT NOT NULL,next_offset INTEGER NOT NULL CHECK(next_offset>=0),pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),items_processed INTEGER NOT NULL CHECK(items_processed>=0),started_at_utc INTEGER NOT NULL,updated_at_utc INTEGER NOT NULL,last_error TEXT NOT NULL,request_budget INTEGER NOT NULL DEFAULT 0 CHECK(request_budget>=0),requests_used INTEGER NOT NULL DEFAULT 0 CHECK(requests_used>=0 AND requests_used<=request_budget))`,
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
	if gotScope != scope || (version != "14" && version != "15" && version != "16" && version != "17" && version != "18" && version != "19" && version != "20" && version != "21" && version != "22" && version != "23" && version != "24" && version != "25" && version != "26") {
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
		manifestVersion := 15
		if version == "16" {
			manifestVersion = 16
		} else if version == "17" {
			manifestVersion = 17
		} else if version == "18" {
			manifestVersion = 18
		} else if version == "19" {
			manifestVersion = 19
		} else if version == "20" {
			manifestVersion = 20
		} else if version == "21" {
			manifestVersion = 21
		}
		body, _ := json.Marshal(manifest{Version: manifestVersion, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "15" || version == "16" || version == "17" || version == "18" || version == "19" || version == "20" || version == "21" || version == "22" {
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

// migrateV15 separates resumable directory-scoped refresh state from the
// account-wide inventory scan checkpoint.
func migrateV15(store *Store, scope string) error {
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
	if gotScope != scope || (version != "15" && version != "16" && version != "17" && version != "18" && version != "19" && version != "20" && version != "21" && version != "22" && version != "23" && version != "24" && version != "25" && version != "26") {
		return fmt.Errorf("SQLite store version or scope mismatch during v15 migration")
	}
	verify := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v15 migration: %s", integrity)
		}
		var tableCount int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('cloud_inventory_refresh_state','cloud_inventory_refresh_queue')`).Scan(&tableCount); err != nil {
			return err
		}
		if tableCount != 2 {
			return fmt.Errorf("SQLite scoped refresh schema is incomplete during v15 migration recovery")
		}
		manifestVersion := 16
		if version == "17" {
			manifestVersion = 17
		} else if version == "18" {
			manifestVersion = 18
		} else if version == "19" {
			manifestVersion = 19
		} else if version == "20" {
			manifestVersion = 20
		} else if version == "21" {
			manifestVersion = 21
		}
		body, _ := json.Marshal(manifest{Version: manifestVersion, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "16" || version == "17" || version == "18" || version == "19" || version == "20" || version == "21" || version == "22" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS cloud_inventory_refresh_state (account_fingerprint TEXT PRIMARY KEY CHECK(length(account_fingerprint)<=128),run_id TEXT NOT NULL CHECK(length(run_id)<=128),root_path TEXT NOT NULL CHECK(length(root_path)<=4096),status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,current_directory TEXT NOT NULL CHECK(length(current_directory)<=4096),next_offset INTEGER NOT NULL CHECK(next_offset>=0),pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),items_processed INTEGER NOT NULL CHECK(items_processed>=0),pending_count INTEGER NOT NULL CHECK(pending_count>=0),started_at_utc INTEGER NOT NULL,updated_at_utc INTEGER NOT NULL,last_error TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS cloud_inventory_refresh_queue (account_fingerprint TEXT NOT NULL,run_id TEXT NOT NULL,sequence INTEGER NOT NULL CHECK(sequence>=0),path TEXT NOT NULL CHECK(length(path)<=4096),PRIMARY KEY(account_fingerprint,run_id,sequence),UNIQUE(account_fingerprint,run_id,path))`,
		`UPDATE store_meta SET value='16' WHERE key='schema_version' AND value='15'`,
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

// migrateV16 adds durable byte counters for duplicate-file hashing. A completed
// counter is reconstructed only from verified hashes that still match the
// current inventory identity; unprovable historical bytes remain zero.
func migrateV16(store *Store, scope string) error {
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
	if gotScope != scope || (version != "16" && version != "17" && version != "18" && version != "19" && version != "20" && version != "21" && version != "22" && version != "23" && version != "24" && version != "25" && version != "26") {
		return fmt.Errorf("SQLite store version or scope mismatch during v16 migration")
	}
	verify := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v16 migration: %s", integrity)
		}
		rows, queryErr := db.QueryContext(ctx, `PRAGMA table_info(cloud_duplicate_scan_state)`)
		if queryErr != nil {
			return queryErr
		}
		columns := map[string]bool{}
		for rows.Next() {
			var cid, notNull, primaryKey int
			var name, dataType string
			var defaultValue any
			if queryErr = rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); queryErr != nil {
				rows.Close()
				return queryErr
			}
			columns[name] = true
		}
		if queryErr = rows.Err(); queryErr != nil {
			rows.Close()
			return queryErr
		}
		rows.Close()
		if !columns["total_bytes"] || !columns["processed_bytes"] {
			return fmt.Errorf("SQLite duplicate scan byte-progress schema is incomplete during v16 migration recovery")
		}
		manifestVersion := 17
		if version == "18" {
			manifestVersion = 18
		} else if version == "19" {
			manifestVersion = 19
		} else if version == "20" {
			manifestVersion = 20
		} else if version == "21" {
			manifestVersion = 21
		}
		body, _ := json.Marshal(manifest{Version: manifestVersion, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "17" || version == "18" || version == "19" || version == "20" || version == "21" || version == "22" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// Historical migration tests and interrupted upgrades can have the new
	// columns already present while metadata still reports v16. Add only the
	// missing columns so retrying the migration remains safe.
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(cloud_duplicate_scan_state)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err = rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			_ = tx.Rollback()
			return err
		}
		columns[name] = true
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		_ = tx.Rollback()
		return err
	}
	if err = rows.Close(); err != nil {
		_ = tx.Rollback()
		return err
	}
	statements := []string{}
	if !columns["total_bytes"] {
		statements = append(statements, `ALTER TABLE cloud_duplicate_scan_state ADD COLUMN total_bytes INTEGER NOT NULL DEFAULT 0 CHECK(total_bytes>=0)`)
	}
	if !columns["processed_bytes"] {
		statements = append(statements, `ALTER TABLE cloud_duplicate_scan_state ADD COLUMN processed_bytes INTEGER NOT NULL DEFAULT 0 CHECK(processed_bytes>=0)`)
	}
	statements = append(statements,
		`UPDATE cloud_duplicate_scan_state SET processed_bytes=coalesce((SELECT sum(i.size_bytes) FROM cloud_inventory i CROSS JOIN cloud_inventory_file_sizes s JOIN cloud_inventory_directories d ON d.account_fingerprint=i.account_fingerprint AND d.path=i.parent_path AND d.status='complete' JOIN cloud_content_hashes h ON h.account_fingerprint=i.account_fingerprint AND h.fs_id=i.fs_id AND h.path=i.path AND h.size_bytes=i.size_bytes AND h.provider_mtime=i.provider_mtime WHERE i.account_fingerprint=cloud_duplicate_scan_state.account_fingerprint AND i.is_dir=0 AND i.fs_id<=cloud_duplicate_scan_state.next_fs_id AND s.account_fingerprint=i.account_fingerprint AND s.size_bytes=i.size_bytes AND s.file_count>1),0)`,
		`UPDATE cloud_duplicate_scan_state SET total_files=processed_files+coalesce((SELECT count(*) FROM cloud_inventory i CROSS JOIN cloud_inventory_file_sizes s JOIN cloud_inventory_directories d ON d.account_fingerprint=i.account_fingerprint AND d.path=i.parent_path AND d.status='complete' WHERE i.account_fingerprint=cloud_duplicate_scan_state.account_fingerprint AND i.is_dir=0 AND i.fs_id>cloud_duplicate_scan_state.next_fs_id AND s.account_fingerprint=i.account_fingerprint AND s.size_bytes=i.size_bytes AND s.file_count>1),0),total_bytes=processed_bytes+coalesce((SELECT sum(i.size_bytes) FROM cloud_inventory i CROSS JOIN cloud_inventory_file_sizes s JOIN cloud_inventory_directories d ON d.account_fingerprint=i.account_fingerprint AND d.path=i.parent_path AND d.status='complete' WHERE i.account_fingerprint=cloud_duplicate_scan_state.account_fingerprint AND i.is_dir=0 AND i.fs_id>cloud_duplicate_scan_state.next_fs_id AND s.account_fingerprint=i.account_fingerprint AND s.size_bytes=i.size_bytes AND s.file_count>1),0)`,
		`UPDATE store_meta SET value='17' WHERE key='schema_version' AND value='16'`,
	)
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

// migrateV17 adds durable per-directory refresh staging. A directory is not
// published from these rows until its complete provider listing is available.
func migrateV17(store *Store, scope string) error {
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
	if gotScope != scope || (version != "17" && version != "18" && version != "19" && version != "20" && version != "21" && version != "22" && version != "23" && version != "24" && version != "25" && version != "26") {
		return fmt.Errorf("SQLite store version or scope mismatch during v17 migration")
	}
	verify := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v17 migration: %s", integrity)
		}
		var objects int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE (type='table' AND name='cloud_inventory_refresh_entries') OR (type='index' AND name='cloud_inventory_refresh_entries_order')`).Scan(&objects); err != nil {
			return err
		}
		if objects != 2 {
			return fmt.Errorf("SQLite inventory refresh staging schema is incomplete during v17 migration recovery")
		}
		manifestVersion := 18
		if version == "19" {
			manifestVersion = 19
		} else if version == "20" {
			manifestVersion = 20
		} else if version == "21" {
			manifestVersion = 21
		}
		body, _ := json.Marshal(manifest{Version: manifestVersion, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "18" || version == "19" || version == "20" || version == "21" || version == "22" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS cloud_inventory_refresh_entries (account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),run_id TEXT NOT NULL CHECK(length(run_id)<=128),directory TEXT NOT NULL CHECK(length(directory)<=4096),fs_id INTEGER NOT NULL CHECK(fs_id>0),path TEXT NOT NULL CHECK(length(path)<=4096),name TEXT NOT NULL CHECK(length(name)<=1024),is_dir INTEGER NOT NULL CHECK(is_dir IN (0,1)),size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),provider_mtime INTEGER NOT NULL,content_md5 TEXT NOT NULL,PRIMARY KEY(account_fingerprint,run_id,directory,fs_id))`,
		`CREATE INDEX IF NOT EXISTS cloud_inventory_refresh_entries_order ON cloud_inventory_refresh_entries(account_fingerprint,run_id,directory,path,fs_id)`,
		`UPDATE store_meta SET value='18' WHERE key='schema_version' AND value='17'`,
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

// migrateV18 adds append-only, account-scoped directory refresh history.
func migrateV18(store *Store, scope string) error {
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
	if gotScope != scope || (version != "18" && version != "19" && version != "20" && version != "21" && version != "22" && version != "23" && version != "24" && version != "25" && version != "26") {
		return fmt.Errorf("SQLite store version or scope mismatch during v18 migration")
	}
	verify := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v18 migration: %s", integrity)
		}
		var objects int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE (type='table' AND name='cloud_inventory_refresh_events') OR (type='index' AND name='cloud_inventory_refresh_events_order') OR (type='trigger' AND name IN ('cloud_inventory_refresh_events_no_update','cloud_inventory_refresh_events_no_delete'))`).Scan(&objects); err != nil {
			return err
		}
		if objects != 4 {
			return fmt.Errorf("SQLite inventory refresh event schema is incomplete during v18 migration recovery")
		}
		manifestVersion := 19
		if version == "20" {
			manifestVersion = 20
		} else if version == "21" {
			manifestVersion = 21
		}
		body, _ := json.Marshal(manifest{Version: manifestVersion, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "19" || version == "20" || version == "21" || version == "22" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS cloud_inventory_refresh_events (sequence INTEGER PRIMARY KEY AUTOINCREMENT,account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),run_id TEXT NOT NULL CHECK(length(run_id)<=128),event_kind TEXT NOT NULL CHECK(event_kind IN ('started','resumed','paused','failed','completed','directory_published')),status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,current_directory TEXT NOT NULL CHECK(length(current_directory)<=4096),pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),items_processed INTEGER NOT NULL CHECK(items_processed>=0),pending_count INTEGER NOT NULL CHECK(pending_count>=0),recorded_at_utc INTEGER NOT NULL,cloud_observed_at_utc INTEGER,directory_changed INTEGER CHECK(directory_changed IN (0,1)))`,
		`CREATE INDEX IF NOT EXISTS cloud_inventory_refresh_events_order ON cloud_inventory_refresh_events(account_fingerprint,run_id,sequence DESC)`,
		`CREATE TRIGGER IF NOT EXISTS cloud_inventory_refresh_events_no_update BEFORE UPDATE ON cloud_inventory_refresh_events BEGIN SELECT RAISE(ABORT, 'cloud inventory refresh events are append-only'); END`,
		`CREATE TRIGGER IF NOT EXISTS cloud_inventory_refresh_events_no_delete BEFORE DELETE ON cloud_inventory_refresh_events BEGIN SELECT RAISE(ABORT, 'cloud inventory refresh events are append-only'); END`,
		`UPDATE store_meta SET value='19' WHERE key='schema_version' AND value='18'`,
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

// migrateV19 adds the account-scoped start-event index used for stable run history paging.
func migrateV19(store *Store, scope string) error {
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
	if gotScope != scope || (version != "19" && version != "20" && version != "21" && version != "22" && version != "23" && version != "24" && version != "25" && version != "26") {
		return fmt.Errorf("SQLite store version or scope mismatch during v19 migration")
	}
	verify := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v19 migration: %s", integrity)
		}
		var indexes int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='index' AND name='cloud_inventory_refresh_runs_order'`).Scan(&indexes); err != nil {
			return err
		}
		if indexes != 1 {
			return fmt.Errorf("SQLite refresh run index is incomplete during v19 migration recovery")
		}
		manifestVersion := 20
		if version == "21" {
			manifestVersion = 21
		}
		body, _ := json.Marshal(manifest{Version: manifestVersion, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "20" || version == "21" || version == "22" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS cloud_inventory_refresh_runs_order ON cloud_inventory_refresh_events(account_fingerprint,event_kind,sequence DESC)`,
		`UPDATE store_meta SET value='20' WHERE key='schema_version' AND value='19'`,
	} {
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

// migrateV20 adds compact direct-child delta counts to directory refresh events.
// SQLite is advanced before the manifest so startup can recover a committed schema change.
func migrateV20(store *Store, scope string) error {
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
	if gotScope != scope || (version != "20" && version != "21" && version != "22" && version != "23" && version != "24" && version != "25" && version != "26") {
		return fmt.Errorf("SQLite store version or scope mismatch during v20 migration")
	}
	verify := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v20 migration: %s", integrity)
		}
		var columns int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_events') WHERE name IN ('added_count','removed_count','changed_count')`).Scan(&columns); err != nil {
			return err
		}
		if columns != 3 {
			return fmt.Errorf("SQLite refresh delta schema is incomplete during v20 migration recovery")
		}
		manifestVersion := 21
		if version == "22" || version == "23" || version == "24" || version == "25" || version == "26" {
			var objects int
			if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE (type='table' AND name='cloud_inventory_refresh_changes') OR (type='index' AND name='cloud_inventory_refresh_changes_order') OR (type='trigger' AND name IN ('cloud_inventory_refresh_changes_no_update','cloud_inventory_refresh_changes_no_delete'))`).Scan(&objects); err != nil {
				return err
			}
			if objects != 4 {
				return fmt.Errorf("SQLite refresh change schema is incomplete during v20 migration recovery")
			}
			manifestVersion = 22
			if version == "23" || version == "24" || version == "25" || version == "26" {
				manifestVersion = 23
			}
		}
		body, _ := json.Marshal(manifest{Version: manifestVersion, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "21" || version == "22" || version == "23" || version == "24" || version == "25" || version == "26" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	columns := map[string]bool{}
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(cloud_inventory_refresh_events)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	for rows.Next() {
		var ordinal, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err = rows.Scan(&ordinal, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			_ = tx.Rollback()
			return err
		}
		columns[name] = true
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		_ = tx.Rollback()
		return err
	}
	if err = rows.Close(); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, column := range []struct{ name, ddl string }{
		{"added_count", `ALTER TABLE cloud_inventory_refresh_events ADD COLUMN added_count INTEGER CHECK(added_count>=0)`},
		{"removed_count", `ALTER TABLE cloud_inventory_refresh_events ADD COLUMN removed_count INTEGER CHECK(removed_count>=0)`},
		{"changed_count", `ALTER TABLE cloud_inventory_refresh_events ADD COLUMN changed_count INTEGER CHECK(changed_count>=0)`},
	} {
		if columns[column.name] {
			continue
		}
		if _, err = tx.ExecContext(ctx, column.ddl); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='21' WHERE key='schema_version' AND value='20'`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return verify()
}

// migrateV21 creates the append-only per-directory refresh change detail table.
// The SQLite schema version commits before manifest publication so restart can recover it.
func migrateV21(store *Store, scope string) error {
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
	if gotScope != scope || (version != "21" && version != "22" && version != "23" && version != "24" && version != "25" && version != "26") {
		return fmt.Errorf("SQLite store version or scope mismatch during v21 migration")
	}
	verify := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v21 migration: %s", integrity)
		}
		var objects int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE (type='table' AND name='cloud_inventory_refresh_changes') OR (type='index' AND name='cloud_inventory_refresh_changes_order') OR (type='trigger' AND name IN ('cloud_inventory_refresh_changes_no_update','cloud_inventory_refresh_changes_no_delete'))`).Scan(&objects); err != nil {
			return err
		}
		if objects != 4 {
			return fmt.Errorf("SQLite refresh change detail schema is incomplete during v21 migration recovery")
		}
		manifestVersion := 22
		if version == "23" || version == "24" || version == "25" || version == "26" {
			manifestVersion = 23
		}
		body, _ := json.Marshal(manifest{Version: manifestVersion, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "22" || version == "23" || version == "24" || version == "25" || version == "26" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS cloud_inventory_refresh_changes (change_sequence INTEGER PRIMARY KEY AUTOINCREMENT,account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),run_id TEXT NOT NULL CHECK(length(run_id)<=128),event_sequence INTEGER NOT NULL CHECK(event_sequence>0),ordinal INTEGER NOT NULL CHECK(ordinal>0),change_kind TEXT NOT NULL CHECK(change_kind IN ('added','removed','metadata_changed','identity_changed')),old_snapshot TEXT CHECK(old_snapshot IS NULL OR length(old_snapshot)<=8192),new_snapshot TEXT CHECK(new_snapshot IS NULL OR length(new_snapshot)<=8192),recorded_at_utc INTEGER NOT NULL,cloud_observed_at_utc INTEGER NOT NULL,CHECK(old_snapshot IS NOT NULL OR new_snapshot IS NOT NULL),UNIQUE(account_fingerprint,run_id,event_sequence,ordinal))`,
		`CREATE INDEX IF NOT EXISTS cloud_inventory_refresh_changes_order ON cloud_inventory_refresh_changes(account_fingerprint,run_id,event_sequence,change_sequence DESC)`,
		`CREATE TRIGGER IF NOT EXISTS cloud_inventory_refresh_changes_no_update BEFORE UPDATE ON cloud_inventory_refresh_changes BEGIN SELECT RAISE(ABORT, 'cloud inventory refresh changes are append-only'); END`,
		`CREATE TRIGGER IF NOT EXISTS cloud_inventory_refresh_changes_no_delete BEFORE DELETE ON cloud_inventory_refresh_changes BEGIN SELECT RAISE(ABORT, 'cloud inventory refresh changes are append-only'); END`,
		`UPDATE store_meta SET value='22' WHERE key='schema_version' AND value='21'`,
	} {
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

// migrateV22 adds durable, account-scoped retry schedules to both inventory
// scan checkpoints. The old rows begin with no scheduled retry.
func migrateV22(store *Store, scope string) error {
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
	if gotScope != scope || (version != "22" && version != "23" && version != "24" && version != "25" && version != "26") {
		return fmt.Errorf("SQLite store version or scope mismatch during v22 migration")
	}
	verify := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v22 migration: %s", integrity)
		}
		var columns int
		if err = db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM pragma_table_info('cloud_inventory_scan_state') WHERE name IN ('retry_delay_millis','retry_at_utc')) + (SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_state') WHERE name IN ('retry_delay_millis','retry_at_utc'))`).Scan(&columns); err != nil {
			return err
		}
		if columns != 4 {
			return fmt.Errorf("SQLite retry checkpoint schema is incomplete during v22 migration recovery")
		}
		body, _ := json.Marshal(manifest{Version: 23, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "23" || version == "24" || version == "25" || version == "26" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, column := range []struct{ table, name, definition string }{
		{"cloud_inventory_scan_state", "retry_delay_millis", `INTEGER NOT NULL DEFAULT 0 CHECK(retry_delay_millis>=0 AND retry_delay_millis<=3600000)`},
		{"cloud_inventory_scan_state", "retry_at_utc", `INTEGER CHECK(retry_at_utc IS NULL OR retry_at_utc>0)`},
		{"cloud_inventory_refresh_state", "retry_delay_millis", `INTEGER NOT NULL DEFAULT 0 CHECK(retry_delay_millis>=0 AND retry_delay_millis<=3600000)`},
		{"cloud_inventory_refresh_state", "retry_at_utc", `INTEGER CHECK(retry_at_utc IS NULL OR retry_at_utc>0)`},
	} {
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name=?`, column.table, column.name).Scan(&exists); err != nil {
			_ = tx.Rollback()
			return err
		}
		if exists == 0 {
			statement := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, column.table, column.name, column.definition)
			if _, err = tx.ExecContext(ctx, statement); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='23' WHERE key='schema_version' AND value='22'`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return verify()
}

// migrateV23 adds resumable per-file duplicate SHA-256 checkpoints.
func migrateV23(store *Store, scope string) error {
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
	if gotScope != scope || (version != "23" && version != "24" && version != "25" && version != "26") {
		return fmt.Errorf("SQLite store version or scope mismatch during v23 migration")
	}
	columns := []struct{ name, definition string }{
		{"current_fs_id", `INTEGER NOT NULL DEFAULT 0 CHECK(current_fs_id>=0)`},
		{"current_path", `TEXT NOT NULL DEFAULT ''`},
		{"current_name", `TEXT NOT NULL DEFAULT ''`},
		{"current_size", `INTEGER NOT NULL DEFAULT 0 CHECK(current_size>=0)`},
		{"current_provider_mtime", `INTEGER NOT NULL DEFAULT 0`},
		{"current_content_md5", `TEXT NOT NULL DEFAULT ''`},
		{"current_bytes", `INTEGER NOT NULL DEFAULT 0 CHECK(current_bytes>=0)`},
		{"current_sha256_state", `BLOB NOT NULL DEFAULT X'' CHECK(length(current_sha256_state)<=1024)`},
	}
	verify := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v23 migration: %s", integrity)
		}
		rows, queryErr := db.QueryContext(ctx, `PRAGMA table_info(cloud_duplicate_scan_state)`)
		if queryErr != nil {
			return queryErr
		}
		found := map[string]bool{}
		for rows.Next() {
			var cid, notNull, primaryKey int
			var name, kind string
			var defaultValue any
			if queryErr = rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); queryErr != nil {
				_ = rows.Close()
				return queryErr
			}
			found[name] = true
		}
		if queryErr = rows.Err(); queryErr != nil {
			_ = rows.Close()
			return queryErr
		}
		if queryErr = rows.Close(); queryErr != nil {
			return queryErr
		}
		for _, column := range columns {
			if !found[column.name] {
				return fmt.Errorf("SQLite duplicate hash checkpoint column %s is missing", column.name)
			}
		}
		body, _ := json.Marshal(manifest{Version: 24, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "24" || version == "25" || version == "26" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, column := range columns {
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_duplicate_scan_state') WHERE name=?`, column.name).Scan(&exists); err != nil {
			_ = tx.Rollback()
			return err
		}
		if exists != 0 {
			continue
		}
		if _, err = tx.ExecContext(ctx, `ALTER TABLE cloud_duplicate_scan_state ADD COLUMN `+column.name+` `+column.definition); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='24' WHERE key='schema_version' AND value='23'`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return verify()
}

func migrateV24(store *Store, scope string) error {
	db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(store.dir, "queue.sqlite")))
	if err != nil {
		return err
	}
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
	if gotScope != scope || (version != "24" && version != "25" && version != "26") {
		return fmt.Errorf("SQLite store version or scope mismatch during v24 migration")
	}
	verify := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v24 migration: %s", integrity)
		}
		var found int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_entries') WHERE name='observed_at_utc'`).Scan(&found); err != nil {
			return err
		}
		if found != 1 {
			return fmt.Errorf("SQLite refresh page observation column is missing")
		}
		body, _ := json.Marshal(manifest{Version: 25, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "25" || version == "26" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_entries') WHERE name='observed_at_utc'`).Scan(&exists); err != nil {
		_ = tx.Rollback()
		return err
	}
	if exists == 0 {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE cloud_inventory_refresh_entries ADD COLUMN observed_at_utc INTEGER NOT NULL DEFAULT 0 CHECK(observed_at_utc>=0)`); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='25' WHERE key='schema_version' AND value='24'`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return verify()
}

// migrateV25 stores the next exponential retry base separately from the most
// recent scheduled delay so a worker restart does not reset its backoff.
func migrateV25(store *Store, scope string) error {
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
	if gotScope != scope || (version != "25" && version != "26") {
		return fmt.Errorf("SQLite store version or scope mismatch during v25 migration")
	}
	tables := []struct{ name, delayColumn string }{
		{"cloud_inventory_scan_state", "retry_delay_millis"},
		{"cloud_inventory_refresh_state", "retry_delay_millis"},
		{"cloud_duplicate_scan_state", "retry_delay_ms"},
	}
	verify := func() error {
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v25 migration: %s", integrity)
		}
		for _, table := range tables {
			var found int
			if err = db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name='retry_backoff_base_millis'`, table.name).Scan(&found); err != nil {
				return err
			}
			if found != 1 {
				return fmt.Errorf("SQLite %s backoff column is missing", table.name)
			}
		}
		body, _ := json.Marshal(manifest{Version: 26, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "26" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, table := range tables {
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name='retry_backoff_base_millis'`, table.name).Scan(&exists); err != nil {
			_ = tx.Rollback()
			return err
		}
		if exists == 0 {
			statement := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN retry_backoff_base_millis INTEGER NOT NULL DEFAULT 0 CHECK(retry_backoff_base_millis>=0 AND retry_backoff_base_millis<=3600000)`, table.name)
			if _, err = tx.ExecContext(ctx, statement); err != nil {
				_ = tx.Rollback()
				return err
			}
			statement = fmt.Sprintf(`UPDATE %s SET retry_backoff_base_millis=CASE WHEN %s>=30000 THEN 60000 ELSE %s*2 END WHERE %s>0`, table.name, table.delayColumn, table.delayColumn, table.delayColumn)
			if _, err = tx.ExecContext(ctx, statement); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='26' WHERE key='schema_version' AND value='25'`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return verify()
}

// migrateV26 persists page lookahead identities beside the two resumable
// directory-scan offsets so page continuity checks survive process restart.
func migrateV26(store *Store, scope string) error {
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
	if gotScope != scope || (version != "26" && version != "27") {
		return fmt.Errorf("SQLite store version or scope mismatch during v26 migration")
	}
	verify := func() error {
		for _, table := range []string{"cloud_inventory_scan_state", "cloud_inventory_refresh_state"} {
			var found int
			if err = db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name='next_page_boundary_json'`, table).Scan(&found); err != nil {
				return err
			}
			if found != 1 {
				return fmt.Errorf("SQLite %s page boundary column is missing", table)
			}
		}
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v26 migration: %s", integrity)
		}
		body, _ := json.Marshal(manifest{Version: 27, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "27" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, table := range []string{"cloud_inventory_scan_state", "cloud_inventory_refresh_state"} {
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name='next_page_boundary_json'`, table).Scan(&exists); err != nil {
			_ = tx.Rollback()
			return err
		}
		if exists == 0 {
			statement := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN next_page_boundary_json TEXT NOT NULL DEFAULT '' CHECK(length(next_page_boundary_json)<=8192 AND (next_page_boundary_json='' OR json_valid(next_page_boundary_json)))`, table)
			if _, err = tx.ExecContext(ctx, statement); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE cloud_inventory SET last_seen_run_id='' WHERE (account_fingerprint,parent_path,last_seen_run_id) IN (SELECT account_fingerprint,current_directory,run_id FROM cloud_inventory_scan_state WHERE next_offset>0 AND pending_count>0 AND status IN ('running','paused','failed'))`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE cloud_inventory_scan_state SET next_offset=0 WHERE next_offset>0 AND pending_count>0 AND status IN ('running','paused','failed')`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE cloud_inventory_refresh_state SET next_offset=0 WHERE next_offset>0 AND pending_count>0 AND status IN ('running','paused','failed')`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='27' WHERE key='schema_version' AND value='26'`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return verify()
}

// migrateV27 adds durable per-run request budgets to scoped cloud refreshes and
// snapshots those counters in the existing append-only refresh event stream.
func migrateV27(store *Store, scope string) error {
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
	if gotScope != scope || (version != "27" && version != "28") {
		return fmt.Errorf("SQLite store version or scope mismatch during v27 migration")
	}
	columns := []struct {
		table string
		name  string
	}{
		{"cloud_inventory_refresh_state", "request_budget"},
		{"cloud_inventory_refresh_state", "requests_used"},
		{"cloud_inventory_refresh_events", "request_budget"},
		{"cloud_inventory_refresh_events", "requests_used"},
	}
	verify := func() error {
		for _, column := range columns {
			var found int
			if err = db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name=?`, column.table, column.name).Scan(&found); err != nil {
				return err
			}
			if found != 1 {
				return fmt.Errorf("SQLite %s.%s budget column is missing", column.table, column.name)
			}
		}
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v27 migration: %s", integrity)
		}
		body, _ := json.Marshal(manifest{Version: 28, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "28" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, column := range columns {
		var found int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name=?`, column.table, column.name).Scan(&found); err != nil {
			_ = tx.Rollback()
			return err
		}
		if found == 0 {
			statement := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s INTEGER NOT NULL DEFAULT 0 CHECK(%s>=0)`, column.table, column.name, column.name)
			if _, err = tx.ExecContext(ctx, statement); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='28' WHERE key='schema_version' AND value='27'`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return verify()
}

// migrateV28 adds durable request-budget counters to account-wide inventory scans.
func migrateV28(store *Store, scope string) error {
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
	if gotScope != scope || (version != "28" && version != "29") {
		return fmt.Errorf("SQLite store version or scope mismatch during v28 migration")
	}
	verify := func() error {
		for _, name := range []string{"request_budget", "requests_used"} {
			var found int
			if err = db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_scan_state') WHERE name=?`, name).Scan(&found); err != nil {
				return err
			}
			if found != 1 {
				return fmt.Errorf("SQLite cloud_inventory_scan_state.%s column is missing", name)
			}
		}
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v28 migration: %s", integrity)
		}
		body, _ := json.Marshal(manifest{Version: 29, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "29" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, name := range []string{"request_budget", "requests_used"} {
		var found int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_scan_state') WHERE name=?`, name).Scan(&found); err != nil {
			_ = tx.Rollback()
			return err
		}
		if found == 0 {
			definition := `INTEGER NOT NULL DEFAULT 0 CHECK(request_budget>=0)`
			if name == "requests_used" {
				definition = `INTEGER NOT NULL DEFAULT 0 CHECK(requests_used>=0 AND (request_budget=0 OR requests_used<=request_budget))`
			}
			if _, err = tx.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE cloud_inventory_scan_state ADD COLUMN %s %s`, name, definition)); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='29' WHERE key='schema_version' AND value='28'`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return verify()
}

func migrateV29(store *Store, scope string) error {
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
	if gotScope != scope || (version != "29" && version != "30") {
		return fmt.Errorf("SQLite store version or scope mismatch during v29 migration")
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS cloud_scan_runs(sequence INTEGER PRIMARY KEY AUTOINCREMENT,account_fingerprint TEXT NOT NULL,run_id TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,current_directory TEXT NOT NULL,pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),items_processed INTEGER NOT NULL CHECK(items_processed>=0),request_budget INTEGER NOT NULL CHECK(request_budget>=0),requests_used INTEGER NOT NULL CHECK(requests_used>=0),pending_count INTEGER NOT NULL CHECK(pending_count>=0),started_at_utc INTEGER NOT NULL,updated_at_utc INTEGER NOT NULL,last_error TEXT NOT NULL,UNIQUE(account_fingerprint,run_id))`,
		`CREATE INDEX IF NOT EXISTS cloud_scan_runs_order ON cloud_scan_runs(account_fingerprint,sequence DESC)`,
		`CREATE TABLE IF NOT EXISTS cloud_inventory_scan_events(sequence INTEGER PRIMARY KEY AUTOINCREMENT,account_fingerprint TEXT NOT NULL,run_id TEXT NOT NULL,event_kind TEXT NOT NULL CHECK(event_kind IN ('started','resumed','paused','request_budget_reached','retry_scheduled','failed','completed')),status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,current_directory TEXT NOT NULL,pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),items_processed INTEGER NOT NULL CHECK(items_processed>=0),request_budget INTEGER NOT NULL CHECK(request_budget>=0),requests_used INTEGER NOT NULL CHECK(requests_used>=0),pending_count INTEGER NOT NULL CHECK(pending_count>=0),recorded_at_utc INTEGER NOT NULL,last_error TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS cloud_inventory_scan_events_order ON cloud_inventory_scan_events(account_fingerprint,run_id,sequence DESC)`,
		`CREATE TRIGGER IF NOT EXISTS cloud_inventory_scan_events_no_update BEFORE UPDATE ON cloud_inventory_scan_events BEGIN SELECT RAISE(ABORT, 'cloud scan events are append-only'); END`,
		`CREATE TRIGGER IF NOT EXISTS cloud_inventory_scan_events_no_delete BEFORE DELETE ON cloud_inventory_scan_events BEGIN SELECT RAISE(ABORT, 'cloud scan events are append-only'); END`,
	}
	verify := func() error {
		for _, name := range []string{"cloud_scan_runs", "cloud_inventory_scan_events"} {
			var found int
			if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&found); err != nil {
				return err
			}
			if found != 1 {
				return fmt.Errorf("SQLite scan history table %s is missing", name)
			}
		}
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v29 migration: %s", integrity)
		}
		body, _ := json.Marshal(manifest{Version: 30, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "30" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='30' WHERE key='schema_version' AND value='29'`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return verify()
}

// migrateV30 rebuilds the append-only scan event tables to admit boundary
// restart evidence and indexes only those rows for bounded per-directory counts.
func migrateV30(store *Store, scope string) error {
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
	if gotScope != scope || (version != "30" && version != "31") {
		return fmt.Errorf("SQLite store version or scope mismatch during v30 migration")
	}
	verify := func() error {
		for _, name := range []string{"cloud_inventory_scan_boundary_restart_count", "cloud_inventory_refresh_boundary_restart_count"} {
			var found int
			if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&found); err != nil {
				return err
			}
			if found != 1 {
				return fmt.Errorf("SQLite boundary restart index %s is missing", name)
			}
		}
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v30 migration: %s", integrity)
		}
		body, _ := json.Marshal(manifest{Version: 31, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "31" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	statements := []string{
		`DROP TRIGGER cloud_inventory_scan_events_no_update`,
		`DROP TRIGGER cloud_inventory_scan_events_no_delete`,
		`DROP INDEX cloud_inventory_scan_events_order`,
		`DROP TABLE IF EXISTS cloud_inventory_scan_events_v31`,
		`CREATE TABLE cloud_inventory_scan_events_v31 (sequence INTEGER PRIMARY KEY AUTOINCREMENT,account_fingerprint TEXT NOT NULL,run_id TEXT NOT NULL,event_kind TEXT NOT NULL CHECK(event_kind IN ('started','resumed','paused','request_budget_reached','retry_scheduled','boundary_restart','failed','completed')),status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,current_directory TEXT NOT NULL,pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),items_processed INTEGER NOT NULL CHECK(items_processed>=0),request_budget INTEGER NOT NULL CHECK(request_budget>=0),requests_used INTEGER NOT NULL CHECK(requests_used>=0),pending_count INTEGER NOT NULL CHECK(pending_count>=0),recorded_at_utc INTEGER NOT NULL,last_error TEXT NOT NULL)`,
		`INSERT INTO cloud_inventory_scan_events_v31(sequence,account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,recorded_at_utc,last_error) SELECT sequence,account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,recorded_at_utc,last_error FROM cloud_inventory_scan_events`,
		`DROP TABLE cloud_inventory_scan_events`,
		`ALTER TABLE cloud_inventory_scan_events_v31 RENAME TO cloud_inventory_scan_events`,
		`CREATE INDEX cloud_inventory_scan_events_order ON cloud_inventory_scan_events(account_fingerprint,run_id,sequence DESC)`,
		`CREATE INDEX cloud_inventory_scan_boundary_restart_count ON cloud_inventory_scan_events(account_fingerprint,run_id,current_directory) WHERE event_kind='boundary_restart'`,
		`CREATE TRIGGER cloud_inventory_scan_events_no_update BEFORE UPDATE ON cloud_inventory_scan_events BEGIN SELECT RAISE(ABORT, 'cloud scan events are append-only'); END`,
		`CREATE TRIGGER cloud_inventory_scan_events_no_delete BEFORE DELETE ON cloud_inventory_scan_events BEGIN SELECT RAISE(ABORT, 'cloud scan events are append-only'); END`,
		`DROP TRIGGER cloud_inventory_refresh_events_no_update`,
		`DROP TRIGGER cloud_inventory_refresh_events_no_delete`,
		`DROP INDEX cloud_inventory_refresh_events_order`,
		`DROP INDEX cloud_inventory_refresh_runs_order`,
		`DROP TABLE IF EXISTS cloud_inventory_refresh_events_v31`,
		`CREATE TABLE cloud_inventory_refresh_events_v31 (sequence INTEGER PRIMARY KEY AUTOINCREMENT,account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),run_id TEXT NOT NULL CHECK(length(run_id)<=128),event_kind TEXT NOT NULL CHECK(event_kind IN ('started','resumed','paused','boundary_restart','failed','completed','directory_published')),status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,current_directory TEXT NOT NULL CHECK(length(current_directory)<=4096),pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),items_processed INTEGER NOT NULL CHECK(items_processed>=0),request_budget INTEGER NOT NULL DEFAULT 0 CHECK(request_budget>=0),requests_used INTEGER NOT NULL DEFAULT 0 CHECK(requests_used>=0),pending_count INTEGER NOT NULL CHECK(pending_count>=0),recorded_at_utc INTEGER NOT NULL,cloud_observed_at_utc INTEGER,directory_changed INTEGER CHECK(directory_changed IN (0,1)),added_count INTEGER CHECK(added_count>=0),removed_count INTEGER CHECK(removed_count>=0),changed_count INTEGER CHECK(changed_count>=0))`,
		`INSERT INTO cloud_inventory_refresh_events_v31(sequence,account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,recorded_at_utc,cloud_observed_at_utc,directory_changed,added_count,removed_count,changed_count) SELECT sequence,account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,recorded_at_utc,cloud_observed_at_utc,directory_changed,added_count,removed_count,changed_count FROM cloud_inventory_refresh_events`,
		`DROP TABLE cloud_inventory_refresh_events`,
		`ALTER TABLE cloud_inventory_refresh_events_v31 RENAME TO cloud_inventory_refresh_events`,
		`CREATE INDEX cloud_inventory_refresh_events_order ON cloud_inventory_refresh_events(account_fingerprint,run_id,sequence DESC)`,
		`CREATE INDEX cloud_inventory_refresh_runs_order ON cloud_inventory_refresh_events(account_fingerprint,event_kind,sequence DESC)`,
		`CREATE INDEX cloud_inventory_refresh_boundary_restart_count ON cloud_inventory_refresh_events(account_fingerprint,run_id,current_directory) WHERE event_kind='boundary_restart'`,
		`CREATE TRIGGER cloud_inventory_refresh_events_no_update BEFORE UPDATE ON cloud_inventory_refresh_events BEGIN SELECT RAISE(ABORT, 'cloud inventory refresh events are append-only'); END`,
		`CREATE TRIGGER cloud_inventory_refresh_events_no_delete BEFORE DELETE ON cloud_inventory_refresh_events BEGIN SELECT RAISE(ABORT, 'cloud inventory refresh events are append-only'); END`,
		`UPDATE store_meta SET value='31' WHERE key='schema_version' AND value='30'`,
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

// migrateV31 adds a durable query projection for scoped refresh history and
// backfills the summary from its append-only event stream.
func migrateV31(store *Store, scope string) error {
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
	if gotScope != scope || (version != "31" && version != "32") {
		return fmt.Errorf("SQLite store version or scope mismatch during v31 migration")
	}
	verify := func() error {
		var tableCount, indexCount int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='cloud_inventory_refresh_runs'`).Scan(&tableCount); err != nil {
			return err
		}
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='index' AND name='cloud_inventory_refresh_run_order'`).Scan(&indexCount); err != nil {
			return err
		}
		if tableCount != 1 || indexCount != 1 {
			return fmt.Errorf("SQLite scoped refresh history projection is incomplete")
		}
		var eventColumns int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_events') WHERE name IN ('request_budget_added','retry_at_utc')`).Scan(&eventColumns); err != nil {
			return err
		}
		if eventColumns != 2 {
			return fmt.Errorf("SQLite scoped refresh event evidence schema is incomplete")
		}
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v31 migration: %s", integrity)
		}
		body, _ := json.Marshal(manifest{Version: 32, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "32" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	statements := []string{
		`DROP TRIGGER IF EXISTS cloud_inventory_refresh_events_no_update`,
		`DROP TRIGGER IF EXISTS cloud_inventory_refresh_events_no_delete`,
		`DROP INDEX IF EXISTS cloud_inventory_refresh_events_order`,
		`DROP INDEX IF EXISTS cloud_inventory_refresh_runs_order`,
		`DROP INDEX IF EXISTS cloud_inventory_refresh_boundary_restart_count`,
		`DROP TABLE IF EXISTS cloud_inventory_refresh_events_v32`,
		`CREATE TABLE cloud_inventory_refresh_events_v32(sequence INTEGER PRIMARY KEY AUTOINCREMENT,account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),run_id TEXT NOT NULL CHECK(length(run_id)<=128),event_kind TEXT NOT NULL CHECK(event_kind IN ('started','resumed','paused','request_budget_reached','retry_scheduled','boundary_restart','failed','completed','directory_published')),status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,current_directory TEXT NOT NULL CHECK(length(current_directory)<=4096),pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),items_processed INTEGER NOT NULL CHECK(items_processed>=0),request_budget INTEGER NOT NULL DEFAULT 0 CHECK(request_budget>=0),requests_used INTEGER NOT NULL DEFAULT 0 CHECK(requests_used>=0),request_budget_added INTEGER NOT NULL DEFAULT 0 CHECK(request_budget_added>=0),retry_at_utc INTEGER,pending_count INTEGER NOT NULL CHECK(pending_count>=0),recorded_at_utc INTEGER NOT NULL,cloud_observed_at_utc INTEGER,directory_changed INTEGER CHECK(directory_changed IN (0,1)),added_count INTEGER CHECK(added_count>=0),removed_count INTEGER CHECK(removed_count>=0),changed_count INTEGER CHECK(changed_count>=0),last_error TEXT NOT NULL DEFAULT '')`,
		`INSERT INTO cloud_inventory_refresh_events_v32(sequence,account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,recorded_at_utc,cloud_observed_at_utc,directory_changed,added_count,removed_count,changed_count) SELECT sequence,account_fingerprint,run_id,event_kind,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,recorded_at_utc,cloud_observed_at_utc,directory_changed,added_count,removed_count,changed_count FROM cloud_inventory_refresh_events`,
		`DROP TABLE cloud_inventory_refresh_events`,
		`ALTER TABLE cloud_inventory_refresh_events_v32 RENAME TO cloud_inventory_refresh_events`,
		`CREATE INDEX cloud_inventory_refresh_events_order ON cloud_inventory_refresh_events(account_fingerprint,run_id,sequence DESC)`,
		`CREATE INDEX cloud_inventory_refresh_runs_order ON cloud_inventory_refresh_events(account_fingerprint,event_kind,sequence DESC)`,
		`CREATE INDEX cloud_inventory_refresh_boundary_restart_count ON cloud_inventory_refresh_events(account_fingerprint,run_id,current_directory) WHERE event_kind='boundary_restart'`,
		`CREATE TRIGGER cloud_inventory_refresh_events_no_update BEFORE UPDATE ON cloud_inventory_refresh_events BEGIN SELECT RAISE(ABORT, 'cloud inventory refresh events are append-only'); END`,
		`CREATE TRIGGER cloud_inventory_refresh_events_no_delete BEFORE DELETE ON cloud_inventory_refresh_events BEGIN SELECT RAISE(ABORT, 'cloud inventory refresh events are append-only'); END`,
		`CREATE TABLE IF NOT EXISTS cloud_inventory_refresh_runs(start_sequence INTEGER PRIMARY KEY,account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),run_id TEXT NOT NULL CHECK(length(run_id)<=128),root_path TEXT NOT NULL CHECK(length(root_path)<=4096),status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,current_directory TEXT NOT NULL CHECK(length(current_directory)<=4096),pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),items_processed INTEGER NOT NULL CHECK(items_processed>=0),request_budget INTEGER NOT NULL CHECK(request_budget>=0),requests_used INTEGER NOT NULL CHECK(requests_used>=0),pending_count INTEGER NOT NULL CHECK(pending_count>=0),started_at_utc INTEGER NOT NULL,updated_at_utc INTEGER NOT NULL,last_error TEXT NOT NULL,latest_sequence INTEGER NOT NULL CHECK(latest_sequence>=start_sequence),latest_kind TEXT NOT NULL,UNIQUE(account_fingerprint,run_id))`,
		`CREATE INDEX IF NOT EXISTS cloud_inventory_refresh_run_order ON cloud_inventory_refresh_runs(account_fingerprint,start_sequence DESC)`,
		`INSERT OR IGNORE INTO cloud_inventory_refresh_runs(start_sequence,account_fingerprint,run_id,root_path,status,phase,current_directory,pages_processed,items_processed,request_budget,requests_used,pending_count,started_at_utc,updated_at_utc,last_error,latest_sequence,latest_kind) SELECT started.sequence,started.account_fingerprint,started.run_id,started.current_directory,latest.status,latest.phase,latest.current_directory,latest.pages_processed,latest.items_processed,latest.request_budget,latest.requests_used,latest.pending_count,started.recorded_at_utc,latest.recorded_at_utc,'',latest.sequence,latest.event_kind FROM cloud_inventory_refresh_events AS started JOIN cloud_inventory_refresh_events AS latest ON latest.account_fingerprint=started.account_fingerprint AND latest.run_id=started.run_id AND latest.sequence=(SELECT MAX(newest.sequence) FROM cloud_inventory_refresh_events AS newest WHERE newest.account_fingerprint=started.account_fingerprint AND newest.run_id=started.run_id) WHERE started.event_kind='started'`,
		`UPDATE cloud_inventory_refresh_runs SET root_path=(SELECT current_directory FROM cloud_inventory_refresh_events e WHERE e.account_fingerprint=cloud_inventory_refresh_runs.account_fingerprint AND e.run_id=cloud_inventory_refresh_runs.run_id AND e.event_kind='started' ORDER BY e.sequence LIMIT 1),status=(SELECT status FROM cloud_inventory_refresh_events e WHERE e.account_fingerprint=cloud_inventory_refresh_runs.account_fingerprint AND e.run_id=cloud_inventory_refresh_runs.run_id ORDER BY e.sequence DESC LIMIT 1),phase=(SELECT phase FROM cloud_inventory_refresh_events e WHERE e.account_fingerprint=cloud_inventory_refresh_runs.account_fingerprint AND e.run_id=cloud_inventory_refresh_runs.run_id ORDER BY e.sequence DESC LIMIT 1),current_directory=(SELECT current_directory FROM cloud_inventory_refresh_events e WHERE e.account_fingerprint=cloud_inventory_refresh_runs.account_fingerprint AND e.run_id=cloud_inventory_refresh_runs.run_id ORDER BY e.sequence DESC LIMIT 1),pages_processed=(SELECT pages_processed FROM cloud_inventory_refresh_events e WHERE e.account_fingerprint=cloud_inventory_refresh_runs.account_fingerprint AND e.run_id=cloud_inventory_refresh_runs.run_id ORDER BY e.sequence DESC LIMIT 1),items_processed=(SELECT items_processed FROM cloud_inventory_refresh_events e WHERE e.account_fingerprint=cloud_inventory_refresh_runs.account_fingerprint AND e.run_id=cloud_inventory_refresh_runs.run_id ORDER BY e.sequence DESC LIMIT 1),request_budget=(SELECT request_budget FROM cloud_inventory_refresh_events e WHERE e.account_fingerprint=cloud_inventory_refresh_runs.account_fingerprint AND e.run_id=cloud_inventory_refresh_runs.run_id ORDER BY e.sequence DESC LIMIT 1),requests_used=(SELECT requests_used FROM cloud_inventory_refresh_events e WHERE e.account_fingerprint=cloud_inventory_refresh_runs.account_fingerprint AND e.run_id=cloud_inventory_refresh_runs.run_id ORDER BY e.sequence DESC LIMIT 1),pending_count=(SELECT pending_count FROM cloud_inventory_refresh_events e WHERE e.account_fingerprint=cloud_inventory_refresh_runs.account_fingerprint AND e.run_id=cloud_inventory_refresh_runs.run_id ORDER BY e.sequence DESC LIMIT 1),updated_at_utc=(SELECT recorded_at_utc FROM cloud_inventory_refresh_events e WHERE e.account_fingerprint=cloud_inventory_refresh_runs.account_fingerprint AND e.run_id=cloud_inventory_refresh_runs.run_id ORDER BY e.sequence DESC LIMIT 1),latest_sequence=(SELECT sequence FROM cloud_inventory_refresh_events e WHERE e.account_fingerprint=cloud_inventory_refresh_runs.account_fingerprint AND e.run_id=cloud_inventory_refresh_runs.run_id ORDER BY e.sequence DESC LIMIT 1),latest_kind=(SELECT event_kind FROM cloud_inventory_refresh_events e WHERE e.account_fingerprint=cloud_inventory_refresh_runs.account_fingerprint AND e.run_id=cloud_inventory_refresh_runs.run_id ORDER BY e.sequence DESC LIMIT 1)`,
		`UPDATE store_meta SET value='32' WHERE key='schema_version' AND value='31'`,
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

// migrateV32 makes existing duplicate-scan transition evidence discoverable by run.
func migrateV32(store *Store, scope string) error {
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
	if gotScope != scope || (version != "32" && version != "33") {
		return fmt.Errorf("SQLite store version or scope mismatch during v32 migration")
	}
	verify := func() error {
		var tableCount, indexCount int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='cloud_duplicate_scan_runs'`).Scan(&tableCount); err != nil {
			return err
		}
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='index' AND name='cloud_duplicate_scan_runs_order'`).Scan(&indexCount); err != nil {
			return err
		}
		if tableCount != 1 || indexCount != 1 {
			return fmt.Errorf("SQLite duplicate scan run history projection is incomplete")
		}
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v32 migration: %s", integrity)
		}
		body, _ := json.Marshal(manifest{Version: 33, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "33" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS cloud_duplicate_scan_runs(start_sequence INTEGER NOT NULL,account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),run_id TEXT NOT NULL CHECK(length(run_id)<=128),status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,next_fs_id INTEGER NOT NULL CHECK(next_fs_id>=0),total_files INTEGER NOT NULL CHECK(total_files>=0),processed_files INTEGER NOT NULL CHECK(processed_files>=0),total_bytes INTEGER NOT NULL CHECK(total_bytes>=0),processed_bytes INTEGER NOT NULL CHECK(processed_bytes>=0),started_at_utc INTEGER NOT NULL,updated_at_utc INTEGER NOT NULL,last_error TEXT NOT NULL,latest_sequence INTEGER NOT NULL CHECK(latest_sequence>=0),UNIQUE(account_fingerprint,run_id))`,
		`CREATE INDEX IF NOT EXISTS cloud_duplicate_scan_runs_order ON cloud_duplicate_scan_runs(account_fingerprint,start_sequence DESC,run_id)`,
		`INSERT OR IGNORE INTO cloud_duplicate_scan_runs(start_sequence,account_fingerprint,run_id,status,phase,next_fs_id,total_files,processed_files,total_bytes,processed_bytes,started_at_utc,updated_at_utc,last_error,latest_sequence) SELECT first.sequence,first.account_fingerprint,first.run_id,last.status,last.phase,coalesce(json_extract(last.evidence_json,'$.next_fs_id'),0),coalesce(json_extract(last.evidence_json,'$.total_files'),0),coalesce(json_extract(last.evidence_json,'$.processed_files'),0),coalesce(json_extract(last.evidence_json,'$.total_bytes'),0),coalesce(json_extract(last.evidence_json,'$.processed_bytes'),0),first.recorded_at_utc,last.recorded_at_utc,coalesce(json_extract(last.evidence_json,'$.last_error'),''),last.sequence FROM cloud_duplicate_scan_events first JOIN cloud_duplicate_scan_events last ON last.account_fingerprint=first.account_fingerprint AND last.run_id=first.run_id AND last.sequence=(SELECT max(newer.sequence) FROM cloud_duplicate_scan_events newer WHERE newer.account_fingerprint=first.account_fingerprint AND newer.run_id=first.run_id) WHERE first.sequence=(SELECT min(oldest.sequence) FROM cloud_duplicate_scan_events oldest WHERE oldest.account_fingerprint=first.account_fingerprint AND oldest.run_id=first.run_id)`,
		`INSERT OR IGNORE INTO cloud_duplicate_scan_runs(start_sequence,account_fingerprint,run_id,status,phase,next_fs_id,total_files,processed_files,total_bytes,processed_bytes,started_at_utc,updated_at_utc,last_error,latest_sequence) SELECT coalesce((SELECT min(e.sequence) FROM cloud_duplicate_scan_events e WHERE e.account_fingerprint=s.account_fingerprint AND e.run_id=s.run_id),0),s.account_fingerprint,s.run_id,s.status,s.phase,s.next_fs_id,s.total_files,s.processed_files,s.total_bytes,s.processed_bytes,s.started_at_utc,s.updated_at_utc,s.last_error,coalesce((SELECT max(e.sequence) FROM cloud_duplicate_scan_events e WHERE e.account_fingerprint=s.account_fingerprint AND e.run_id=s.run_id),0) FROM cloud_duplicate_scan_state s`,
		`UPDATE cloud_duplicate_scan_runs SET status=(SELECT s.status FROM cloud_duplicate_scan_state s WHERE s.account_fingerprint=cloud_duplicate_scan_runs.account_fingerprint AND s.run_id=cloud_duplicate_scan_runs.run_id),phase=(SELECT s.phase FROM cloud_duplicate_scan_state s WHERE s.account_fingerprint=cloud_duplicate_scan_runs.account_fingerprint AND s.run_id=cloud_duplicate_scan_runs.run_id),next_fs_id=(SELECT s.next_fs_id FROM cloud_duplicate_scan_state s WHERE s.account_fingerprint=cloud_duplicate_scan_runs.account_fingerprint AND s.run_id=cloud_duplicate_scan_runs.run_id),total_files=(SELECT s.total_files FROM cloud_duplicate_scan_state s WHERE s.account_fingerprint=cloud_duplicate_scan_runs.account_fingerprint AND s.run_id=cloud_duplicate_scan_runs.run_id),processed_files=(SELECT s.processed_files FROM cloud_duplicate_scan_state s WHERE s.account_fingerprint=cloud_duplicate_scan_runs.account_fingerprint AND s.run_id=cloud_duplicate_scan_runs.run_id),total_bytes=(SELECT s.total_bytes FROM cloud_duplicate_scan_state s WHERE s.account_fingerprint=cloud_duplicate_scan_runs.account_fingerprint AND s.run_id=cloud_duplicate_scan_runs.run_id),processed_bytes=(SELECT s.processed_bytes FROM cloud_duplicate_scan_state s WHERE s.account_fingerprint=cloud_duplicate_scan_runs.account_fingerprint AND s.run_id=cloud_duplicate_scan_runs.run_id),started_at_utc=(SELECT s.started_at_utc FROM cloud_duplicate_scan_state s WHERE s.account_fingerprint=cloud_duplicate_scan_runs.account_fingerprint AND s.run_id=cloud_duplicate_scan_runs.run_id),updated_at_utc=(SELECT s.updated_at_utc FROM cloud_duplicate_scan_state s WHERE s.account_fingerprint=cloud_duplicate_scan_runs.account_fingerprint AND s.run_id=cloud_duplicate_scan_runs.run_id),last_error=(SELECT s.last_error FROM cloud_duplicate_scan_state s WHERE s.account_fingerprint=cloud_duplicate_scan_runs.account_fingerprint AND s.run_id=cloud_duplicate_scan_runs.run_id) WHERE EXISTS(SELECT 1 FROM cloud_duplicate_scan_state s WHERE s.account_fingerprint=cloud_duplicate_scan_runs.account_fingerprint AND s.run_id=cloud_duplicate_scan_runs.run_id)`,
		`UPDATE store_meta SET value='33' WHERE key='schema_version' AND value='32'`,
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

// migrateV33 adds durable, account-scoped automatic queue monitor scan evidence.
func migrateV33(store *Store, scope string) error {
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
	if gotScope != scope || (version != "33" && version != "34") {
		return fmt.Errorf("SQLite store version or scope mismatch during v33 migration")
	}
	verify := func() error {
		var count int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE (type='table' AND name IN ('queue_monitor_scan_runs','queue_monitor_scan_events')) OR (type='index' AND name IN ('queue_monitor_scan_runs_order','queue_monitor_scan_events_order')) OR (type='trigger' AND name IN ('queue_monitor_scan_events_no_update','queue_monitor_scan_events_no_delete'))`).Scan(&count); err != nil {
			return err
		}
		if count != 6 {
			return fmt.Errorf("SQLite queue monitor scan schema is incomplete")
		}
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v33 migration: %s", integrity)
		}
		body, _ := json.Marshal(manifest{Version: 34, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "34" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS queue_monitor_scan_runs(sequence INTEGER PRIMARY KEY AUTOINCREMENT,account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),run_id TEXT NOT NULL CHECK(length(run_id)=32),status TEXT NOT NULL CHECK(status IN ('running','success','failed','interrupted')),monitor_path TEXT NOT NULL CHECK(length(monitor_path)<=4096),started_at_utc INTEGER NOT NULL,finished_at_utc INTEGER,cloud_observed_at_utc INTEGER,recorded_at_utc INTEGER NOT NULL,error_code TEXT NOT NULL DEFAULT '',UNIQUE(account_fingerprint,run_id))`,
		`CREATE INDEX IF NOT EXISTS queue_monitor_scan_runs_order ON queue_monitor_scan_runs(account_fingerprint,sequence DESC)`,
		`CREATE TABLE IF NOT EXISTS queue_monitor_scan_events(sequence INTEGER PRIMARY KEY AUTOINCREMENT,account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),run_id TEXT NOT NULL CHECK(length(run_id)=32),event_kind TEXT NOT NULL CHECK(event_kind IN ('started','cloud_listed','completed','failed','interrupted')),recorded_at_utc INTEGER NOT NULL,cloud_observed_at_utc INTEGER,error_code TEXT NOT NULL DEFAULT '')`,
		`CREATE INDEX IF NOT EXISTS queue_monitor_scan_events_order ON queue_monitor_scan_events(account_fingerprint,run_id,sequence DESC)`,
		`CREATE TRIGGER IF NOT EXISTS queue_monitor_scan_events_no_update BEFORE UPDATE ON queue_monitor_scan_events BEGIN SELECT RAISE(ABORT, 'queue monitor scan events are append-only'); END`,
		`CREATE TRIGGER IF NOT EXISTS queue_monitor_scan_events_no_delete BEFORE DELETE ON queue_monitor_scan_events BEGIN SELECT RAISE(ABORT, 'queue monitor scan events are append-only'); END`,
		`UPDATE store_meta SET value='34' WHERE key='schema_version' AND value='33'`,
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

// migrateV34 records fixed, non-sensitive reasons for scoped refresh boundary restarts.
func migrateV34(store *Store, scope string) error {
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
	if gotScope != scope || (version != "34" && version != "35") {
		return fmt.Errorf("SQLite store version or scope mismatch during v34 migration")
	}
	verify := func() error {
		var columns int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_events') WHERE name='restart_reason'`).Scan(&columns); err != nil {
			return err
		}
		if columns != 1 {
			return fmt.Errorf("SQLite cloud refresh restart evidence schema is incomplete")
		}
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v34 migration: %s", integrity)
		}
		body, _ := json.Marshal(manifest{Version: 35, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "35" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var columns int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_events') WHERE name='restart_reason'`).Scan(&columns); err != nil {
		_ = tx.Rollback()
		return err
	}
	statements := []string{}
	if columns == 0 {
		statements = append(statements, `ALTER TABLE cloud_inventory_refresh_events ADD COLUMN restart_reason TEXT NOT NULL DEFAULT '' CHECK(restart_reason IN ('','page_boundary_mismatch','provider_duplicate_page_id','staged_page_duplicate_id'))`)
	} else if columns != 1 {
		_ = tx.Rollback()
		return fmt.Errorf("invalid cloud refresh restart evidence schema")
	}
	statements = append(statements, `UPDATE store_meta SET value='35' WHERE key='schema_version' AND value='34'`)
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

// migrateV35 records fixed reasons for full inventory scan boundary restarts.
func migrateV35(store *Store, scope string) error {
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
	if gotScope != scope || (version != "35" && version != "36") {
		return fmt.Errorf("SQLite store version or scope mismatch during v35 migration")
	}
	verify := func() error {
		var columns int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_scan_events') WHERE name='restart_reason'`).Scan(&columns); err != nil {
			return err
		}
		if columns != 1 {
			return fmt.Errorf("SQLite cloud scan restart evidence schema is incomplete")
		}
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v35 migration: %s", integrity)
		}
		body, _ := json.Marshal(manifest{Version: 36, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "36" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var columns int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_scan_events') WHERE name='restart_reason'`).Scan(&columns); err != nil {
		_ = tx.Rollback()
		return err
	}
	statements := []string{}
	if columns == 0 {
		statements = append(statements, `ALTER TABLE cloud_inventory_scan_events ADD COLUMN restart_reason TEXT NOT NULL DEFAULT '' CHECK(restart_reason IN ('','page_boundary_mismatch','provider_duplicate_page_id','staged_page_duplicate_id'))`)
	} else if columns != 1 {
		_ = tx.Rollback()
		return fmt.Errorf("invalid cloud scan restart evidence schema")
	}
	statements = append(statements, `UPDATE store_meta SET value='36' WHERE key='schema_version' AND value='35'`)
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

// migrateV36 adds nullable completion-time coverage evidence. Existing scan
// runs deliberately remain NULL because their historical cache state cannot
// be reconstructed from today's mutable inventory.
func migrateV36(store *Store, scope string) error {
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
	if gotScope != scope || (version != "36" && version != "37") {
		return fmt.Errorf("SQLite store version or scope mismatch during v36 migration")
	}
	verify := func() error {
		var columns int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_scan_runs') WHERE name IN ('coverage_status','coverage_incomplete_directories','coverage_generation','coverage_observed_at_utc')`).Scan(&columns); err != nil {
			return err
		}
		if columns != 4 {
			return fmt.Errorf("SQLite cloud scan coverage history schema is incomplete")
		}
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v36 migration: %s", integrity)
		}
		body, _ := json.Marshal(manifest{Version: 37, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "37" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var columns int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_scan_runs') WHERE name IN ('coverage_status','coverage_incomplete_directories','coverage_generation','coverage_observed_at_utc')`).Scan(&columns); err != nil {
		_ = tx.Rollback()
		return err
	}
	if columns != 0 && columns != 4 {
		_ = tx.Rollback()
		return fmt.Errorf("invalid cloud scan coverage history schema")
	}
	if columns == 0 {
		for _, statement := range []string{
			`ALTER TABLE cloud_scan_runs ADD COLUMN coverage_status TEXT CHECK(coverage_status IS NULL OR coverage_status IN ('unknown','scanning','incomplete','complete'))`,
			`ALTER TABLE cloud_scan_runs ADD COLUMN coverage_incomplete_directories INTEGER CHECK(coverage_incomplete_directories IS NULL OR coverage_incomplete_directories>=0)`,
			`ALTER TABLE cloud_scan_runs ADD COLUMN coverage_generation INTEGER CHECK(coverage_generation IS NULL OR coverage_generation>=0)`,
			`ALTER TABLE cloud_scan_runs ADD COLUMN coverage_observed_at_utc INTEGER CHECK(coverage_observed_at_utc IS NULL OR coverage_observed_at_utc>0)`,
		} {
			if _, err = tx.ExecContext(ctx, statement); err != nil {
				_ = tx.Rollback()
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='37' WHERE key='schema_version' AND value='36'`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return verify()
}

// migrateV37 adds completion-time coverage reason evidence. Existing runs keep
// NULL because their past cache state cannot be reconstructed safely.
func migrateV37(store *Store, scope string) error {
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
	if gotScope != scope || (version != "37" && version != "38") {
		return fmt.Errorf("SQLite store version or scope mismatch during v37 migration")
	}
	verify := func() error {
		var columns int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_scan_runs') WHERE name='coverage_reasons_json'`).Scan(&columns); err != nil {
			return err
		}
		if columns != 1 {
			return fmt.Errorf("SQLite cloud scan coverage reason schema is incomplete")
		}
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v37 migration: %s", integrity)
		}
		body, _ := json.Marshal(manifest{Version: 38, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "38" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var columns int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_scan_runs') WHERE name='coverage_reasons_json'`).Scan(&columns); err != nil {
		_ = tx.Rollback()
		return err
	}
	if columns != 0 && columns != 1 {
		_ = tx.Rollback()
		return fmt.Errorf("invalid cloud scan coverage reason schema")
	}
	if columns == 0 {
		if _, err = tx.ExecContext(ctx, `ALTER TABLE cloud_scan_runs ADD COLUMN coverage_reasons_json TEXT CHECK(coverage_reasons_json IS NULL OR json_valid(coverage_reasons_json))`); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE store_meta SET value='38' WHERE key='schema_version' AND value='37'`); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return verify()
}

// migrateV38 adds indexes used by the global task-event history. The rows are
// immutable and remain untouched; manifest publication follows the SQLite commit.
func migrateV38(store *Store, scope string) error {
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
	if gotScope != scope || (version != "38" && version != "39" && version != "40" && version != "41") {
		return fmt.Errorf("SQLite store version or scope mismatch during v38 migration")
	}
	verify := func() error {
		var indexes int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='index' AND name IN ('job_events_kind_sequence','job_events_operation_sequence','job_events_attempt_sequence','job_events_recorded_sequence')`).Scan(&indexes); err != nil {
			return err
		}
		if indexes != 4 {
			return fmt.Errorf("SQLite global task-event indexes are incomplete")
		}
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v38 migration: %s", integrity)
		}
		body, _ := json.Marshal(manifest{Version: 39, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "39" || version == "40" || version == "41" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS job_events_kind_sequence ON job_events(event_type,sequence)`,
		`CREATE INDEX IF NOT EXISTS job_events_operation_sequence ON job_events(operation_id,sequence)`,
		`CREATE INDEX IF NOT EXISTS job_events_attempt_sequence ON job_events(attempt_id,sequence)`,
		`CREATE INDEX IF NOT EXISTS job_events_recorded_sequence ON job_events(recorded_at_utc,sequence)`,
		`UPDATE store_meta SET value='39' WHERE key='schema_version' AND value='38'`,
	} {
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

// migrateV39 adds optional details to new scoped-refresh drift events without
// changing the immutable history already written by schema v39.
func migrateV39(store *Store, scope string) error {
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
	if gotScope != scope || (version != "39" && version != "40" && version != "41") {
		return fmt.Errorf("SQLite store version or scope mismatch during v39 migration")
	}
	verify := func() error {
		var columns int
		if err = db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_events') WHERE name IN ('restart_offset','restart_fs_id')`).Scan(&columns); err != nil {
			return err
		}
		if columns != 2 {
			return fmt.Errorf("SQLite scoped refresh restart evidence columns are incomplete")
		}
		var integrity string
		if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
			return err
		}
		if integrity != "ok" {
			return fmt.Errorf("SQLite integrity check failed during v39 migration: %s", integrity)
		}
		body, _ := json.Marshal(manifest{Version: 40, Scope: scope})
		return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
	}
	if version == "40" || version == "41" {
		return verify()
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	var offsetExists, fsIDExists int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_events') WHERE name='restart_offset'`).Scan(&offsetExists); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_events') WHERE name='restart_fs_id'`).Scan(&fsIDExists); err != nil {
		_ = tx.Rollback()
		return err
	}
	statements := []string{}
	if offsetExists == 0 {
		statements = append(statements, `ALTER TABLE cloud_inventory_refresh_events ADD COLUMN restart_offset INTEGER CHECK(restart_offset IS NULL OR restart_offset>0)`)
	}
	if fsIDExists == 0 {
		statements = append(statements, `ALTER TABLE cloud_inventory_refresh_events ADD COLUMN restart_fs_id INTEGER CHECK(restart_fs_id IS NULL OR restart_fs_id>0)`)
	}
	statements = append(statements, `UPDATE store_meta SET value='40' WHERE key='schema_version' AND value='39'`)
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

// migrateV40 creates an empty persisted cooldown table; earlier failures have
// no reliable retry deadline, so the migration intentionally does not infer one.
func migrateV40(store *Store, scope string) error {
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
	if gotScope != scope || (version != "40" && version != "41") {
		return fmt.Errorf("SQLite store version or scope mismatch during v40 migration")
	}
	if version == "40" {
		tx, txErr := db.BeginTx(ctx, nil)
		if txErr != nil {
			return txErr
		}
		if _, txErr = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS queue_monitor_retry_state (account_fingerprint TEXT PRIMARY KEY CHECK(length(account_fingerprint)<=128),failure_count INTEGER NOT NULL CHECK(failure_count>0),error_code TEXT NOT NULL CHECK(length(error_code)<=64),retry_delay_millis INTEGER NOT NULL CHECK(retry_delay_millis>0),retry_at_utc INTEGER NOT NULL CHECK(retry_at_utc>0),updated_at_utc INTEGER NOT NULL CHECK(updated_at_utc>0))`); txErr == nil {
			_, txErr = tx.ExecContext(ctx, `UPDATE store_meta SET value='41' WHERE key='schema_version' AND value='40'`)
		}
		if txErr != nil {
			_ = tx.Rollback()
			return txErr
		}
		if txErr = tx.Commit(); txErr != nil {
			return txErr
		}
	}
	var count int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='queue_monitor_retry_state'`).Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("SQLite queue monitor retry state schema is incomplete")
	}
	var integrity string
	if err = db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("SQLite integrity check failed during v40 migration: %s", integrity)
	}
	body, _ := json.Marshal(manifest{Version: 41, Scope: scope})
	return atomicWrite(filepath.Join(store.dir, "manifest.json"), body)
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
