package taskstore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGlobalTaskEventHistoryPinsPagesAndCountsToOneSequenceSnapshot(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, id := range []int64{42, 43} {
		if _, err = store.db.Exec(`INSERT INTO jobs(id,payload_json,status,updated_at_utc) VALUES(?,?,?,?)`, id, []byte(`{"status":"pending"}`), "pending", time.Now().UTC().UnixMicro()); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	before := start.Add(time.Second)
	for _, item := range []struct {
		jobID     int64
		kind      string
		at        time.Time
		operation string
		attempt   string
	}{
		{42, "download_attempt_started", start, "op-a", "attempt-a"},
		{43, "source_operation_unknown", start.Add(time.Microsecond), "op-a", ""},
		{42, "historical_future_kind", before.Add(-time.Microsecond), "", "attempt-a"},
		{43, "download_attempt_finished", before, "", ""},
	} {
		if _, err = store.db.Exec(`INSERT INTO job_events(job_id,event_type,recorded_at_utc,run_id,operation_id,attempt_id,evidence_type,evidence_json) VALUES(?,?,?,?,?,?,?,?)`, item.jobID, item.kind, item.at.UnixMicro(), "", item.operation, item.attempt, "test", []byte(`{"ok":true}`)); err != nil {
			t.Fatal(err)
		}
	}
	filter := GlobalTaskEventFilter{TaskEventFilter: TaskEventFilter{OperationID: "op-a", RecordedAtFromUTC: &start, RecordedAtBeforeUTC: &before}}
	page, err := store.TaskEventHistoryPage(context.Background(), filter, 0, nil, 1)
	if err != nil || page.TotalMatches != 2 || len(page.Events) != 1 || page.Events[0].JobID != 43 || page.AsOfSequence != 4 || !page.HasMore || page.NextBeforeSequence != 2 {
		t.Fatalf("initial page=%+v err=%v", page, err)
	}
	if _, err = store.db.Exec(`INSERT INTO job_events(job_id,event_type,recorded_at_utc,run_id,operation_id,evidence_type,evidence_json) VALUES(?,?,?,?,?,?,?)`, 42, "source_operation_confirmed", before.UnixMicro(), "", "op-a", "test", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	page, err = store.TaskEventHistoryPage(context.Background(), filter, 2, int64Pointer(4), 10)
	if err != nil || page.TotalMatches != 2 || page.AsOfSequence != 4 || len(page.Events) != 1 || page.Events[0].JobID != 42 || page.HasMore {
		t.Fatalf("pinned older page=%+v err=%v", page, err)
	}
	counts := map[string]int64{}
	for _, count := range page.KindCounts {
		counts[count.Kind] = count.Count
	}
	if counts["source_operation_unknown"] != 1 || counts["download_attempt_started"] != 1 || counts["other"] != 0 {
		t.Fatalf("kind counts changed across pages: %#v", counts)
	}
}

func TestGlobalTaskEventHistoryPinsEmptySnapshotAtZero(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	filter := GlobalTaskEventFilter{}
	page, err := store.TaskEventHistoryPage(context.Background(), filter, 0, nil, 20)
	if err != nil || page.AsOfSequence != 0 || len(page.Events) != 0 || page.TotalMatches != 0 {
		t.Fatalf("empty snapshot=%+v err=%v", page, err)
	}
	if _, err = store.db.Exec(`INSERT INTO jobs(id,payload_json,status,updated_at_utc) VALUES(?,?,?,?)`, 42, []byte(`{"status":"pending"}`), "pending", time.Now().UTC().UnixMicro()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`INSERT INTO job_events(job_id,event_type,recorded_at_utc,run_id,evidence_type,evidence_json) VALUES(?,?,?,?,?,?)`, 42, "download_attempt_started", time.Now().UTC().UnixMicro(), "", "test", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	pinnedZero := int64(0)
	page, err = store.TaskEventHistoryPage(context.Background(), filter, 0, &pinnedZero, 20)
	if err != nil || page.AsOfSequence != 0 || len(page.Events) != 0 || page.TotalMatches != 0 {
		t.Fatalf("pinned zero page=%+v err=%v", page, err)
	}
	events, total, asOf, err := store.TaskEventHistoryExport(context.Background(), filter, &pinnedZero)
	if err != nil || asOf != 0 || total != 0 || len(events) != 0 {
		t.Fatalf("pinned zero export events=%d total=%d as_of=%d err=%v", len(events), total, asOf, err)
	}
}

func TestGlobalTaskEventHistoryRejectsInvalidFiltersAndSnapshotCursors(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.db.Exec(`INSERT INTO jobs(id,payload_json,status,updated_at_utc) VALUES(?,?,?,?)`, 42, []byte(`{"status":"pending"}`), "pending", time.Now().UTC().UnixMicro()); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		name   string
		filter GlobalTaskEventFilter
		before int64
		asOf   *int64
	}{
		{name: "unknown event kind", filter: GlobalTaskEventFilter{TaskID: 42, TaskEventFilter: TaskEventFilter{Kind: "not-a-real-kind"}}},
		{name: "future task id", filter: GlobalTaskEventFilter{TaskID: -1}},
		{name: "inverted time", filter: GlobalTaskEventFilter{TaskEventFilter: TaskEventFilter{RecordedAtFromUTC: timePointer(time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)), RecordedAtBeforeUTC: timePointer(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC))}}},
		{name: "mixed date and exact time", filter: GlobalTaskEventFilter{TaskEventFilter: TaskEventFilter{RecordedAtFromUTC: timePointer(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)), RecordedExactFromUTC: timePointer(time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC))}}},
		{name: "cursor beyond snapshot", before: 3, asOf: int64Pointer(2)},
		{name: "snapshot beyond current sequence", asOf: int64Pointer(3)},
	} {
		t.Run(input.name, func(t *testing.T) {
			if _, err := store.TaskEventHistoryPage(context.Background(), input.filter, input.before, input.asOf, 20); err == nil {
				t.Fatal("invalid global task-event query unexpectedly succeeded")
			}
		})
	}
}

func TestGlobalTaskEventHistoryFilterQueriesUseAddedIndexes(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, query := range []struct {
		name, sql, index string
		value            any
	}{
		{"kind", `EXPLAIN QUERY PLAN SELECT sequence FROM job_events WHERE event_type=? ORDER BY sequence DESC LIMIT 20`, "job_events_kind_sequence", "task_retry_requested"},
		{"operation", `EXPLAIN QUERY PLAN SELECT sequence FROM job_events WHERE operation_id=? ORDER BY sequence DESC LIMIT 20`, "job_events_operation_sequence", "operation-1"},
		{"attempt", `EXPLAIN QUERY PLAN SELECT sequence FROM job_events WHERE attempt_id=? ORDER BY sequence DESC LIMIT 20`, "job_events_attempt_sequence", "attempt-1"},
		{"recorded UTC", `EXPLAIN QUERY PLAN SELECT sequence FROM job_events WHERE recorded_at_utc>=? AND recorded_at_utc<?`, "job_events_recorded_sequence", int64(1)},
	} {
		t.Run(query.name, func(t *testing.T) {
			var rows *sql.Rows
			var queryErr error
			if query.name != "recorded UTC" {
				rows, queryErr = store.db.Query(query.sql, query.value)
			} else {
				rows, queryErr = store.db.Query(query.sql, query.value, int64(2))
			}
			if queryErr != nil {
				t.Fatal(queryErr)
			}
			defer rows.Close()
			var details []string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if queryErr = rows.Scan(&id, &parent, &unused, &detail); queryErr != nil {
					t.Fatal(queryErr)
				}
				details = append(details, detail)
			}
			if queryErr = rows.Err(); queryErr != nil {
				t.Fatal(queryErr)
			}
			joined := strings.Join(details, "\n")
			if !strings.Contains(joined, query.index) {
				t.Fatalf("query plan does not use %s: %s", query.index, joined)
			}
		})
	}
}

func TestGlobalTaskEventHistoryExportIsAllOrNothingAtLimit(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.db.Exec(`INSERT INTO jobs(id,payload_json,status,updated_at_utc) VALUES(?,?,?,?)`, 42, []byte(`{"status":"pending"}`), "pending", time.Now().UTC().UnixMicro()); err != nil {
		t.Fatal(err)
	}
	stmt, err := store.db.Prepare(`INSERT INTO job_events(job_id,event_type,recorded_at_utc,run_id,evidence_type,evidence_json) VALUES(?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < TaskEventHistoryExportLimit; i++ {
		if _, err = stmt.Exec(42, "download_attempt_started", int64(i+1), "", "test", []byte(`{}`)); err != nil {
			stmt.Close()
			t.Fatal(err)
		}
	}
	if err = stmt.Close(); err != nil {
		t.Fatal(err)
	}
	filter := GlobalTaskEventFilter{TaskID: 42}
	evidence, total, asOf, err := store.TaskEventHistoryExport(context.Background(), filter, nil)
	if err != nil || total != TaskEventHistoryExportLimit || asOf != TaskEventHistoryExportLimit || len(evidence) != TaskEventHistoryExportLimit {
		t.Fatalf("export len=%d total=%d as_of=%d err=%v", len(evidence), total, asOf, err)
	}
	if _, err = store.db.Exec(`INSERT INTO job_events(job_id,event_type,recorded_at_utc,run_id,evidence_type,evidence_json) VALUES(?,?,?,?,?,?)`, 42, "download_attempt_finished", int64(TaskEventHistoryExportLimit+1), "", "test", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if events, total, newAsOf, exportErr := store.TaskEventHistoryExport(context.Background(), filter, nil); !errors.Is(exportErr, ErrTaskEventHistoryExportTooLarge) || total != TaskEventHistoryExportLimit+1 || newAsOf != TaskEventHistoryExportLimit+1 || events != nil {
		t.Fatalf("oversized export events=%d total=%d as_of=%d err=%v", len(events), total, newAsOf, exportErr)
	}
}

func timePointer(value time.Time) *time.Time { return &value }
