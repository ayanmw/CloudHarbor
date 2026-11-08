package taskstore

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCleanupBatchCreationIsIdempotentAndStopSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	m := cleanupManifestFixture()
	ctx := context.Background()
	first, err := s.CreateCleanupBatch(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	m.CreatedAtUTC = m.CreatedAtUTC.Add(time.Hour)
	again, err := s.CreateCleanupBatch(ctx, m)
	if err != nil || !again.CreatedAtUTC.Equal(first.CreatedAtUTC) {
		t.Fatalf("retry changed first clock: %+v %v", again, err)
	}
	m.Items[0].Expected.Mtime++
	if _, err = s.CreateCleanupBatch(ctx, m); !errors.Is(err, ErrCleanupBatchConflict) {
		t.Fatalf("changed manifest accepted: %v", err)
	}
	if err = s.StopCleanupBatch(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.StopCleanupBatch(ctx, m.ID); err != nil {
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
	got, err := s.CleanupBatch(ctx, m.ID)
	if err != nil || !got.StopRequested || got.Status != "stopped" || got.ItemCount != 1 || got.ManifestSHA256 != first.ManifestSHA256 {
		t.Fatalf("restart lost batch: %+v %v", got, err)
	}
	var events int
	if err = s.db.QueryRow(`SELECT count(*) FROM cleanup_batch_events WHERE batch_id=?`, m.ID).Scan(&events); err != nil || events != 2 {
		t.Fatalf("duplicate events: %d %v", events, err)
	}
}
func TestCleanupBatchCreationRollsBackAllRows(t *testing.T) {
	s, err := Open(t.TempDir(), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.db.Exec(`CREATE TRIGGER fail_cleanup_item BEFORE INSERT ON cleanup_batch_items BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateCleanupBatch(context.Background(), cleanupManifestFixture()); err == nil {
		t.Fatal("failure ignored")
	}
	for _, table := range []string{"cleanup_batches", "cleanup_batch_items", "cleanup_batch_events"} {
		var n int
		if err = s.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("partial transaction %s: %d %v", table, n, err)
		}
	}
}

func TestCleanupBatchEvidenceCannotBeRewritten(t *testing.T) {
	s, err := Open(t.TempDir(), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := cleanupManifestFixture()
	if _, err = s.CreateCleanupBatch(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`UPDATE cleanup_batches SET manifest_json='{}'`,
		`UPDATE cleanup_batches SET created_at_utc='2027-01-01T00:00:00Z'`,
		`UPDATE cleanup_batch_events SET kind='confirmed'`,
		`DELETE FROM cleanup_batch_events`,
		`UPDATE cleanup_batch_items SET operation_id='ffffffffffffffffffffffffffffffff'`,
	} {
		if _, err = s.db.Exec(query); err == nil {
			t.Fatalf("invalid evidence update accepted: %s", query)
		}
	}
}

func TestCleanupOperationPreparedAndLinkedAtomically(t *testing.T) {
	for _, mode := range []string{"success", "stopped", "wrong_owner", "wrong_source", "projection_failure"} {
		t.Run(mode, func(t *testing.T) {
			s, err := Open(t.TempDir(), "/queue")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ctx := context.Background()
			m := cleanupManifestFixture()
			if _, err = s.CreateCleanupBatch(ctx, m); err != nil {
				t.Fatal(err)
			}
			scope := CleanupBatchScope{m.AccountFingerprint, m.ApplicationFingerprint, m.CacheNamespace}
			op := RemoteOperation{ID: strings.Repeat("f", 32), AccountFingerprint: "sending-token-fingerprint", Kind: "delete", ObjectID: m.Items[0].Expected.FsID, SourcePath: m.Items[0].Expected.Path, Status: "prepared", CreatedAtUTC: time.Now().UTC(), Evidence: json.RawMessage(`{"verified":true}`)}
			switch mode {
			case "stopped":
				if err = s.StopCleanupBatch(ctx, m.ID); err != nil {
					t.Fatal(err)
				}
			case "wrong_owner":
				scope.AccountFingerprint = strings.Repeat("0", 64)
			case "wrong_source":
				op.SourcePath = "/another.bin"
			case "projection_failure":
				if _, err = s.db.Exec(`CREATE TRIGGER fail_cleanup_link BEFORE UPDATE ON cleanup_batch_items BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
					t.Fatal(err)
				}
			}
			err = s.PrepareCleanupOperation(ctx, m.ID, 0, scope, op)
			var count int
			if queryErr := s.db.QueryRow(`SELECT count(*) FROM remote_operations`).Scan(&count); queryErr != nil {
				t.Fatal(queryErr)
			}
			if mode != "success" {
				if err == nil || count != 0 {
					t.Fatalf("unsafe or partial prepare count=%d err=%v", count, err)
				}
				return
			}
			if err != nil || count != 1 {
				t.Fatalf("prepare count=%d err=%v", count, err)
			}
			var status, operation string
			if err = s.db.QueryRow(`SELECT status,operation_id FROM cleanup_batch_items`).Scan(&status, &operation); err != nil || status != "sending" || operation != op.ID {
				t.Fatalf("link %s %s %v", status, operation, err)
			}
			if err = s.PrepareCleanupOperation(ctx, m.ID, 0, scope, op); err == nil {
				t.Fatal("sending item admitted twice")
			}
		})
	}
}
