package taskstore

import (
	"context"
	"testing"
)

func TestCleanupDetailsHaveScopedExactItemsAndEventCursor(t *testing.T) {
	s, err := Open(t.TempDir(), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	m := cleanupManifestFixture()
	scope := CleanupBatchScope{m.AccountFingerprint, m.ApplicationFingerprint, m.CacheNamespace}
	if _, err = s.CreateCleanupBatch(ctx, m); err != nil {
		t.Fatal(err)
	}
	items, total, err := s.CleanupItemPage(ctx, m.ID, scope, 0, 25)
	if err != nil || total != 1 || len(items) != 1 || items[0].Expected.FsID != m.Items[0].Expected.FsID || items[0].Status != "pending" || items[0].OperationID != "" {
		t.Fatalf("items %+v total=%d err=%v", items, total, err)
	}
	if err = s.StopCleanupBatch(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	events, more, err := s.CleanupEventPage(ctx, m.ID, scope, 0, 1)
	if err != nil || !more || len(events) != 1 || events[0].Kind != "created" {
		t.Fatalf("events %+v more=%v err=%v", events, more, err)
	}
	next, more, err := s.CleanupEventPage(ctx, m.ID, scope, events[0].Sequence, 1)
	if err != nil || more || len(next) != 1 || next[0].Kind != "stop_requested" || next[0].ResponseObservedAtUTC != nil {
		t.Fatalf("events %+v more=%v err=%v", next, more, err)
	}
	wrong := scope
	wrong.ApplicationFingerprint = "wrong"
	if _, _, err = s.CleanupItemPage(ctx, m.ID, wrong, 0, 25); err == nil {
		t.Fatal("scope leak")
	}
	if _, _, err = s.CleanupEventPage(ctx, m.ID, scope, 0, 101); err == nil {
		t.Fatal("unbounded events accepted")
	}
}
