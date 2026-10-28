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

const sqliteSchemaVersion = 2

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
CREATE INDEX IF NOT EXISTS job_events_job_sequence ON job_events(job_id, sequence);
CREATE TRIGGER IF NOT EXISTS job_events_no_update BEFORE UPDATE ON job_events BEGIN SELECT RAISE(ABORT, 'job events are append-only'); END;
CREATE TRIGGER IF NOT EXISTS job_events_no_delete BEFORE DELETE ON job_events BEGIN SELECT RAISE(ABORT, 'job events are append-only'); END;
		`); err != nil {
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
	var schemaObjects int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE (type='table' AND name IN ('store_meta','jobs','job_events')) OR (type='index' AND name='job_events_job_sequence') OR (type='trigger' AND name IN ('job_events_no_update','job_events_no_delete'))`).Scan(&schemaObjects); err != nil {
		return err
	}
	if schemaObjects != 6 {
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
