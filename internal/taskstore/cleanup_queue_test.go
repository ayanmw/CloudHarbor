package taskstore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func TestCleanupQueueSelectsScopedPendingAndReportsCounts(t *testing.T) {
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
	second := m
	second.ID = strings.Repeat("e", 32)
	if _, err = s.CreateCleanupBatch(ctx, second); err != nil {
		t.Fatal(err)
	}
	id, ordinal, err := s.NextCleanupItem(ctx, scope)
	if err != nil || id != m.ID || ordinal != 0 {
		t.Fatalf("next=%s/%d %v", id, ordinal, err)
	}
	if err = s.StopCleanupBatch(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	id, _, err = s.NextCleanupItem(ctx, scope)
	if err != nil || id != second.ID {
		t.Fatalf("stopped selected: %s %v", id, err)
	}
	page, total, err := s.CleanupBatchPage(ctx, scope, 0, 1)
	if err != nil || total != 2 || len(page) != 1 || page[0].ID != second.ID || page[0].Pending != 1 {
		t.Fatalf("page %+v total=%d err=%v", page, total, err)
	}
	wrong := scope
	wrong.AccountFingerprint = strings.Repeat("0", 64)
	_, _, err = s.NextCleanupItem(ctx, wrong)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("scope leak %v", err)
	}
	if _, _, err = s.CleanupBatchPage(ctx, scope, 0, 51); err == nil {
		t.Fatal("unbounded page accepted")
	}
}
