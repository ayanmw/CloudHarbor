package taskstore

import (
	"context"
	"strings"
	"testing"
)

func TestCleanupChangeSequenceScopesAllBatchesAndSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	m := cleanupManifestFixture()
	scope := CleanupBatchScope{m.AccountFingerprint, m.ApplicationFingerprint, m.CacheNamespace}
	check := func(want int64) {
		t.Helper()
		got, e := s.CleanupChangeSequence(ctx, scope)
		if e != nil || got != want {
			t.Fatalf("sequence=%d want=%d err=%v", got, want, e)
		}
	}
	check(0)
	for i := 0; i < 3; i++ {
		m.ID = strings.Repeat(string(rune('a'+i)), 32)
		if _, err = s.CreateCleanupBatch(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	// A success in the oldest batch must be seen even when newer batches exist.
	add := func(id, reason string) int64 {
		t.Helper()
		r, e := s.db.Exec(`INSERT INTO cleanup_batch_events(batch_id,kind,recorded_at_utc,reason) VALUES(?,'item_result','2000-01-01T00:00:00Z',?)`, id, reason)
		if e != nil {
			t.Fatal(e)
		}
		n, e := r.LastInsertId()
		if e != nil {
			t.Fatal(e)
		}
		return n
	}
	add(strings.Repeat("b", 32), "remote_failed")
	add(strings.Repeat("c", 32), "remote_unknown")
	check(0)
	n := add(strings.Repeat("a", 32), "remote_confirmed")
	check(n)
	for i := 0; i < 3; i++ {
		other := scope
		switch i {
		case 0:
			other.AccountFingerprint = strings.Repeat("9", 64)
		case 1:
			other.ApplicationFingerprint = strings.Repeat("9", 64)
		case 2:
			other.CacheNamespace = strings.Repeat("9", 64)
		}
		got, e := s.CleanupChangeSequence(ctx, other)
		if e != nil || got != 0 {
			t.Fatalf("scope leak: %d %v", got, e)
		}
	}
	n = add(strings.Repeat("b", 32), "confirmed_before_local_reconciliation")
	// Event precision must not depend on JavaScript's safe integer range.
	if _, err = s.db.Exec(`INSERT INTO cleanup_batch_events(sequence,batch_id,kind,recorded_at_utc,reason) VALUES(9007199254740993,?,'item_result','1999-01-01T00:00:00Z','remote_confirmed')`, strings.Repeat("a", 32)); err != nil {
		t.Fatal(err)
	}
	n = 9007199254740993

	check(n)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	check(n)
	if _, err = s.CleanupChangeSequence(ctx, CleanupBatchScope{}); err == nil {
		t.Fatal("invalid scope accepted")
	}
}
