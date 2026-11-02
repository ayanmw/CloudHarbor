package taskstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEventPageFilteredUsesRecordedUTCAndStableSequenceCursor(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	body := []byte(`{"status":"pending"}`)
	if err = s.Save(42, body); err != nil {
		t.Fatal(err)
	}
	kinds := []string{"download_attempt_started", "task_retry_requested", "download_attempt_started", "download_attempt_started"}
	times := []time.Time{
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 2, 23, 59, 59, 999999000, time.UTC),
		time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC),
	}
	for index, at := range times {
		if _, err = s.db.Exec(`INSERT INTO job_events(job_id,event_type,recorded_at_utc,run_id,evidence_type,evidence_json) VALUES(?,?,?,?,?,?)`, 42, kinds[index], at.UnixMicro(), "", "test", []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	filter := TaskEventFilter{Kind: "download_attempt_started", RecordedAtFromUTC: &from, RecordedAtBeforeUTC: &before}
	page, total, cursor, more, err := s.EventPageFiltered(context.Background(), 42, 0, 1, filter)
	if err != nil || total != 2 || len(page) != 1 || page[0].Sequence != 5 || cursor != 5 || !more {
		t.Fatalf("filtered page=%#v total=%d cursor=%d more=%v err=%v", page, total, cursor, more, err)
	}
	page, total, cursor, more, err = s.EventPageFiltered(context.Background(), 42, cursor, 1, filter)
	if err != nil || total != 2 || len(page) != 1 || page[0].Sequence != 2 || cursor != 0 || more {
		t.Fatalf("older filtered page=%#v total=%d cursor=%d more=%v err=%v", page, total, cursor, more, err)
	}
	if _, err = s.db.Exec(`INSERT INTO job_events(job_id,event_type,recorded_at_utc,run_id,evidence_type,evidence_json) VALUES(?,?,?,?,?,?)`, 42, "download_attempt_started", time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC).UnixMicro(), "", "test", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	page, _, _, _, err = s.EventPageFiltered(context.Background(), 42, 5, 10, filter)
	if err != nil || len(page) != 1 || page[0].Sequence != 2 {
		t.Fatalf("appended event shifted existing cursor page: %#v, err=%v", page, err)
	}
}

func TestTaskEventExactUTCFilterAndKindCountsShareTheFilteredSnapshot(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []int64{42, 43} {
		if err = s.Save(id, []byte(`{"status":"pending"}`)); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	before := start.Add(time.Second)
	for _, item := range []struct {
		job  int64
		kind string
		at   time.Time
	}{
		{42, "download_attempt_started", start},
		{42, "download_attempt_finished", start.Add(time.Second - time.Microsecond)},
		{42, "source_operation_unknown", before},
		{43, "download_attempt_started", start},
	} {
		if _, err = s.db.Exec(`INSERT INTO job_events(job_id,event_type,recorded_at_utc,run_id,evidence_type,evidence_json) VALUES(?,?,?,?,?,?)`, item.job, item.kind, item.at.UnixMicro(), "", "test", []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	filter := TaskEventFilter{OperationID: "", RecordedAtFromUTC: &start, RecordedAtBeforeUTC: &before}
	page, total, _, more, counts, err := s.EventPageFilteredWithCounts(context.Background(), 42, 0, 1, filter)
	if err != nil || total != 2 || len(page) != 1 || !more {
		t.Fatalf("page=%#v total=%d more=%v err=%v", page, total, more, err)
	}
	got := map[string]int64{}
	for _, count := range counts {
		got[count.Kind] = count.Count
	}
	if got["download_attempt_started"] != 1 || got["download_attempt_finished"] != 1 || got["source_operation_unknown"] != 0 || len(page) != 1 {
		t.Fatalf("kind counts under task and half-open UTC filters: %#v", got)
	}
}

func TestTaskEventContextIsBoundedTaskScopedAndSequenceOrdered(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []int64{42, 43} {
		if err = s.Save(id, []byte(`{"status":"pending"}`)); err != nil {
			t.Fatal(err)
		}
	}
	var anchor int64
	for i := 1; i <= 15; i++ {
		result, insertErr := s.db.Exec(`INSERT INTO job_events(job_id,event_type,recorded_at_utc,run_id,evidence_type,evidence_json) VALUES(?,?,?,?,?,?)`, 42, "task_retry_requested", time.Now().UTC().UnixMicro()+int64(i), "", "test", []byte(`{}`))
		if insertErr != nil {
			t.Fatal(insertErr)
		}
		if i == 8 {
			anchor, _ = result.LastInsertId()
		}
	}
	other, err := s.db.Exec(`INSERT INTO job_events(job_id,event_type,recorded_at_utc,run_id,evidence_type,evidence_json) VALUES(?,?,?,?,?,?)`, 43, "task_retry_requested", time.Now().UTC().UnixMicro(), "", "test", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	otherSequence, err := other.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	eventContext, err := s.TaskEventContext(context.Background(), 42, anchor)
	if err != nil {
		t.Fatal(err)
	}
	if eventContext.Anchor.Sequence != anchor || len(eventContext.Before) != 5 || len(eventContext.After) != 5 || eventContext.Before[0].Sequence != anchor-5 || eventContext.After[4].Sequence != anchor+5 {
		t.Fatalf("unexpected bounded context: before=%#v anchor=%#v after=%#v", eventContext.Before, eventContext.Anchor, eventContext.After)
	}
	if _, err = s.TaskEventContext(context.Background(), 42, otherSequence); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("other task sequence accepted: %v", err)
	}
	if _, err = s.TaskEventContext(context.Background(), 42, anchor+100); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing sequence accepted: %v", err)
	}
	var first, last int64
	if err = s.db.QueryRow(`SELECT min(sequence),max(sequence) FROM job_events WHERE job_id=?`, 42).Scan(&first, &last); err != nil { t.Fatal(err) }
	firstContext, err := s.TaskEventContext(context.Background(), 42, first)
	if err != nil || len(firstContext.Before) != 0 || firstContext.Anchor.Sequence != first { t.Fatalf("first-event context=%#v err=%v", firstContext, err) }
	lastContext, err := s.TaskEventContext(context.Background(), 42, last)
	if err != nil || len(lastContext.After) != 0 || lastContext.Anchor.Sequence != last { t.Fatalf("last-event context=%#v err=%v", lastContext, err) }
}

func TestTaskEventCorrelationFiltersAreExactTaskScopedAndStable(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	body := []byte(`{"status":"pending"}`)
	for _, id := range []int64{42, 43} {
		if err = s.Save(id, body); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		jobID     int64
		kind      string
		operation string
		attempt   string
	}{
		{jobID: 42, kind: "download_attempt_started", attempt: "attempt-a"},
		{jobID: 42, kind: "source_operation_sending", operation: "operation-x", attempt: "attempt-b"},
		{jobID: 42, kind: "source_operation_confirmed", operation: "operation-x", attempt: "attempt-b"},
		{jobID: 43, kind: "source_operation_confirmed", operation: "operation-x", attempt: "attempt-b"},
	} {
		if _, err = s.db.Exec(`INSERT INTO job_events(job_id,event_type,recorded_at_utc,run_id,operation_id,attempt_id,evidence_type,evidence_json) VALUES(?,?,?,?,?,?,?,?)`, item.jobID, item.kind, time.Now().UTC().UnixMicro(), "", item.operation, item.attempt, "test", []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	operationFilter := TaskEventFilter{OperationID: "operation-x"}
	page, total, cursor, more, err := s.EventPageFiltered(context.Background(), 42, 0, 1, operationFilter)
	if err != nil || total != 2 || len(page) != 1 || page[0].Kind != "source_operation_confirmed" || cursor != page[0].Sequence || !more {
		t.Fatalf("operation page=%#v total=%d cursor=%d more=%v err=%v", page, total, cursor, more, err)
	}
	page, total, cursor, more, err = s.EventPageFiltered(context.Background(), 42, cursor, 1, operationFilter)
	if err != nil || total != 2 || len(page) != 1 || page[0].Kind != "source_operation_sending" || cursor != 0 || more {
		t.Fatalf("older operation page=%#v total=%d cursor=%d more=%v err=%v", page, total, cursor, more, err)
	}
	attemptFilter := TaskEventFilter{AttemptID: "attempt-b"}
	if _, total, _, _, err = s.EventPageFiltered(context.Background(), 42, 0, 10, attemptFilter); err != nil || total != 2 {
		t.Fatalf("attempt matches=%d err=%v", total, err)
	}
	combined := TaskEventFilter{Kind: "source_operation_sending", OperationID: "operation-x", AttemptID: "attempt-b"}
	start := time.Now().UTC().Add(-time.Hour)
	end := time.Now().UTC().Add(time.Hour)
	combined.RecordedAtFromUTC, combined.RecordedAtBeforeUTC = &start, &end
	page, total, _, more, err = s.EventPageFiltered(context.Background(), 42, 0, 10, combined)
	if err != nil || total != 1 || len(page) != 1 || page[0].Kind != "source_operation_sending" || more {
		t.Fatalf("combined page=%#v total=%d more=%v err=%v", page, total, more, err)
	}
	missing := TaskEventFilter{OperationID: "operation-' OR 1=1 --"}
	page, total, _, _, err = s.EventPageFiltered(context.Background(), 42, 0, 10, missing)
	if err != nil || total != 0 || len(page) != 0 {
		t.Fatalf("unmatched exact ID page=%#v total=%d err=%v", page, total, err)
	}
}

func TestTaskEventCorrelationExportHas100ItemAllOrNothingLimit(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Save(42, []byte(`{"status":"pending"}`)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < TaskEventEvidenceExportLimit; i++ {
		if _, err = s.db.Exec(`INSERT INTO job_events(job_id,event_type,recorded_at_utc,run_id,operation_id,attempt_id,evidence_type,evidence_json) VALUES(?,?,?,?,?,?,?,?)`, 42, "source_operation_sending", time.Now().UTC().UnixMicro()+int64(i), "", "operation-export", "attempt-export", "test", []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	filter := TaskEventFilter{OperationID: "operation-export", AttemptID: "attempt-export"}
	events, total, err := s.TaskEventEvidenceExportFiltered(context.Background(), 42, filter)
	if err != nil || total != TaskEventEvidenceExportLimit || len(events) != TaskEventEvidenceExportLimit {
		t.Fatalf("100-item correlation export len=%d total=%d err=%v", len(events), total, err)
	}
	if _, err = s.db.Exec(`INSERT INTO job_events(job_id,event_type,recorded_at_utc,run_id,operation_id,attempt_id,evidence_type,evidence_json) VALUES(?,?,?,?,?,?,?,?)`, 42, "source_operation_confirmed", time.Now().UTC().UnixMicro(), "", "operation-export", "attempt-export", "test", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if events, total, err = s.TaskEventEvidenceExportFiltered(context.Background(), 42, filter); !errors.Is(err, ErrTaskEventEvidenceExportTooLarge) || total != TaskEventEvidenceExportLimit+1 || events != nil {
		t.Fatalf("101-item correlation export len=%d total=%d err=%v", len(events), total, err)
	}
}

func TestFilteredTaskEventExportIsScopedAndBounded(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	body := []byte(`{"status":"pending"}`)
	if err = s.Save(42, body); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []struct {
		name  string
		count int
	}{{name: "task_retry_requested", count: TaskEventEvidenceExportLimit + 1}, {name: "download_attempt_started", count: TaskEventEvidenceExportLimit}} {
		for i := 0; i < kind.count; i++ {
			if _, err = s.db.Exec(`INSERT INTO job_events(job_id,event_type,recorded_at_utc,run_id,evidence_type,evidence_json) VALUES(?,?,?,?,?,?)`, 42, kind.name, time.Date(2026, 11, 2, 12, 0, i%60, 0, time.UTC).UnixMicro()+int64(i/60), "", "test", []byte(`{}`)); err != nil {
				t.Fatal(err)
			}
		}
	}
	filter := TaskEventFilter{Kind: "download_attempt_started"}
	events, total, err := s.TaskEventEvidenceExportFiltered(context.Background(), 42, filter)
	if err != nil || total != TaskEventEvidenceExportLimit || len(events) != TaskEventEvidenceExportLimit {
		t.Fatalf("filtered export events=%d total=%d err=%v", len(events), total, err)
	}
	filter.Kind = "task_retry_requested"
	if _, total, err = s.TaskEventEvidenceExportFiltered(context.Background(), 42, filter); !errors.Is(err, ErrTaskEventEvidenceExportTooLarge) || total != 101 {
		t.Fatalf("oversized filtered export total=%d err=%v", total, err)
	}
	if _, _, err = s.TaskEventEvidenceExportFiltered(context.Background(), 43, filter); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing task error=%v, want os.ErrNotExist", err)
	}
}

func TestFilteredTaskEventQueriesValidateInputs(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tc := range []struct {
		name   string
		id     int64
		before int64
		limit  int
		filter TaskEventFilter
	}{{name: "negative id", id: -1, limit: 1}, {name: "negative cursor", id: 42, before: -1, limit: 1}, {name: "zero limit", id: 42, limit: 0}, {name: "unknown kind", id: 42, limit: 1, filter: TaskEventFilter{Kind: "made_up"}}, {name: "inverted time", id: 42, limit: 1, filter: TaskEventFilter{RecordedAtFromUTC: timePtr(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)), RecordedAtBeforeUTC: timePtr(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))}}} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, _, _, err := s.EventPageFiltered(context.Background(), tc.id, tc.before, tc.limit, tc.filter); err == nil {
				t.Fatal("invalid event query was accepted")
			}
		})
	}
}

func TestTaskEventFilterIncludesPersistedKinds(t *testing.T) {
	for _, kind := range []string{
		"authentication_wait_entered", "cloud_file_observed", "download_attempt_finished", "download_attempt_started",
		"job_completed", "job_registered", "job_state_saved", "legacy_import", "source_identity_changed",
		"source_operation_confirmed", "source_operation_retryable_failure", "source_operation_sending", "source_operation_unknown",
		"startup_recovery", "status_transition", "task_cancelled", "task_cloud_identity_updated", "task_paused",
		"task_recovered_after_scan", "task_resumed", "task_retry_requested", "task_source_deleted_remotely", "task_source_missing",
	} {
		if !IsTaskEventFilterKind(kind) {
			t.Errorf("persisted event kind %q is not filterable", kind)
		}
	}
}

func timePtr(value time.Time) *time.Time { return &value }
