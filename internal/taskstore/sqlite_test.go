package taskstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSQLiteStartupDiagnosticsDescribeValidatedQueueWithoutLeakingScope(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/private/queue-scope")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveWithEvent(41, []byte(`{"status":"pending"}`), Event{Kind: "job_registered"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveWithEvent(41, []byte(`{"status":"done"}`), Event{Kind: "job_completed"}); err != nil {
		t.Fatal(err)
	}

	diagnostic, err := store.StartupDiagnostics()
	if err != nil {
		t.Fatal(err)
	}
	if diagnostic.SchemaVersion != sqliteSchemaVersion || diagnostic.JobCount != 1 || diagnostic.EventCount != 2 || diagnostic.LatestEventSequence != 2 {
		t.Fatalf("wrong queue summary: %+v", diagnostic)
	}
	if diagnostic.ScopeFingerprint == "" || diagnostic.ScopeFingerprint == "/private/queue-scope" || strings.Contains(diagnostic.ScopeFingerprint, "/private") {
		t.Fatalf("scope was not summarized: %+v", diagnostic)
	}
	if diagnostic.IntegrityCheck != "ok" || diagnostic.JournalMode != "delete" || diagnostic.Synchronous != 3 || !diagnostic.ForeignKeys || diagnostic.BusyTimeoutMS != 5000 {
		t.Fatalf("wrong effective SQLite checks: %+v", diagnostic)
	}
	if diagnostic.MigrationAction != "created" || diagnostic.RunID == "" || diagnostic.RecordedAtUTC.IsZero() || diagnostic.DatabaseSizeBytes <= 0 {
		t.Fatalf("missing startup context: %+v", diagnostic)
	}
	if _, err := time.Parse(time.RFC3339Nano, diagnostic.RecordedAtUTC.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("startup time is not RFC3339 UTC: %v", err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER reject_event BEFORE INSERT ON job_events BEGIN SELECT RAISE(ABORT, 'diagnostics must remain read-only'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartupDiagnostics(); err != nil {
		t.Fatalf("read-only diagnostics failed: %v", err)
	}
}

func TestSQLiteStartupDiagnosticsDistinguishMigrationAndReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "migrated-queue")
	writeLegacyStore(t, dir, map[string]string{"7.json": `{"version":1,"file":{"fs_id":7,"path":"/queue/seven","server_filename":"seven"},"mode":"keep","status":"pending"}`})
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	diagnostic, err := store.StartupDiagnostics()
	if err != nil || diagnostic.MigrationAction != "migrated_v1" || diagnostic.JobCount != 1 || diagnostic.EventCount != 1 {
		store.Close()
		t.Fatalf("migration not captured: %+v err=%v", diagnostic, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	diagnostic, err = store.StartupDiagnostics()
	if err != nil || diagnostic.MigrationAction != "reopened" {
		t.Fatalf("reopen incorrectly reported migration: %+v err=%v", diagnostic, err)
	}
}

func TestSQLiteProjectionAndEventCommitAtomically(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.SaveWithEvent(1, []byte(`{"status":"pending"}`), Event{Kind: "job_registered", EvidenceType: "local_state"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(context.Background(), `CREATE TRIGGER reject_event BEFORE INSERT ON job_events BEGIN SELECT RAISE(ABORT, 'injected event failure'); END`); err != nil {
		t.Fatal(err)
	}
	err = store.SaveWithEvent(1, []byte(`{"status":"done"}`), Event{Kind: "job_completed", EvidenceType: "local_state"})
	if err == nil {
		t.Fatal("expected injected event insert failure")
	}
	var current []byte
	if err := store.db.QueryRowContext(context.Background(), `SELECT payload_json FROM jobs WHERE id=1`).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if string(current) != `{"status":"pending"}` {
		t.Fatalf("projection changed without its event: %s", current)
	}
	events, err := store.Events(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "job_registered" {
		t.Fatalf("unexpected events after rollback: %#v", events)
	}
}

func TestSQLiteEventTimesKeepLocalProviderAndFutureDistinct(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	providerTime := time.Date(2026, 10, 27, 12, 30, 0, 0, time.UTC)
	observedTime := time.Date(2026, 10, 28, 1, 0, 0, 0, time.UTC)
	responseTime := time.Date(2026, 10, 28, 1, 0, 1, 0, time.UTC)
	if err := store.SaveWithEvent(2, []byte(`{"status":"retry_wait","next_attempt":"2026-10-29T00:00:00Z"}`), Event{
		Kind: "retry_scheduled", EvidenceType: "provider_response", OccurredAtUTC: &providerTime, CloudObservedAtUTC: &observedTime, ResponseObservedAtUTC: &responseTime,
		Evidence: json.RawMessage(`{"response":"rate_limited"}`),
	}); err != nil {
		t.Fatal(err)
	}
	events, err := store.Events(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].OccurredAtUTC == nil || !events[0].OccurredAtUTC.Equal(providerTime) || events[0].CloudObservedAtUTC == nil || !events[0].CloudObservedAtUTC.Equal(observedTime) || events[0].ResponseObservedAtUTC == nil || !events[0].ResponseObservedAtUTC.Equal(responseTime) {
		t.Fatalf("provider event timestamps were not kept distinct: %#v", events)
	}
	if events[0].RecordedAtUTC.IsZero() || !events[0].RecordedAtUTC.After(observedTime) {
		t.Fatalf("local record time missing or not separate: %#v", events[0])
	}
	if events[0].OccurredAtUTC.Equal(events[0].RecordedAtUTC) || events[0].CloudObservedAtUTC.Equal(events[0].RecordedAtUTC) {
		t.Fatal("local record time was conflated with a provider timestamp")
	}
}

func TestOpenFailsClosedWhenAuthoritativeSQLiteSchemaIsDamaged(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "queue.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DROP TABLE job_events`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(dir, "/queue"); err == nil {
		reopened.Close()
		t.Fatal("recreated a missing authoritative event table")
	}
	db, err = sql.Open("sqlite", filepath.Join(dir, "queue.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='job_events'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("damaged schema was silently repaired: count=%d err=%v", count, err)
	}
}

func TestSQLiteJobEventsAreAppendOnly(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Save(1, []byte(`{"status":"pending"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`UPDATE job_events SET event_type='rewritten' WHERE job_id=1`); err == nil {
		t.Fatal("rewrote immutable job evidence")
	}
}
