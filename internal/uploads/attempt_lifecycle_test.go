package uploads

import (
	"context"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestDurableUploadAttemptClaim(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	item := b.Items[0].ID
	if _, err = m.db.Exec(`UPDATE items SET state='queued' WHERE id=?`, item); err != nil {
		t.Fatal(err)
	}
	first, second := strings.Repeat("a", 32), strings.Repeat("b", 32)
	if _, err = m.db.Exec(`CREATE TRIGGER fail_attempt BEFORE INSERT ON events WHEN NEW.state='attempt_started' BEGIN SELECT RAISE(ABORT,'fixture'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = m.claimUploadAttempt(item, first); err == nil {
		t.Fatal("failed evidence accepted")
	}
	var state string
	if err = m.db.QueryRow(`SELECT state FROM items WHERE id=?`, item).Scan(&state); err != nil || state != "queued" || m.activeAttempts[item].ID != "" {
		t.Fatalf("partial claim state=%s err=%v", state, err)
	}
	if _, err = m.db.Exec(`DROP TRIGGER fail_attempt`); err != nil {
		t.Fatal(err)
	}
	claimed, err := m.claimUploadAttempt(item, first)
	if err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if _, err = m.db.Exec(`UPDATE items SET state='queued' WHERE id=?`, item); err != nil {
		t.Fatal(err)
	}
	if _, err = m.claimUploadAttempt(item, second); err == nil {
		t.Fatal("live attempt replaced")
	}
	if _, err = m.db.Exec(`CREATE TRIGGER fail_return BEFORE INSERT ON events WHEN NEW.state='attempt_runner_returned' BEGIN SELECT RAISE(ABORT,'fixture'); END`); err != nil {
		t.Fatal(err)
	}
	if err = m.recordUploadAttemptReturned(item, first); err == nil || m.activeAttempts[item].RunnerReturned {
		t.Fatal("failed return evidence changed memory")
	}
	if _, err = m.db.Exec(`DROP TRIGGER fail_return`); err != nil {
		t.Fatal(err)
	}
	if err = m.recordUploadAttemptReturned(item, first); err != nil {
		t.Fatal(err)
	}
	if err = m.recordUploadAttemptReturned(item, first); err == nil {
		t.Fatal("duplicate runner return")
	}
	m.finishUploadAttempt(item, first)
	claimed, err = m.claimUploadAttempt(item, second)
	if err != nil || !claimed {
		t.Fatalf("second=%v err=%v", claimed, err)
	}
	evidence, err := m.latestUploadAttempt(item)
	if err != nil || evidence.AttemptID != second || evidence.PreviousAttemptID != first {
		t.Fatalf("evidence=%+v err=%v", evidence, err)
	}
}

func TestAttemptClaimAcrossRestartAndBatchIsolation(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if m != nil {
			m.Close()
		}
	}()
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}, {Path: "b", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	item, other := b.Items[0].ID, b.Items[1].ID
	if _, err = m.db.Exec(`UPDATE items SET state='queued' WHERE batch=?`, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.db.Exec(`UPDATE items SET state='unknown' WHERE id=?`, other); err != nil {
		t.Fatal(err)
	}
	first, second := strings.Repeat("c", 32), strings.Repeat("d", 32)
	if claimed, e := m.claimUploadAttempt(item, first); e != nil || claimed {
		t.Fatalf("unknown sibling admitted: %v %v", claimed, e)
	}
	if _, err = m.db.Exec(`UPDATE items SET state='confirmed' WHERE id=?`, other); err != nil {
		t.Fatal(err)
	}
	if claimed, e := m.claimUploadAttempt(item, first); e != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, e)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m = nil
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.activeAttempts) != 0 {
		t.Fatal("historical attempt became live")
	}
	claimed, err := m.claimUploadAttempt(item, second)
	if err != nil || !claimed {
		t.Fatalf("reclaim: %v %v", claimed, err)
	}
	e, err := m.latestUploadAttempt(item)
	if err != nil || e.PreviousAttemptID != first || e.AttemptID != second {
		t.Fatalf("lost history: %+v %v", e, err)
	}
	// The old start remains historical; a restart does not invent a runner return.
	var returns int
	if err = m.db.QueryRow(`SELECT count(*) FROM events WHERE item=? AND state='attempt_runner_returned'`, item).Scan(&returns); err != nil || returns != 0 {
		t.Fatalf("fabricated return: %d %v", returns, err)
	}
}

func TestRunnerReturnWriteFailureDoesNotLeaveLiveAttempt(t *testing.T) {
	m, err := Open(t.TempDir(), func(_ context.Context, _, _ string, _ func(string, int64) error, _ func(baiduapi.UploadTransferSample) error) (baiduapi.UploadReceipt, error) {
		return baiduapi.UploadReceipt{Path: "/target/a", FsID: "42", Size: "3"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err = m.db.Exec(`CREATE TRIGGER fail_return BEFORE INSERT ON events WHEN NEW.state='attempt_runner_returned' BEGIN SELECT RAISE(ABORT,'fixture'); END`); err != nil {
		t.Fatal(err)
	}
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Receive(context.Background(), b.ID, b.Items[0].ID, strings.NewReader("abc")); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop on lifecycle failure")
	}
	if len(m.snapshotUploadAttempts()) != 0 {
		t.Fatal("returned runner remains live")
	}
	current, err := m.Get(b.ID)
	if err != nil || current.Items[0].State == "confirmed" {
		t.Fatalf("failed commit declared complete: %+v %v", current, err)
	}
}
