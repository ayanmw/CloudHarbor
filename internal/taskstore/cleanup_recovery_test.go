package taskstore

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCleanupRecoveryUsesDurableOutcomeAndNeverReplays(t *testing.T) {
	for _, outcome := range []string{"prepared", "unknown", "failed", "confirmed_unreconciled", "confirmed"} {
		t.Run(outcome, func(t *testing.T) {
			dir := t.TempDir()
			s, err := Open(dir, "/queue")
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			m := cleanupManifestFixture()
			if _, err = s.CreateCleanupBatch(ctx, m); err != nil {
				t.Fatal(err)
			}
			scope := CleanupBatchScope{m.AccountFingerprint, m.ApplicationFingerprint, m.CacheNamespace}
			op := RemoteOperation{ID: strings.Repeat("f", 32), AccountFingerprint: strings.Repeat("1", 64), Kind: "delete", ObjectID: m.Items[0].Expected.FsID, SourcePath: m.Items[0].Expected.Path, Status: "prepared", CreatedAtUTC: time.Now().UTC(), Evidence: json.RawMessage(`{}`)}
			if err = s.PrepareCleanupOperation(ctx, m.ID, 0, scope, op); err != nil {
				t.Fatal(err)
			}
			observed := time.Now().UTC()
			if outcome != "prepared" {
				state := outcome
				if state == "confirmed_unreconciled" {
					state = "confirmed"
				}
				if err = s.UpdateRemoteOperation(op.ID, state, json.RawMessage(`{"receipt":"test"}`), observed); err != nil {
					t.Fatal(err)
				}
			}
			if outcome == "confirmed" {
				if err = s.MarkRemoteOperationLocalReconciliationComplete(op.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = Open(dir, "/queue")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			wantItem, wantBatch := outcome, outcome
			switch outcome {
			case "prepared":
				wantItem, wantBatch = "unknown", "unknown"
			case "confirmed_unreconciled":
				wantItem, wantBatch = "confirmed", "failed"
			case "confirmed":
				wantBatch = "completed"
			}
			got, err := s.CleanupBatch(ctx, m.ID)
			if err != nil || got.Status != wantBatch {
				t.Fatalf("batch %+v expected %s err=%v", got, wantBatch, err)
			}
			var state string
			if err = s.db.QueryRow(`SELECT status FROM cleanup_batch_items WHERE batch_id=?`, m.ID).Scan(&state); err != nil || state != wantItem {
				t.Fatalf("item %s expected %s err=%v", state, wantItem, err)
			}
			if err = s.PrepareCleanupOperation(ctx, m.ID, 0, scope, op); err == nil {
				t.Fatal("replayed recovered operation")
			}
			var events int
			if err = s.db.QueryRow(`SELECT count(*) FROM cleanup_batch_events WHERE batch_id=?`, m.ID).Scan(&events); err != nil {
				t.Fatal(err)
			}
			if err = s.ReconcileCleanupOperation(ctx, m.ID, 0); err != nil {
				t.Fatal(err)
			}
			var again int
			if err = s.db.QueryRow(`SELECT count(*) FROM cleanup_batch_events WHERE batch_id=?`, m.ID).Scan(&again); err != nil || again != events {
				t.Fatalf("duplicate projection event %d/%d %v", again, events, err)
			}
		})
	}
}

func TestCleanupResultProjectionStopsFutureItemsAndRollsBack(t *testing.T) {
	for _, mode := range []string{"continue", "stop_inflight", "event_failure"} {
		t.Run(mode, func(t *testing.T) {
			s, err := Open(t.TempDir(), "/queue")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			ctx := context.Background()
			m := cleanupManifestFixture()
			next := m.Items[0]
			next.Expected.FsID++
			next.Expected.Path = "/second.bin"
			next.Expected.Name = "second.bin"
			m.Items = append(m.Items, next)
			if _, err = s.CreateCleanupBatch(ctx, m); err != nil {
				t.Fatal(err)
			}
			scope := CleanupBatchScope{m.AccountFingerprint, m.ApplicationFingerprint, m.CacheNamespace}
			op := RemoteOperation{ID: strings.Repeat("f", 32), AccountFingerprint: strings.Repeat("1", 64), Kind: "delete", ObjectID: m.Items[0].Expected.FsID, SourcePath: m.Items[0].Expected.Path, Status: "prepared", CreatedAtUTC: time.Now().UTC(), Evidence: json.RawMessage(`{}`)}
			if err = s.PrepareCleanupOperation(ctx, m.ID, 0, scope, op); err != nil {
				t.Fatal(err)
			}
			if mode == "stop_inflight" {
				if err = s.StopCleanupBatch(ctx, m.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err = s.UpdateRemoteOperation(op.ID, "confirmed", json.RawMessage(`{"receipt":"test"}`), time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			if err = s.MarkRemoteOperationLocalReconciliationComplete(op.ID); err != nil {
				t.Fatal(err)
			}
			if mode == "event_failure" {
				if _, err = s.db.Exec(`CREATE TRIGGER fail_cleanup_result BEFORE INSERT ON cleanup_batch_events WHEN NEW.kind='item_result' BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
					t.Fatal(err)
				}
			}
			err = s.ReconcileCleanupOperation(ctx, m.ID, 0)
			if mode == "event_failure" {
				if err == nil {
					t.Fatal("event failure ignored")
				}
				var state string
				if err = s.db.QueryRow(`SELECT status FROM cleanup_batch_items WHERE ordinal=0`).Scan(&state); err != nil || state != "sending" {
					t.Fatalf("partial projection %s %v", state, err)
				}
				if _, err = s.db.Exec(`DROP TRIGGER fail_cleanup_result`); err != nil {
					t.Fatal(err)
				}
				if err = s.ReconcileCleanupOperation(ctx, m.ID, 0); err != nil {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			got, err := s.CleanupBatch(ctx, m.ID)
			want := "queued"
			if mode == "stop_inflight" {
				want = "stopped"
			}
			if err != nil || got.Status != want {
				t.Fatalf("batch %+v expected %s err=%v", got, want, err)
			}
			op.ID = strings.Repeat("e", 32)
			op.ObjectID = next.Expected.FsID
			op.SourcePath = next.Expected.Path
			err = s.PrepareCleanupOperation(ctx, m.ID, 1, scope, op)
			if (mode == "stop_inflight" && err == nil) || (mode != "stop_inflight" && err != nil) {
				t.Fatalf("next admission %s: %v", mode, err)
			}
		})
	}
}
