package taskstore

import (
	"context"
	"testing"
)

func TestCleanupManifestReadAndPreflightFailureStayLocal(t *testing.T) {
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
	got, err := s.CleanupManifest(ctx, m.ID, scope)
	if err != nil || got.Items[0].Expected.FsID != m.Items[0].Expected.FsID || !got.CreatedAtUTC.Equal(m.CreatedAtUTC) {
		t.Fatalf("manifest %+v %v", got, err)
	}
	wrong := scope
	wrong.CacheNamespace = "wrong"
	if _, err = s.CleanupManifest(ctx, m.ID, wrong); err == nil {
		t.Fatal("wrong scope read manifest")
	}
	if err = s.FailCleanupPreflight(ctx, m.ID, 0, scope, "source_identity_changed"); err != nil {
		t.Fatal(err)
	}
	if err = s.FailCleanupPreflight(ctx, m.ID, 0, scope, "source_identity_changed"); err != nil {
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
	batch, err := s.CleanupBatch(ctx, m.ID)
	if err != nil || batch.Status != "failed" {
		t.Fatalf("batch %+v %v", batch, err)
	}
	var remote, events int
	if err = s.db.QueryRow(`SELECT count(*) FROM remote_operations`).Scan(&remote); err != nil || remote != 0 {
		t.Fatalf("invented remote intent %d %v", remote, err)
	}
	if err = s.db.QueryRow(`SELECT count(*) FROM cleanup_batch_events WHERE kind='preflight_failed' AND operation_id IS NULL AND response_observed_at_utc IS NULL AND reason='source_identity_changed'`).Scan(&events); err != nil || events != 1 {
		t.Fatalf("preflight evidence %d %v", events, err)
	}
}

func TestCleanupPreflightFailureRollsBackAndRejectsSending(t *testing.T) {
	for _, mode := range []string{"event_failure", "sending", "invalid_reason", "wrong_owner"} {
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
			reason := "source_identity_changed"
			switch mode {
			case "event_failure":
				_, err = s.db.Exec(`CREATE TRIGGER fail_preflight_event BEFORE INSERT ON cleanup_batch_events WHEN NEW.kind='preflight_failed' BEGIN SELECT RAISE(ABORT,'injected'); END`)
			case "sending":
				_, err = s.db.Exec(`UPDATE cleanup_batch_items SET status='sending'`)
			case "invalid_reason":
				reason = "https://example.invalid/?access_token=secret"
			case "wrong_owner":
				scope.ApplicationFingerprint = "wrong"
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = s.FailCleanupPreflight(ctx, m.ID, 0, scope, reason); err == nil {
				t.Fatal("unsafe preflight transition accepted")
			}
			var state string
			if err = s.db.QueryRow(`SELECT status FROM cleanup_batch_items`).Scan(&state); err != nil {
				t.Fatal(err)
			}
			want := "pending"
			if mode == "sending" {
				want = "sending"
			}
			if state != want {
				t.Fatalf("partial change %s expected %s", state, want)
			}
			var events int
			if err = s.db.QueryRow(`SELECT count(*) FROM cleanup_batch_events WHERE kind='preflight_failed'`).Scan(&events); err != nil || events != 0 {
				t.Fatalf("unexpected evidence %d %v", events, err)
			}
		})
	}
}
