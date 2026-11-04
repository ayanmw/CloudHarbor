package taskstore

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteSchemaVersion = 42

type Event struct {
	Sequence              int64           `json:"sequence"`
	JobID                 int64           `json:"job_id"`
	Kind                  string          `json:"kind"`
	RecordedAtUTC         time.Time       `json:"recorded_at_utc"`
	OccurredAtUTC         *time.Time      `json:"occurred_at_utc,omitempty"`
	CloudObservedAtUTC    *time.Time      `json:"cloud_observed_at_utc,omitempty"`
	ResponseObservedAtUTC *time.Time      `json:"response_observed_at_utc,omitempty"`
	RunID                 string          `json:"run_id,omitempty"`
	OperationID           string          `json:"operation_id,omitempty"`
	AttemptID             string          `json:"attempt_id,omitempty"`
	EvidenceType          string          `json:"evidence_type"`
	Evidence              json.RawMessage `json:"evidence,omitempty"`
}

func newRunID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func openSQLite(path, scope string) (*sql.DB, error) {
	return openSQLiteMode(path, scope, true)
}

func openSQLiteExisting(path, scope string) (*sql.DB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	return openSQLiteMode(path, scope, false)
}

func openSQLiteMode(path, scope string, initialize bool) (*sql.DB, error) {
	db, err := sql.Open("sqlite", sqliteDataSourceName(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	fail := func(err error) (*sql.DB, error) {
		db.Close()
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if !initialize {
		if err = validateSQLiteSchemaAndScope(ctx, db, scope); err != nil {
			return fail(err)
		}
	}
	var journal string
	if err = db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journal); err != nil {
		return fail(err)
	}
	if journal != "delete" {
		return fail(fmt.Errorf("unexpected SQLite journal mode %q", journal))
	}
	for _, check := range []struct {
		query string
		want  int
	}{{"PRAGMA synchronous", 3}, {"PRAGMA foreign_keys", 1}, {"PRAGMA busy_timeout", 5000}} {
		var got int
		if err = db.QueryRowContext(ctx, check.query).Scan(&got); err != nil {
			return fail(err)
		}
		if got != check.want {
			return fail(fmt.Errorf("SQLite %s=%d, want %d", check.query, got, check.want))
		}
	}
	if initialize {
		if _, err = db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS store_meta (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS jobs (
	id INTEGER PRIMARY KEY CHECK(id > 0),
	payload_json BLOB NOT NULL,
	status TEXT NOT NULL,
	updated_at_utc INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS job_events (
	sequence INTEGER PRIMARY KEY AUTOINCREMENT,
	job_id INTEGER NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
	event_type TEXT NOT NULL,
	recorded_at_utc INTEGER NOT NULL,
	occurred_at_utc INTEGER,
	cloud_observed_at_utc INTEGER,
	response_observed_at_utc INTEGER,
	run_id TEXT NOT NULL,
	operation_id TEXT,
	attempt_id TEXT,
	evidence_type TEXT NOT NULL,
	evidence_json BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS admin_sessions (
	session_hash TEXT PRIMARY KEY CHECK(length(session_hash)=64),
	created_at_utc INTEGER NOT NULL,
	revoked_at_utc INTEGER
);
CREATE TABLE IF NOT EXISTS remote_operations (
	operation_id TEXT PRIMARY KEY CHECK(length(operation_id)=32),
	account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),
	operation_kind TEXT NOT NULL CHECK(length(operation_kind)<=32),
	object_id INTEGER NOT NULL CHECK(object_id>=0),
	source_path TEXT NOT NULL CHECK(length(source_path)<=4096),
	destination_path TEXT NOT NULL CHECK(length(destination_path)<=4096),
	status TEXT NOT NULL CHECK(status IN ('prepared','confirmed','unknown','failed')),
	created_at_utc INTEGER NOT NULL,
	response_observed_at_utc INTEGER,
	evidence_json BLOB NOT NULL CHECK(json_valid(evidence_json))
);
CREATE TABLE IF NOT EXISTS remote_operation_events (
	sequence INTEGER PRIMARY KEY AUTOINCREMENT,
	operation_id TEXT NOT NULL REFERENCES remote_operations(operation_id) ON DELETE RESTRICT,
	status TEXT NOT NULL CHECK(status IN ('prepared','confirmed','unknown','failed')),
	recorded_at_utc INTEGER NOT NULL,
	response_observed_at_utc INTEGER,
	evidence_json BLOB NOT NULL CHECK(json_valid(evidence_json))
);
CREATE TABLE IF NOT EXISTS cloud_inventory (
	account_fingerprint TEXT NOT NULL,
	fs_id INTEGER NOT NULL CHECK(fs_id>0),
	parent_path TEXT NOT NULL,
	path TEXT NOT NULL,
	name TEXT NOT NULL,
	is_dir INTEGER NOT NULL CHECK(is_dir IN (0,1)),
	size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),
	provider_mtime INTEGER NOT NULL,
	content_md5 TEXT NOT NULL,
	observed_at_utc INTEGER NOT NULL,
	last_seen_run_id TEXT NOT NULL,
	PRIMARY KEY(account_fingerprint,fs_id)
);
CREATE TABLE IF NOT EXISTS cloud_inventory_file_sizes (account_fingerprint TEXT NOT NULL,size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),file_count INTEGER NOT NULL CHECK(file_count>=0),PRIMARY KEY(account_fingerprint,size_bytes));
CREATE TABLE IF NOT EXISTS cloud_inventory_directories (
	account_fingerprint TEXT NOT NULL,
	path TEXT NOT NULL,
	status TEXT NOT NULL CHECK(status IN ('scanning','complete','stale','unknown','failed')),
	observed_at_utc INTEGER,
	last_run_id TEXT NOT NULL,
	PRIMARY KEY(account_fingerprint,path)
);
CREATE TABLE IF NOT EXISTS cloud_inventory_generations (
	account_fingerprint TEXT PRIMARY KEY CHECK(length(account_fingerprint)<=128),
	generation INTEGER NOT NULL CHECK(generation>=0),
	updated_at_utc INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS cloud_directory_size_cache (
	account_fingerprint TEXT NOT NULL,
	path TEXT NOT NULL,
	generation INTEGER NOT NULL CHECK(generation>=0),
	bytes INTEGER NOT NULL CHECK(bytes>=0),
	files INTEGER NOT NULL CHECK(files>=0),
	directories INTEGER NOT NULL CHECK(directories>=0),
	complete INTEGER NOT NULL CHECK(complete IN (0,1)),
	observed_at_utc INTEGER,
	cached_at_utc INTEGER NOT NULL,
	PRIMARY KEY(account_fingerprint,path),
	FOREIGN KEY(account_fingerprint) REFERENCES cloud_inventory_generations(account_fingerprint) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS cloud_inventory_scan_state (
	account_fingerprint TEXT PRIMARY KEY,
	run_id TEXT NOT NULL,
	status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),
	phase TEXT NOT NULL,
	pending_json BLOB NOT NULL CHECK(json_valid(pending_json)),
	current_directory TEXT NOT NULL,
	next_offset INTEGER NOT NULL CHECK(next_offset>=0),
	pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),
	items_processed INTEGER NOT NULL CHECK(items_processed>=0),
	pending_count INTEGER NOT NULL DEFAULT 0 CHECK(pending_count>=0),
	started_at_utc INTEGER NOT NULL,
	updated_at_utc INTEGER NOT NULL,
	last_error TEXT NOT NULL,
	retry_delay_millis INTEGER NOT NULL DEFAULT 0 CHECK(retry_delay_millis>=0 AND retry_delay_millis<=3600000),
	retry_backoff_base_millis INTEGER NOT NULL DEFAULT 0 CHECK(retry_backoff_base_millis>=0 AND retry_backoff_base_millis<=3600000),
	retry_at_utc INTEGER CHECK(retry_at_utc IS NULL OR retry_at_utc>0),
	next_page_boundary_json TEXT NOT NULL DEFAULT '' CHECK(length(next_page_boundary_json)<=8192 AND (next_page_boundary_json='' OR json_valid(next_page_boundary_json)))
	,request_budget INTEGER NOT NULL DEFAULT 0 CHECK(request_budget>=0)
	,requests_used INTEGER NOT NULL DEFAULT 0 CHECK(requests_used>=0 AND (request_budget=0 OR requests_used<=request_budget))
);
CREATE TABLE IF NOT EXISTS cloud_inventory_scan_queue (
	account_fingerprint TEXT NOT NULL,
	run_id TEXT NOT NULL,
	sequence INTEGER NOT NULL CHECK(sequence>=0),
	path TEXT NOT NULL,
	PRIMARY KEY(account_fingerprint,run_id,sequence),
	UNIQUE(account_fingerprint,run_id,path)
);
CREATE TABLE IF NOT EXISTS cloud_scan_runs (
	sequence INTEGER PRIMARY KEY AUTOINCREMENT,
	account_fingerprint TEXT NOT NULL,
	run_id TEXT NOT NULL,
	status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),
	phase TEXT NOT NULL,
	current_directory TEXT NOT NULL,
	pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),
	items_processed INTEGER NOT NULL CHECK(items_processed>=0),
	request_budget INTEGER NOT NULL CHECK(request_budget>=0),
	requests_used INTEGER NOT NULL CHECK(requests_used>=0),
	pending_count INTEGER NOT NULL CHECK(pending_count>=0),
	started_at_utc INTEGER NOT NULL,
	updated_at_utc INTEGER NOT NULL,
	last_error TEXT NOT NULL,
	coverage_status TEXT CHECK(coverage_status IS NULL OR coverage_status IN ('unknown','scanning','incomplete','complete')),
	coverage_incomplete_directories INTEGER CHECK(coverage_incomplete_directories IS NULL OR coverage_incomplete_directories>=0),
	coverage_generation INTEGER CHECK(coverage_generation IS NULL OR coverage_generation>=0),
	coverage_observed_at_utc INTEGER CHECK(coverage_observed_at_utc IS NULL OR coverage_observed_at_utc>0),
	coverage_reasons_json TEXT CHECK(coverage_reasons_json IS NULL OR json_valid(coverage_reasons_json)),
	UNIQUE(account_fingerprint,run_id)
);
CREATE INDEX IF NOT EXISTS cloud_scan_runs_order ON cloud_scan_runs(account_fingerprint,sequence DESC);
CREATE TABLE IF NOT EXISTS queue_monitor_scan_runs (
	sequence INTEGER PRIMARY KEY AUTOINCREMENT,
	account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),
	run_id TEXT NOT NULL CHECK(length(run_id)=32),
	status TEXT NOT NULL CHECK(status IN ('running','success','failed','interrupted')),
	monitor_path TEXT NOT NULL CHECK(length(monitor_path)<=4096),
	started_at_utc INTEGER NOT NULL,
	finished_at_utc INTEGER,
	cloud_observed_at_utc INTEGER,
	recorded_at_utc INTEGER NOT NULL,
	error_code TEXT NOT NULL DEFAULT '',
	UNIQUE(account_fingerprint,run_id)
);
CREATE INDEX IF NOT EXISTS queue_monitor_scan_runs_order ON queue_monitor_scan_runs(account_fingerprint,sequence DESC);
CREATE TABLE IF NOT EXISTS queue_monitor_scan_events (
	sequence INTEGER PRIMARY KEY AUTOINCREMENT,
	account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),
	run_id TEXT NOT NULL CHECK(length(run_id)=32),
	event_kind TEXT NOT NULL CHECK(event_kind IN ('started','cloud_listed','completed','failed','interrupted')),
	recorded_at_utc INTEGER NOT NULL,
	cloud_observed_at_utc INTEGER,
	error_code TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS queue_monitor_scan_events_order ON queue_monitor_scan_events(account_fingerprint,run_id,sequence DESC);
CREATE TRIGGER IF NOT EXISTS queue_monitor_scan_events_no_update BEFORE UPDATE ON queue_monitor_scan_events BEGIN SELECT RAISE(ABORT, 'queue monitor scan events are append-only'); END;
CREATE TRIGGER IF NOT EXISTS queue_monitor_scan_events_no_delete BEFORE DELETE ON queue_monitor_scan_events BEGIN SELECT RAISE(ABORT, 'queue monitor scan events are append-only'); END;
CREATE TABLE IF NOT EXISTS queue_monitor_retry_state (
	account_fingerprint TEXT PRIMARY KEY CHECK(length(account_fingerprint)<=128),
	failure_count INTEGER NOT NULL CHECK(failure_count>0),
	error_code TEXT NOT NULL CHECK(length(error_code)<=64),
	retry_delay_millis INTEGER NOT NULL CHECK(retry_delay_millis>0),
	retry_at_utc INTEGER NOT NULL CHECK(retry_at_utc>0),
	updated_at_utc INTEGER NOT NULL CHECK(updated_at_utc>0)
);
CREATE TABLE IF NOT EXISTS cloud_inventory_scan_events (
	sequence INTEGER PRIMARY KEY AUTOINCREMENT,
	account_fingerprint TEXT NOT NULL,
	run_id TEXT NOT NULL,
	event_kind TEXT NOT NULL CHECK(event_kind IN ('started','resumed','paused','request_budget_reached','retry_scheduled','boundary_restart','failed','completed')),
	status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),
	phase TEXT NOT NULL,
	current_directory TEXT NOT NULL,
	pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),
	items_processed INTEGER NOT NULL CHECK(items_processed>=0),
	request_budget INTEGER NOT NULL CHECK(request_budget>=0),
	requests_used INTEGER NOT NULL CHECK(requests_used>=0),
	pending_count INTEGER NOT NULL CHECK(pending_count>=0),
	recorded_at_utc INTEGER NOT NULL,
	last_error TEXT NOT NULL,
	restart_reason TEXT NOT NULL DEFAULT '' CHECK(restart_reason IN ('','page_boundary_mismatch','provider_duplicate_page_id','staged_page_duplicate_id'))
);
CREATE INDEX IF NOT EXISTS cloud_inventory_scan_events_order ON cloud_inventory_scan_events(account_fingerprint,run_id,sequence DESC);
CREATE INDEX IF NOT EXISTS cloud_inventory_scan_boundary_restart_count ON cloud_inventory_scan_events(account_fingerprint,run_id,current_directory) WHERE event_kind='boundary_restart';
CREATE TRIGGER IF NOT EXISTS cloud_inventory_scan_events_no_update BEFORE UPDATE ON cloud_inventory_scan_events BEGIN SELECT RAISE(ABORT, 'cloud scan events are append-only'); END;
CREATE TRIGGER IF NOT EXISTS cloud_inventory_scan_events_no_delete BEFORE DELETE ON cloud_inventory_scan_events BEGIN SELECT RAISE(ABORT, 'cloud scan events are append-only'); END;
CREATE TABLE IF NOT EXISTS cloud_inventory_refresh_state (
	account_fingerprint TEXT PRIMARY KEY CHECK(length(account_fingerprint)<=128),
	run_id TEXT NOT NULL CHECK(length(run_id)<=128),
	root_path TEXT NOT NULL CHECK(length(root_path)<=4096),
	status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),
	phase TEXT NOT NULL,
	current_directory TEXT NOT NULL CHECK(length(current_directory)<=4096),
	next_offset INTEGER NOT NULL CHECK(next_offset>=0),
	pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),
	items_processed INTEGER NOT NULL CHECK(items_processed>=0),
	request_budget INTEGER NOT NULL DEFAULT 0 CHECK(request_budget>=0),
	requests_used INTEGER NOT NULL DEFAULT 0 CHECK(requests_used>=0),
	pending_count INTEGER NOT NULL CHECK(pending_count>=0),
	started_at_utc INTEGER NOT NULL,
	updated_at_utc INTEGER NOT NULL,
	last_error TEXT NOT NULL,
	retry_delay_millis INTEGER NOT NULL DEFAULT 0 CHECK(retry_delay_millis>=0 AND retry_delay_millis<=3600000),
	retry_backoff_base_millis INTEGER NOT NULL DEFAULT 0 CHECK(retry_backoff_base_millis>=0 AND retry_backoff_base_millis<=3600000),
	retry_at_utc INTEGER CHECK(retry_at_utc IS NULL OR retry_at_utc>0),
	next_page_boundary_json TEXT NOT NULL DEFAULT '' CHECK(length(next_page_boundary_json)<=8192 AND (next_page_boundary_json='' OR json_valid(next_page_boundary_json)))
);
CREATE TABLE IF NOT EXISTS cloud_inventory_refresh_runs (
	start_sequence INTEGER PRIMARY KEY,
	account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),
	run_id TEXT NOT NULL CHECK(length(run_id)<=128),
	root_path TEXT NOT NULL CHECK(length(root_path)<=4096),
	status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),
	phase TEXT NOT NULL,
	current_directory TEXT NOT NULL CHECK(length(current_directory)<=4096),
	pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),
	items_processed INTEGER NOT NULL CHECK(items_processed>=0),
	request_budget INTEGER NOT NULL CHECK(request_budget>=0),
	requests_used INTEGER NOT NULL CHECK(requests_used>=0),
	pending_count INTEGER NOT NULL CHECK(pending_count>=0),
	started_at_utc INTEGER NOT NULL,
	updated_at_utc INTEGER NOT NULL,
	last_error TEXT NOT NULL,
	latest_sequence INTEGER NOT NULL CHECK(latest_sequence>=start_sequence),
	latest_kind TEXT NOT NULL,
	UNIQUE(account_fingerprint,run_id)
);
CREATE INDEX IF NOT EXISTS cloud_inventory_refresh_run_order ON cloud_inventory_refresh_runs(account_fingerprint,start_sequence DESC);
CREATE TABLE IF NOT EXISTS cloud_inventory_refresh_queue (
	account_fingerprint TEXT NOT NULL,
	run_id TEXT NOT NULL,
	sequence INTEGER NOT NULL CHECK(sequence>=0),
	path TEXT NOT NULL CHECK(length(path)<=4096),
	PRIMARY KEY(account_fingerprint,run_id,sequence),
	UNIQUE(account_fingerprint,run_id,path)
);
CREATE TABLE IF NOT EXISTS cloud_inventory_refresh_entries (
	account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),
	run_id TEXT NOT NULL CHECK(length(run_id)<=128),
	directory TEXT NOT NULL CHECK(length(directory)<=4096),
	fs_id INTEGER NOT NULL CHECK(fs_id>0),
	path TEXT NOT NULL CHECK(length(path)<=4096),
	name TEXT NOT NULL CHECK(length(name)<=1024),
	is_dir INTEGER NOT NULL CHECK(is_dir IN (0,1)),
	size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),
	provider_mtime INTEGER NOT NULL,
	content_md5 TEXT NOT NULL,
	observed_at_utc INTEGER NOT NULL DEFAULT 0 CHECK(observed_at_utc>=0),
	PRIMARY KEY(account_fingerprint,run_id,directory,fs_id)
);
CREATE INDEX IF NOT EXISTS cloud_inventory_refresh_entries_order ON cloud_inventory_refresh_entries(account_fingerprint,run_id,directory,path,fs_id);
CREATE TABLE IF NOT EXISTS cloud_inventory_refresh_events (sequence INTEGER PRIMARY KEY AUTOINCREMENT,account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),run_id TEXT NOT NULL CHECK(length(run_id)<=128),event_kind TEXT NOT NULL CHECK(event_kind IN ('started','resumed','paused','request_budget_reached','retry_scheduled','boundary_restart','failed','completed','directory_published')),status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,current_directory TEXT NOT NULL CHECK(length(current_directory)<=4096),pages_processed INTEGER NOT NULL CHECK(pages_processed>=0),items_processed INTEGER NOT NULL CHECK(items_processed>=0),request_budget INTEGER NOT NULL DEFAULT 0 CHECK(request_budget>=0),requests_used INTEGER NOT NULL DEFAULT 0 CHECK(requests_used>=0),request_budget_added INTEGER NOT NULL DEFAULT 0 CHECK(request_budget_added>=0),retry_at_utc INTEGER,pending_count INTEGER NOT NULL CHECK(pending_count>=0),recorded_at_utc INTEGER NOT NULL,cloud_observed_at_utc INTEGER,directory_changed INTEGER CHECK(directory_changed IN (0,1)),added_count INTEGER CHECK(added_count>=0),removed_count INTEGER CHECK(removed_count>=0),changed_count INTEGER CHECK(changed_count>=0),last_error TEXT NOT NULL DEFAULT '',restart_reason TEXT NOT NULL DEFAULT '' CHECK(restart_reason IN ('','page_boundary_mismatch','provider_duplicate_page_id','staged_page_duplicate_id')),restart_offset INTEGER CHECK(restart_offset IS NULL OR restart_offset>0),restart_fs_id INTEGER CHECK(restart_fs_id IS NULL OR restart_fs_id>0));
CREATE INDEX IF NOT EXISTS cloud_inventory_refresh_events_order ON cloud_inventory_refresh_events(account_fingerprint,run_id,sequence DESC);
CREATE INDEX IF NOT EXISTS cloud_inventory_refresh_runs_order ON cloud_inventory_refresh_events(account_fingerprint,event_kind,sequence DESC);
CREATE INDEX IF NOT EXISTS cloud_inventory_refresh_boundary_restart_count ON cloud_inventory_refresh_events(account_fingerprint,run_id,current_directory) WHERE event_kind='boundary_restart';
CREATE TRIGGER IF NOT EXISTS cloud_inventory_refresh_events_no_update BEFORE UPDATE ON cloud_inventory_refresh_events BEGIN SELECT RAISE(ABORT, 'cloud inventory refresh events are append-only'); END;
CREATE TRIGGER IF NOT EXISTS cloud_inventory_refresh_events_no_delete BEFORE DELETE ON cloud_inventory_refresh_events BEGIN SELECT RAISE(ABORT, 'cloud inventory refresh events are append-only'); END;
CREATE TABLE IF NOT EXISTS cloud_inventory_refresh_changes (change_sequence INTEGER PRIMARY KEY AUTOINCREMENT,account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),run_id TEXT NOT NULL CHECK(length(run_id)<=128),event_sequence INTEGER NOT NULL CHECK(event_sequence>0),ordinal INTEGER NOT NULL CHECK(ordinal>0),change_kind TEXT NOT NULL CHECK(change_kind IN ('added','removed','metadata_changed','identity_changed')),old_snapshot TEXT CHECK(old_snapshot IS NULL OR length(old_snapshot)<=8192),new_snapshot TEXT CHECK(new_snapshot IS NULL OR length(new_snapshot)<=8192),recorded_at_utc INTEGER NOT NULL,cloud_observed_at_utc INTEGER NOT NULL,CHECK(old_snapshot IS NOT NULL OR new_snapshot IS NOT NULL),UNIQUE(account_fingerprint,run_id,event_sequence,ordinal));
CREATE INDEX IF NOT EXISTS cloud_inventory_refresh_changes_order ON cloud_inventory_refresh_changes(account_fingerprint,run_id,event_sequence,change_sequence DESC);
CREATE TRIGGER IF NOT EXISTS cloud_inventory_refresh_changes_no_update BEFORE UPDATE ON cloud_inventory_refresh_changes BEGIN SELECT RAISE(ABORT, 'cloud inventory refresh changes are append-only'); END;
CREATE TRIGGER IF NOT EXISTS cloud_inventory_refresh_changes_no_delete BEFORE DELETE ON cloud_inventory_refresh_changes BEGIN SELECT RAISE(ABORT, 'cloud inventory refresh changes are append-only'); END;
CREATE TABLE IF NOT EXISTS cloud_content_hashes (account_fingerprint TEXT NOT NULL,fs_id INTEGER NOT NULL,path TEXT NOT NULL,size_bytes INTEGER NOT NULL,provider_mtime INTEGER NOT NULL,sha256 TEXT NOT NULL CHECK(length(sha256)=64),verified_at_utc INTEGER NOT NULL,PRIMARY KEY(account_fingerprint,fs_id));
CREATE INDEX IF NOT EXISTS cloud_content_hashes_duplicate_group ON cloud_content_hashes(account_fingerprint,sha256,size_bytes,fs_id);
CREATE TABLE IF NOT EXISTS cloud_duplicate_scan_state (account_fingerprint TEXT PRIMARY KEY,run_id TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,next_fs_id INTEGER NOT NULL CHECK(next_fs_id>=0),total_files INTEGER NOT NULL CHECK(total_files>=0),processed_files INTEGER NOT NULL CHECK(processed_files>=0),total_bytes INTEGER NOT NULL DEFAULT 0 CHECK(total_bytes>=0),processed_bytes INTEGER NOT NULL DEFAULT 0 CHECK(processed_bytes>=0),started_at_utc INTEGER NOT NULL,updated_at_utc INTEGER NOT NULL,last_error TEXT NOT NULL,retry_delay_ms INTEGER NOT NULL DEFAULT 0 CHECK(retry_delay_ms>=0),retry_backoff_base_millis INTEGER NOT NULL DEFAULT 0 CHECK(retry_backoff_base_millis>=0 AND retry_backoff_base_millis<=3600000),retry_at_utc INTEGER,current_fs_id INTEGER NOT NULL DEFAULT 0 CHECK(current_fs_id>=0),current_path TEXT NOT NULL DEFAULT '',current_name TEXT NOT NULL DEFAULT '',current_size INTEGER NOT NULL DEFAULT 0 CHECK(current_size>=0),current_provider_mtime INTEGER NOT NULL DEFAULT 0,current_content_md5 TEXT NOT NULL DEFAULT '',current_bytes INTEGER NOT NULL DEFAULT 0 CHECK(current_bytes>=0),current_sha256_state BLOB NOT NULL DEFAULT X'' CHECK(length(current_sha256_state)<=1024));
CREATE TABLE IF NOT EXISTS cloud_duplicate_scan_events (sequence INTEGER PRIMARY KEY AUTOINCREMENT,account_fingerprint TEXT NOT NULL,run_id TEXT NOT NULL,status TEXT NOT NULL,phase TEXT NOT NULL,recorded_at_utc INTEGER NOT NULL,evidence_json BLOB NOT NULL CHECK(json_valid(evidence_json)));
CREATE TABLE IF NOT EXISTS cloud_duplicate_scan_runs (start_sequence INTEGER NOT NULL,account_fingerprint TEXT NOT NULL CHECK(length(account_fingerprint)<=128),run_id TEXT NOT NULL CHECK(length(run_id)<=128),status TEXT NOT NULL CHECK(status IN ('running','paused','complete','failed')),phase TEXT NOT NULL,next_fs_id INTEGER NOT NULL CHECK(next_fs_id>=0),total_files INTEGER NOT NULL CHECK(total_files>=0),processed_files INTEGER NOT NULL CHECK(processed_files>=0),total_bytes INTEGER NOT NULL CHECK(total_bytes>=0),processed_bytes INTEGER NOT NULL CHECK(processed_bytes>=0),started_at_utc INTEGER NOT NULL,updated_at_utc INTEGER NOT NULL,last_error TEXT NOT NULL,latest_sequence INTEGER NOT NULL CHECK(latest_sequence>=0),UNIQUE(account_fingerprint,run_id));
CREATE TABLE IF NOT EXISTS local_directory_migrations (
	migration_id TEXT PRIMARY KEY CHECK(length(migration_id)=32),
	source_path TEXT NOT NULL CHECK(length(source_path)<=4096),
	target_path TEXT NOT NULL CHECK(length(target_path)<=4096),
	desired_queue_path TEXT NOT NULL CHECK(length(desired_queue_path)<=4096),
	tombstone_path TEXT NOT NULL CHECK(length(tombstone_path)<=8192),
	status TEXT NOT NULL CHECK(status IN ('running','failed','completed')),
	phase TEXT NOT NULL CHECK(phase IN ('copying','verifying','retiring_source','source_retired','deleting_source','complete')),
	total_files INTEGER NOT NULL CHECK(total_files>=0),
	total_bytes INTEGER NOT NULL CHECK(total_bytes>=0),
	processed_files INTEGER NOT NULL CHECK(processed_files>=0),
	processed_bytes INTEGER NOT NULL CHECK(processed_bytes>=0),
	started_at_utc INTEGER NOT NULL,
	updated_at_utc INTEGER NOT NULL,
	finished_at_utc INTEGER,
	last_error TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS local_directory_migration_events (
	sequence INTEGER PRIMARY KEY AUTOINCREMENT,
	migration_id TEXT NOT NULL REFERENCES local_directory_migrations(migration_id) ON DELETE RESTRICT,
	status TEXT NOT NULL CHECK(status IN ('running','failed','completed')),
	phase TEXT NOT NULL,
	recorded_at_utc INTEGER NOT NULL,
	evidence_json BLOB NOT NULL CHECK(json_valid(evidence_json))
);
CREATE INDEX IF NOT EXISTS remote_operation_events_order ON remote_operation_events(operation_id, sequence);
CREATE INDEX IF NOT EXISTS cloud_inventory_parent_name_id ON cloud_inventory(account_fingerprint,parent_path,name,fs_id);
CREATE INDEX IF NOT EXISTS cloud_inventory_path ON cloud_inventory(account_fingerprint,path);
CREATE INDEX IF NOT EXISTS cloud_inventory_duplicate_size ON cloud_inventory(account_fingerprint,is_dir,size_bytes,fs_id);
CREATE INDEX IF NOT EXISTS cloud_duplicate_scan_events_order ON cloud_duplicate_scan_events(account_fingerprint,sequence);
CREATE INDEX IF NOT EXISTS cloud_duplicate_scan_runs_order ON cloud_duplicate_scan_runs(account_fingerprint,start_sequence DESC,run_id);
CREATE INDEX IF NOT EXISTS local_directory_migration_events_order ON local_directory_migration_events(migration_id, sequence);
CREATE TRIGGER IF NOT EXISTS remote_operation_events_no_update BEFORE UPDATE ON remote_operation_events BEGIN SELECT RAISE(ABORT, 'remote operation events are append-only'); END;
CREATE TRIGGER IF NOT EXISTS remote_operation_events_no_delete BEFORE DELETE ON remote_operation_events BEGIN SELECT RAISE(ABORT, 'remote operation events are append-only'); END;
CREATE TRIGGER IF NOT EXISTS local_directory_migration_events_no_update BEFORE UPDATE ON local_directory_migration_events BEGIN SELECT RAISE(ABORT, 'local migration events are append-only'); END;
CREATE TRIGGER IF NOT EXISTS local_directory_migration_events_no_delete BEFORE DELETE ON local_directory_migration_events BEGIN SELECT RAISE(ABORT, 'local migration events are append-only'); END;
CREATE TRIGGER IF NOT EXISTS cloud_duplicate_scan_events_no_update BEFORE UPDATE ON cloud_duplicate_scan_events BEGIN SELECT RAISE(ABORT, 'duplicate scan events are append-only'); END;
CREATE TRIGGER IF NOT EXISTS cloud_duplicate_scan_events_no_delete BEFORE DELETE ON cloud_duplicate_scan_events BEGIN SELECT RAISE(ABORT, 'duplicate scan events are append-only'); END;
CREATE TRIGGER IF NOT EXISTS cloud_inventory_file_size_insert AFTER INSERT ON cloud_inventory WHEN NEW.is_dir=0 BEGIN INSERT INTO cloud_inventory_file_sizes(account_fingerprint,size_bytes,file_count) VALUES(NEW.account_fingerprint,NEW.size_bytes,1) ON CONFLICT(account_fingerprint,size_bytes) DO UPDATE SET file_count=file_count+1; END;
CREATE TRIGGER IF NOT EXISTS cloud_inventory_file_size_delete AFTER DELETE ON cloud_inventory WHEN OLD.is_dir=0 BEGIN UPDATE cloud_inventory_file_sizes SET file_count=file_count-1 WHERE account_fingerprint=OLD.account_fingerprint AND size_bytes=OLD.size_bytes; DELETE FROM cloud_inventory_file_sizes WHERE account_fingerprint=OLD.account_fingerprint AND size_bytes=OLD.size_bytes AND file_count<=0; END;
CREATE TRIGGER IF NOT EXISTS cloud_inventory_file_size_update AFTER UPDATE OF account_fingerprint,is_dir,size_bytes ON cloud_inventory WHEN OLD.account_fingerprint<>NEW.account_fingerprint OR OLD.is_dir<>NEW.is_dir OR OLD.size_bytes<>NEW.size_bytes BEGIN UPDATE cloud_inventory_file_sizes SET file_count=file_count-1 WHERE account_fingerprint=OLD.account_fingerprint AND size_bytes=OLD.size_bytes AND OLD.is_dir=0; DELETE FROM cloud_inventory_file_sizes WHERE account_fingerprint=OLD.account_fingerprint AND size_bytes=OLD.size_bytes AND file_count<=0; INSERT INTO cloud_inventory_file_sizes(account_fingerprint,size_bytes,file_count) SELECT NEW.account_fingerprint,NEW.size_bytes,1 WHERE NEW.is_dir=0 ON CONFLICT(account_fingerprint,size_bytes) DO UPDATE SET file_count=file_count+1; END;
CREATE INDEX IF NOT EXISTS job_events_job_sequence ON job_events(job_id, sequence);
CREATE INDEX IF NOT EXISTS job_events_kind_sequence ON job_events(event_type, sequence);
CREATE INDEX IF NOT EXISTS job_events_operation_sequence ON job_events(operation_id, sequence);
CREATE INDEX IF NOT EXISTS job_events_attempt_sequence ON job_events(attempt_id, sequence);
CREATE INDEX IF NOT EXISTS job_events_recorded_sequence ON job_events(recorded_at_utc, sequence);
CREATE TRIGGER IF NOT EXISTS job_events_no_update BEFORE UPDATE ON job_events BEGIN SELECT RAISE(ABORT, 'job events are append-only'); END;
CREATE TRIGGER IF NOT EXISTS job_events_no_delete BEFORE DELETE ON job_events BEGIN SELECT RAISE(ABORT, 'job events are append-only'); END;
		`); err != nil {
			return fail(err)
		}
		if _, err = db.ExecContext(ctx, downloadManifestSchema); err != nil {
			return fail(err)
		}
		if _, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO store_meta(key, value) VALUES('schema_version', ?), ('scope', ?)`, fmt.Sprint(sqliteSchemaVersion), scope); err != nil {
			return fail(err)
		}
		if err = validateSQLiteSchemaAndScope(ctx, db, scope); err != nil {
			return fail(err)
		}
	}
	var integrity string
	if err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return fail(err)
	}
	if integrity != "ok" {
		return fail(fmt.Errorf("SQLite integrity check failed: %s", integrity))
	}
	if err = os.Chmod(path, 0600); err != nil {
		return fail(err)
	}
	return db, nil
}

func sqliteDataSourceName(path string) string {
	query := url.Values{}
	for _, pragma := range []string{
		"busy_timeout(5000)",
		"foreign_keys(1)",
		"journal_mode(DELETE)",
		"synchronous(3)",
	} {
		query.Add("_pragma", pragma)
	}
	// modernc applies these pragmas whenever database/sql opens a physical
	// connection, including after it discards a connection with an I/O error.
	return path + "?" + query.Encode()
}

func validateSQLiteSchemaAndScope(ctx context.Context, db *sql.DB, scope string) error {
	if err := validateDownloadManifestSchema(ctx, db); err != nil {
		return err
	}
	var schemaObjects int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE (type='table' AND name IN ('store_meta','jobs','job_events','admin_sessions','remote_operations','remote_operation_events','cloud_inventory','cloud_inventory_file_sizes','cloud_inventory_directories','cloud_inventory_generations','cloud_directory_size_cache','cloud_inventory_scan_state','cloud_inventory_scan_queue','cloud_inventory_scan_events','cloud_scan_runs','queue_monitor_scan_runs','queue_monitor_scan_events','cloud_inventory_refresh_state','cloud_inventory_refresh_runs','cloud_inventory_refresh_queue','cloud_inventory_refresh_entries','cloud_inventory_refresh_events','cloud_inventory_refresh_changes','cloud_duplicate_scan_runs','local_directory_migrations','local_directory_migration_events')) OR (type='index' AND name IN ('job_events_job_sequence','job_events_kind_sequence','job_events_operation_sequence','job_events_attempt_sequence','job_events_recorded_sequence','remote_operation_events_order','cloud_inventory_parent_name_id','cloud_inventory_path','cloud_inventory_duplicate_size','cloud_content_hashes_duplicate_group','cloud_inventory_refresh_entries_order','cloud_inventory_scan_events_order','cloud_inventory_scan_boundary_restart_count','cloud_inventory_refresh_events_order','cloud_inventory_refresh_runs_order','cloud_inventory_refresh_run_order','cloud_inventory_refresh_boundary_restart_count','cloud_inventory_refresh_changes_order','local_directory_migration_events_order','cloud_scan_runs_order','queue_monitor_scan_runs_order','queue_monitor_scan_events_order','cloud_duplicate_scan_runs_order')) OR (type='trigger' AND name IN ('job_events_no_update','job_events_no_delete','remote_operation_events_no_update','remote_operation_events_no_delete','cloud_inventory_scan_events_no_update','cloud_inventory_scan_events_no_delete','queue_monitor_scan_events_no_update','queue_monitor_scan_events_no_delete','cloud_inventory_refresh_events_no_update','cloud_inventory_refresh_events_no_delete','cloud_inventory_refresh_changes_no_update','cloud_inventory_refresh_changes_no_delete','local_directory_migration_events_no_update','local_directory_migration_events_no_delete','cloud_inventory_file_size_insert','cloud_inventory_file_size_delete','cloud_inventory_file_size_update'))`).Scan(&schemaObjects); err != nil {
		return err
	}
	if schemaObjects != 66 {
		return fmt.Errorf("SQLite schema is incomplete")
	}
	var version, gotScope string
	if err := db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT value FROM store_meta WHERE key='scope'`).Scan(&gotScope); err != nil {
		return err
	}
	if version != fmt.Sprint(sqliteSchemaVersion) || gotScope != scope {
		return fmt.Errorf("SQLite store version or scope mismatch")
	}
	var pendingCountColumn int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_scan_state') WHERE name='pending_count'`).Scan(&pendingCountColumn); err != nil {
		return err
	}
	if pendingCountColumn != 1 {
		return fmt.Errorf("SQLite cloud scan state schema is incomplete")
	}
	var boundaryColumns int
	if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM pragma_table_info('cloud_inventory_scan_state') WHERE name='next_page_boundary_json') + (SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_state') WHERE name='next_page_boundary_json')`).Scan(&boundaryColumns); err != nil {
		return err
	}
	if boundaryColumns != 2 {
		return fmt.Errorf("SQLite cloud page boundary schema is incomplete")
	}
	var scanCoverageColumns int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_scan_runs') WHERE name IN ('coverage_status','coverage_incomplete_directories','coverage_generation','coverage_observed_at_utc')`).Scan(&scanCoverageColumns); err != nil {
		return err
	}
	if scanCoverageColumns != 4 {
		return fmt.Errorf("SQLite cloud scan coverage history schema is incomplete")
	}
	var scanCoverageReasonsColumn int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_scan_runs') WHERE name='coverage_reasons_json'`).Scan(&scanCoverageReasonsColumn); err != nil {
		return err
	}
	if scanCoverageReasonsColumn != 1 {
		return fmt.Errorf("SQLite cloud scan coverage reason schema is incomplete")
	}
	var refreshDeltaColumns int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_events') WHERE name IN ('added_count','removed_count','changed_count')`).Scan(&refreshDeltaColumns); err != nil {
		return err
	}
	if refreshDeltaColumns != 3 {
		return fmt.Errorf("SQLite cloud refresh delta schema is incomplete")
	}
	var refreshRunColumns int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_runs') WHERE name IN ('start_sequence','root_path','request_budget','requests_used','latest_sequence','latest_kind')`).Scan(&refreshRunColumns); err != nil {
		return err
	}
	if refreshRunColumns != 6 {
		return fmt.Errorf("SQLite cloud refresh run summary schema is incomplete")
	}
	var refreshAuditColumns int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_events') WHERE name IN ('request_budget_added','retry_at_utc','last_error')`).Scan(&refreshAuditColumns); err != nil {
		return err
	}
	if refreshAuditColumns != 3 {
		return fmt.Errorf("SQLite cloud refresh audit event schema is incomplete")
	}
	var restartReasonColumns int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_events') WHERE name='restart_reason'`).Scan(&restartReasonColumns); err != nil {
		return err
	}
	if restartReasonColumns != 1 {
		return fmt.Errorf("SQLite cloud refresh restart evidence schema is incomplete")
	}
	var refreshRestartDetailColumns int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_events') WHERE name IN ('restart_offset','restart_fs_id')`).Scan(&refreshRestartDetailColumns); err != nil {
		return err
	}
	if refreshRestartDetailColumns != 2 {
		return fmt.Errorf("SQLite cloud refresh restart detail schema is incomplete")
	}
	var scanRestartReasonColumns int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_scan_events') WHERE name='restart_reason'`).Scan(&scanRestartReasonColumns); err != nil {
		return err
	}
	if scanRestartReasonColumns != 1 {
		return fmt.Errorf("SQLite cloud scan restart evidence schema is incomplete")
	}
	var fullRetryColumns, refreshRetryColumns, duplicateRetryColumns int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_scan_state') WHERE name IN ('retry_delay_millis','retry_at_utc')`).Scan(&fullRetryColumns); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_inventory_refresh_state') WHERE name IN ('retry_delay_millis','retry_at_utc')`).Scan(&refreshRetryColumns); err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('cloud_duplicate_scan_state') WHERE name IN ('retry_delay_ms','retry_at_utc')`).Scan(&duplicateRetryColumns); err != nil {
		return err
	}
	var backoffColumns int
	for _, table := range []string{"cloud_inventory_scan_state", "cloud_inventory_refresh_state", "cloud_duplicate_scan_state"} {
		var found int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info(?) WHERE name='retry_backoff_base_millis'`, table).Scan(&found); err != nil {
			return err
		}
		backoffColumns += found
	}
	if fullRetryColumns != 2 || refreshRetryColumns != 2 || duplicateRetryColumns != 2 || backoffColumns != 3 {
		return fmt.Errorf("SQLite cloud retry state schema is incomplete")
	}
	return nil
}

func eventDefaults(event Event, id int64) (Event, error) {
	if event.Kind == "" {
		event.Kind = "job_state_saved"
	}
	if event.EvidenceType == "" {
		event.EvidenceType = "local_state"
	}
	if len(event.Evidence) == 0 {
		event.Evidence = json.RawMessage(`{}`)
	}
	if len(event.Kind) > 128 || len(event.EvidenceType) > 128 || len(event.OperationID) > 128 || len(event.AttemptID) > 128 || len(event.Evidence) > 64*1024 || !json.Valid(event.Evidence) {
		return Event{}, fmt.Errorf("invalid event evidence")
	}
	event.JobID = id
	return event, nil
}

func saveJobTx(ctx context.Context, tx *sql.Tx, runID string, id int64, body []byte, event Event) error {
	var oldStatus string
	err := tx.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id=?`, id).Scan(&oldStatus)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var record struct {
		Status  string    `json:"status"`
		Updated time.Time `json:"updated"`
	}
	if err := json.Unmarshal(body, &record); err != nil {
		return fmt.Errorf("invalid job JSON: %w", err)
	}
	if record.Updated.IsZero() {
		record.Updated = time.Now().UTC()
	}
	if event.Kind == "" {
		if errors.Is(err, sql.ErrNoRows) {
			event.Kind = "job_registered"
		} else if oldStatus != record.Status {
			event.Kind = "status_transition"
		} else {
			event.Kind = "job_state_saved"
		}
	}
	if len(event.Evidence) == 0 {
		from := oldStatus
		if errors.Is(err, sql.ErrNoRows) {
			from = ""
		}
		event.Evidence, _ = json.Marshal(struct {
			From string `json:"from_status,omitempty"`
			To   string `json:"to_status"`
		}{From: from, To: record.Status})
	}
	event, err = eventDefaults(event, id)
	if err != nil {
		return err
	}
	if event.RunID == "" {
		event.RunID = runID
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO jobs(id,payload_json,status,updated_at_utc) VALUES(?,?,?,?)
ON CONFLICT(id) DO UPDATE SET payload_json=excluded.payload_json,status=excluded.status,updated_at_utc=excluded.updated_at_utc`,
		id, body, record.Status, record.Updated.UTC().UnixMicro()); err != nil {
		return err
	}
	var occurred, observed, responseObserved any
	if event.OccurredAtUTC != nil {
		occurred = event.OccurredAtUTC.UTC().UnixMicro()
	}
	if event.CloudObservedAtUTC != nil {
		observed = event.CloudObservedAtUTC.UTC().UnixMicro()
	}
	if event.ResponseObservedAtUTC != nil {
		responseObserved = event.ResponseObservedAtUTC.UTC().UnixMicro()
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO job_events(job_id,event_type,recorded_at_utc,occurred_at_utc,cloud_observed_at_utc,response_observed_at_utc,run_id,operation_id,attempt_id,evidence_type,evidence_json)
VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, event.Kind, time.Now().UTC().UnixMicro(), occurred, observed, responseObserved, event.RunID, nullableString(event.OperationID), nullableString(event.AttemptID), event.EvidenceType, []byte(event.Evidence))
	return err
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func eventFromRow(rows *sql.Rows) (Event, error) {
	var event Event
	var recorded int64
	var occurred, observed, responseObserved sql.NullInt64
	var operation, attempt sql.NullString
	var evidence []byte
	if err := rows.Scan(&event.Sequence, &event.JobID, &event.Kind, &recorded, &occurred, &observed, &responseObserved, &event.RunID, &operation, &attempt, &event.EvidenceType, &evidence); err != nil {
		return Event{}, err
	}
	event.RecordedAtUTC = time.UnixMicro(recorded).UTC()
	if occurred.Valid {
		value := time.UnixMicro(occurred.Int64).UTC()
		event.OccurredAtUTC = &value
	}
	if observed.Valid {
		value := time.UnixMicro(observed.Int64).UTC()
		event.CloudObservedAtUTC = &value
	}
	if responseObserved.Valid {
		value := time.UnixMicro(responseObserved.Int64).UTC()
		event.ResponseObservedAtUTC = &value
	}
	event.OperationID, event.AttemptID = operation.String, attempt.String
	event.Evidence = append(json.RawMessage(nil), evidence...)
	return event, nil
}

const eventColumns = `sequence,job_id,event_type,recorded_at_utc,occurred_at_utc,cloud_observed_at_utc,response_observed_at_utc,run_id,operation_id,attempt_id,evidence_type,evidence_json`
