package uploads

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestStagingPersistsAndRestartIsolatesAnUnconfirmedRemoteWrite(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	batch, err := m.Create("/target", []Input{{Path: "folder/a.txt", Size: "5"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Receive(context.Background(), batch.ID, batch.Items[0].ID, strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	if err = m.update(batch.Items[0].ID, "finalizing", 5, ""); err != nil {
		t.Fatal(err)
	}
	m.Close()
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	read, err := m.Get(batch.ID)
	if err != nil || read.Items[0].State != "unknown" {
		t.Fatalf("read=%+v err=%v", read, err)
	}
	if _, err = os.Stat(filepath.Join(dir, batch.Items[0].ID+".data")); err != nil {
		t.Fatal(err)
	}
}
func TestUploadRejectsTraversalAndShortBody(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, p := range []string{"../x", "/x", "a/../x", "a\\x"} {
		if _, err = m.Create("/target", []Input{{Path: p, Size: "5"}}); err == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	b, err := m.Create("/target", []Input{{Path: "x", Size: "5"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Receive(context.Background(), b.ID, b.Items[0].ID, strings.NewReader("hi")); err == nil {
		t.Fatal("short content accepted")
	}
	read, _ := m.Get(b.ID)
	if read.Items[0].State != "receiving" {
		t.Fatal(read.Items[0].State)
	}
}

func TestWorkerConfirmsAndReclaimsOnlyItsStagingFile(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	m, err := Open(dir, func(ctx context.Context, local, remote string, report func(string, int64) error, observe func(baiduapi.UploadTransferSample) error) (baiduapi.UploadReceipt, error) {
		calls++
		if remote != "/target/folder/a.txt" {
			t.Errorf("target=%s", remote)
		}
		for _, phase := range []string{"hashing", "submitting", "uploading", "finalizing"} {
			if err := report(phase, 5); err != nil {
				return baiduapi.UploadReceipt{}, err
			}
		}
		return baiduapi.UploadReceipt{Path: remote, FsID: "123", Size: "5"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "folder/a.txt", Size: "5"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Receive(context.Background(), b.ID, b.Items[0].ID, strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		read, err := m.Get(b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if read.Items[0].State == "confirmed" {
			if read.Items[0].ActualPath != "/target/folder/a.txt" || read.Items[0].Sent != "5" {
				t.Fatal(read)
			}
			m.Close()
			if calls != 1 {
				t.Fatal(calls)
			}
			if _, err = os.Stat(filepath.Join(dir, b.Items[0].ID+".data")); !os.IsNotExist(err) {
				t.Fatal("confirmed payload not reclaimed")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("worker did not confirm")
}
func TestRestartRequeuesOnlySafeStagesAndNeverRetriesUnknown(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Create("/target", []Input{{Path: "a", Size: "1"}, {Path: "b", Size: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range b.Items {
		if err = m.Receive(context.Background(), b.ID, i.ID, strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
	}
	m.update(b.Items[0].ID, "uploading", 1, "")
	m.update(b.Items[1].ID, "submitting", 0, "")
	m.Close()
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.Retry(b.ID)
	read, _ := m.Get(b.ID)
	states := map[string]string{}
	for _, i := range read.Items {
		states[i.Path] = i.State
	}
	if states["a"] != "queued" || states["b"] != "unknown" {
		t.Fatal(states)
	}
	if err = m.Cancel(b.ID); err != nil {
		t.Fatal(err)
	}
	read, _ = m.Get(b.ID)
	for _, i := range read.Items {
		if i.Path == "a" && i.State != "cancelled" || i.Path == "b" && i.State != "unknown" {
			t.Fatal(read)
		}
	}
}

func TestCancellationAndStartupReclaimOnlyOwnedPayloads(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Create("/target", []Input{{Path: "a", Size: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	item := b.Items[0].ID
	if err = m.Receive(context.Background(), b.ID, item, strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	if err = m.Cancel(b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, item+".data")); !os.IsNotExist(err) {
		t.Fatal("cancelled payload retained")
	}
	os.WriteFile(filepath.Join(dir, item+".data"), []byte("x"), 0600)
	os.WriteFile(filepath.Join(dir, item+".part"), []byte("x"), 0600)
	os.WriteFile(filepath.Join(dir, "user.part"), []byte("keep"), 0600)
	m.Close()
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, suffix := range []string{".data", ".part"} {
		if _, err = os.Stat(filepath.Join(dir, item+suffix)); !os.IsNotExist(err) {
			t.Fatal("interrupted payload retained")
		}
	}
	if _, err = os.Stat(filepath.Join(dir, "user.part")); err != nil {
		t.Fatal("unrelated file removed")
	}
}

func TestRestoredUploadWaitsForAccountVerification(t *testing.T) {
	var ready atomic.Bool
	m, err := Open(t.TempDir(), func(ctx context.Context, local, remote string, report func(string, int64) error, observe func(baiduapi.UploadTransferSample) error) (baiduapi.UploadReceipt, error) {
		return baiduapi.UploadReceipt{Path: remote, FsID: "1", Size: "1"}, nil
	}, ready.Load)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "file", Size: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Receive(context.Background(), b.ID, b.Items[0].ID, strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	read, _ := m.Get(b.ID)
	if read.Items[0].State != "queued" {
		t.Fatal("claimed before account verification")
	}
	ready.Store(true)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		read, _ = m.Get(b.ID)
		if read.Items[0].State == "confirmed" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("verified upload did not resume")
}

func TestRestartRecoveryRecordsPriorStageAndTimeExactlyOnce(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if m != nil {
			_ = m.Close()
		}
	})
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}, {Path: "b", Size: "3"}, {Path: "c", Size: "3"}, {Path: "d", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	stages := []string{"hashing", "uploading", "submitting", "finalizing"}
	old := map[string]string{}
	for index, item := range b.Items {
		if err = m.update(item.ID, stages[index], int64(index%3), ""); err != nil {
			t.Fatal(err)
		}
		var at string
		if err = m.db.QueryRow(`SELECT updated FROM items WHERE id=?`, item.ID).Scan(&at); err != nil {
			t.Fatal(err)
		}
		old[item.ID] = at
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC()
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index, item := range b.Items {
		var state, at, message string
		var sent int64
		err = m.db.QueryRow(`SELECT state,sent,recorded,error FROM events WHERE item=? AND error LIKE 'restart_recovery:%'`, item.ID).Scan(&state, &sent, &at, &message)
		if err != nil {
			t.Fatalf("missing recovery event for %s: %v", stages[index], err)
		}
		expected := "queued"
		if index > 1 {
			expected = "unknown"
		}
		parsed, parseErr := time.Parse(time.RFC3339Nano, at)
		if state != expected || sent != int64(index%3) || !strings.Contains(message, stages[index]) || !strings.Contains(message, old[item.ID]) || parseErr != nil || parsed.Before(before) || !strings.HasSuffix(at, "Z") {
			t.Fatalf("bad recovery: %s %d %s %s", state, sent, at, message)
		}
		var updated string
		if err = m.db.QueryRow(`SELECT updated FROM items WHERE id=?`, item.ID).Scan(&updated); err != nil || updated != at {
			t.Fatalf("event/state timestamps differ: %s %s %v", at, updated, err)
		}
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	var count int
	if err = m.db.QueryRow(`SELECT count(*) FROM events WHERE error LIKE 'restart_recovery:%'`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("duplicate restart evidence: %d %v", count, err)
	}
}

func TestRestartRecoveryFailureRollsBackStateAndEvents(t *testing.T) {
	for _, testCase := range []struct{ name, trigger string }{
		{"event_write", `CREATE TRIGGER reject_recovery BEFORE INSERT ON events WHEN NEW.error LIKE 'restart_recovery:%' BEGIN SELECT RAISE(ABORT,'test recovery failure'); END;`},
		{"state_write", `CREATE TRIGGER reject_recovery BEFORE UPDATE ON items WHEN NEW.state='queued' BEGIN SELECT RAISE(ABORT,'test state failure'); END;`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			m, err := Open(dir, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = m.Close() })
			b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
			if err != nil {
				t.Fatal(err)
			}
			if err = m.update(b.Items[0].ID, "uploading", 2, ""); err != nil {
				t.Fatal(err)
			}
			var original string
			if err = m.db.QueryRow(`SELECT updated FROM items WHERE id=?`, b.Items[0].ID).Scan(&original); err != nil {
				t.Fatal(err)
			}
			if _, err = m.db.Exec(testCase.trigger); err != nil {
				t.Fatal(err)
			}
			if err = m.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(dir, nil)
			if err == nil {
				reopened.Close()
				t.Fatal("startup accepted a failed evidence write")
			}
			db, err := sql.Open("sqlite", filepath.Join(dir, "uploads.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			var state, updated string
			var sent int64
			if err = db.QueryRow(`SELECT state,sent,updated FROM items WHERE id=?`, b.Items[0].ID).Scan(&state, &sent, &updated); err != nil {
				t.Fatal(err)
			}
			if state != "uploading" || sent != 2 || updated != original {
				t.Fatalf("failed transaction changed prior evidence: %s %d %s", state, sent, updated)
			}
			var count int
			if err = db.QueryRow(`SELECT count(*) FROM events WHERE error LIKE 'restart_recovery:%'`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("partial recovery event: %d %v", count, err)
			}
		})
	}
}
