package taskstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestTaskEventEvidenceExportIsCompleteBoundedSnapshot(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.db.Exec(`INSERT INTO jobs(id,payload_json,status,updated_at_utc) VALUES(42, ?, 'pending', 0)`, []byte(`{"status":"pending"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.TaskEventEvidenceExport(context.Background(), 42); err != nil {
		t.Fatalf("empty task history should export: %v", err)
	}
	for i := 1; i <= TaskEventEvidenceExportLimit; i++ {
		if err = s.SaveWithEvent(42, []byte(`{"status":"pending"}`), Event{Kind: "event-" + strconv.Itoa(i)}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := s.TaskEventEvidenceExport(context.Background(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != TaskEventEvidenceExportLimit {
		t.Fatalf("100 event export returned %d events, want %d", len(events), TaskEventEvidenceExportLimit)
	}
	if events[0].Kind != "event-1" || events[len(events)-1].Kind != "event-100" {
		t.Fatalf("events are not in ascending sequence order: first=%q last=%q", events[0].Kind, events[len(events)-1].Kind)
	}
	if err = s.SaveWithEvent(42, []byte(`{"status":"pending"}`), Event{Kind: "event-101"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.TaskEventEvidenceExport(context.Background(), 42); !errors.Is(err, ErrTaskEventEvidenceExportTooLarge) {
		t.Fatalf("101 events error = %v, want all-or-nothing limit error", err)
	}
	if _, err = s.TaskEventEvidenceExport(context.Background(), 43); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing task error = %v, want os.ErrNotExist", err)
	}
}
