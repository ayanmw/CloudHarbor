package taskstore

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQueueMonitorScanHistoryRecoveryAndAccountIsolation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 2, 1, 2, 3, 0, time.UTC)
	if err = s.StartQueueMonitorScan("acct-a", "00000000000000000000000000000001", "/queue", start); err != nil {
		t.Fatal(err)
	}
	cloud := start.Add(time.Second)
	if err = s.RecordQueueMonitorCloudListed("acct-a", "00000000000000000000000000000001", cloud); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.StartQueueMonitorScan("acct-a", "00000000000000000000000000000002", "/queue/new", start.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	runs, err := s.ListQueueMonitorScanRuns("acct-a", 0, QueueMonitorScanFilter{}, 10)
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
	if runs[1].Status != "interrupted" || runs[1].CloudObservedAtUTC == nil || !runs[1].CloudObservedAtUTC.Equal(cloud) {
		t.Fatalf("recovered run=%+v", runs[1])
	}
	other, err := s.ListQueueMonitorScanRuns("acct-b", 0, QueueMonitorScanFilter{}, 10)
	if err != nil || len(other) != 0 {
		t.Fatalf("cross account runs=%+v err=%v", other, err)
	}
	events, err := s.ListQueueMonitorScanEvents("acct-a", runs[1].RunID, 0, 10)
	if err != nil || len(events) != 3 || events[0].Kind != "interrupted" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	encoded, err := json.Marshal(runs[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) == "" || strings.Contains(string(encoded), "account_fingerprint") {
		t.Fatalf("account fingerprint leaked: %s", encoded)
	}
}

func TestQueueMonitorRetryStatePersistsByAccountAndClearsOnSuccess(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 11, 2, 4, 5, 6, 0, time.UTC)
	state, err := s.ScheduleQueueMonitorRetry("acct-a", "provider_error", 2, 45*time.Second, now)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(45 * time.Second); !state.RetryAtUTC.Equal(want) {
		t.Fatalf("retry_at=%s want %s", state.RetryAtUTC, want)
	}
	got, err := s.QueueMonitorRetryState("acct-a")
	if err != nil || got == nil || !got.RetryAtUTC.Equal(state.RetryAtUTC) || got.FailureCount != 2 {
		t.Fatalf("persisted state=%+v err=%v", got, err)
	}
	other, err := s.QueueMonitorRetryState("acct-b")
	if err != nil || other != nil {
		t.Fatalf("other account state=%+v err=%v", other, err)
	}
	if err = s.ClearQueueMonitorRetryState("acct-a"); err != nil {
		t.Fatal(err)
	}
	got, err = s.QueueMonitorRetryState("acct-a")
	if err != nil || got != nil {
		t.Fatalf("cleared state=%+v err=%v", got, err)
	}
}

func TestQueueMonitorScanFinishSummaryAndEventAreAtomic(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	id := "00000000000000000000000000000003"
	if err = s.StartQueueMonitorScan("acct", id, "/queue", now); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER reject_monitor_finish BEFORE INSERT ON queue_monitor_scan_events WHEN NEW.event_kind='completed' BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishQueueMonitorScan("acct", id, "success", "", now.Add(time.Second)); err == nil {
		t.Fatal("expected injected error")
	}
	runs, err := s.ListQueueMonitorScanRuns("acct", 0, QueueMonitorScanFilter{}, 10)
	if err != nil || len(runs) != 1 || runs[0].Status != "running" || runs[0].FinishedAtUTC != nil {
		t.Fatalf("run after rollback=%+v err=%v", runs, err)
	}
	events, err := s.ListQueueMonitorScanEvents("acct", id, 0, 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("events after rollback=%+v err=%v", events, err)
	}
	if _, err = s.ExportQueueMonitorScanEvidence(context.Background(), "other", id); err == nil {
		t.Fatal("cross-account export must not find run")
	}
}

func TestQueueMonitorScanStartSummaryAndEventAreAtomic(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.db.Exec(`CREATE TRIGGER reject_monitor_start BEFORE INSERT ON queue_monitor_scan_events WHEN NEW.event_kind='started' BEGIN SELECT RAISE(ABORT,'injected start failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = s.StartQueueMonitorScan("acct", "00000000000000000000000000000005", "/queue", time.Now().UTC()); err == nil {
		t.Fatal("expected injected error")
	}
	var runs, events int
	if err = s.db.QueryRow(`SELECT (SELECT count(*) FROM queue_monitor_scan_runs),(SELECT count(*) FROM queue_monitor_scan_events)`).Scan(&runs, &events); err != nil || runs != 0 || events != 0 {
		t.Fatalf("partial start run=%d events=%d err=%v", runs, events, err)
	}
}

func TestQueueMonitorScanDateFilterUTC(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 11, 2, 23, 0, 0, 0, time.UTC)
	if err = s.StartQueueMonitorScan("acct", "00000000000000000000000000000004", "/queue", now); err != nil {
		t.Fatal(err)
	}
	runs, err := s.ListQueueMonitorScanRuns("acct", 0, QueueMonitorScanFilter{StartedFromUTC: "2026-11-02", StartedThroughUTC: "2026-11-02"}, 10)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
	if _, err = s.ListQueueMonitorScanRuns("acct", 0, QueueMonitorScanFilter{StartedFromUTC: "2026-11-03", StartedThroughUTC: "2026-11-02"}, 10); err == nil {
		t.Fatal("expected invalid range")
	}
}

func TestQueueMonitorScanLatestRunsUseOneSnapshot(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start := time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC)
	firstID := "00000000000000000000000000000011"
	secondID := "00000000000000000000000000000012"
	if err = s.StartQueueMonitorScan("acct", firstID, "/queue", start); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishQueueMonitorScan("acct", firstID, "success", "", start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = s.StartQueueMonitorScan("acct", secondID, "/queue", start.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	latest, latestSuccess, err := s.LatestQueueMonitorScanRuns(context.Background(), "acct")
	if err != nil || latest == nil || latestSuccess == nil || latest.RunID != secondID || latest.Status != "running" || latestSuccess.RunID != firstID || latestSuccess.Status != "success" {
		t.Fatalf("latest=%+v latestSuccess=%+v err=%v", latest, latestSuccess, err)
	}
	if err = s.FinishQueueMonitorScan("acct", secondID, "success", "", start.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	latest, latestSuccess, err = s.LatestQueueMonitorScanRuns(context.Background(), "acct")
	if err != nil || latest == nil || latestSuccess == nil || latest.RunID != secondID || latest.Status != "success" || latestSuccess.RunID != latest.RunID {
		t.Fatalf("completed snapshot latest=%+v latestSuccess=%+v err=%v", latest, latestSuccess, err)
	}
}

func TestBoundAccountFingerprintSurvivesTokenChangesWithoutReturningAccountID(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.BoundAccountFingerprint(); err != ErrAccountNotBound {
		t.Fatalf("unbound account err=%v", err)
	}
	if err = s.BindAccount("3400000000000000123"); err != nil {
		t.Fatal(err)
	}
	first, err := s.BoundAccountFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.BoundAccountFingerprint()
	if err != nil || first != second || first == "3400000000000000123" {
		t.Fatalf("fingerprints first=%q second=%q err=%v", first, second, err)
	}
}
