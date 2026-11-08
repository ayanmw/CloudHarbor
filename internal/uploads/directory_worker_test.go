package uploads

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDirectoryWorkerStoresReceiptAndDoesNotReplay(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.createManifest("/target", nil, []string{"root", "root/empty"})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	m.SetDirectoryRunner(func(ctx context.Context, remote string) (DirectoryReceipt, error) {
		calls++
		expected := "/target/root"
		if calls == 2 {
			expected += "/empty"
		}
		if remote != expected {
			t.Fatalf("path=%s expected=%s", remote, expected)
		}
		return DirectoryReceipt{Status: "confirmed", Path: remote, OperationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RemoteID: "9007199254740993", ObservedAtUTC: time.Now().UTC().Format(time.RFC3339Nano)}, nil
	})
	for i := 0; i < 2; i++ {
		if worked, err := m.processNextDirectory(); err != nil || !worked {
			t.Fatalf("worked=%v err=%v", worked, err)
		}
	}
	if worked, err := m.processNextDirectory(); err != nil || worked {
		t.Fatalf("replayed worked=%v err=%v", worked, err)
	}
	got, err := m.Get(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || got.Directories[1].State != "confirmed" || got.Directories[1].ActualPath != "/target/root/empty" {
		t.Fatalf("calls=%d batch=%+v", calls, got)
	}
	var evidence string
	if err = m.db.QueryRow(`SELECT evidence FROM upload_directory_events WHERE directory=? AND state='confirmed'`, got.Directories[1].ID).Scan(&evidence); err != nil {
		t.Fatal(err)
	}
	if evidence == "" {
		t.Fatal("receipt evidence missing")
	}
}

func TestDirectoryWorkerUnknownBlocksRemainingAndRestart(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.createManifest("/target", nil, []string{"root", "root/empty"})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	m.SetDirectoryRunner(func(context.Context, string) (DirectoryReceipt, error) {
		calls++
		return DirectoryReceipt{Status: "unknown", OperationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, errors.New("lost response")
	})
	if _, err = m.processNextDirectory(); err != nil {
		t.Fatal(err)
	}
	if worked, err := m.processNextDirectory(); err != nil || worked {
		t.Fatalf("unknown admitted next: %v %v", worked, err)
	}
	m.Close()
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.SetDirectoryRunner(func(context.Context, string) (DirectoryReceipt, error) {
		t.Fatal("unknown replayed after restart")
		return DirectoryReceipt{}, nil
	})
	if worked, err := m.processNextDirectory(); err != nil || worked {
		t.Fatalf("restart admitted %v %v", worked, err)
	}
	got, _ := m.Get(b.ID)
	if calls != 1 || got.Directories[0].State != "unknown" {
		t.Fatalf("calls=%d batch=%+v", calls, got)
	}
}

func TestDirectoryWorkerRejectsInvalidSuccessAndHonorsReadiness(t *testing.T) {
	for _, receipt := range []DirectoryReceipt{
		{Status: "confirmed", Path: "/other", RemoteID: "1", OperationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ObservedAtUTC: time.Now().UTC().Format(time.RFC3339Nano)},
		{Status: "confirmed", Path: "/target/root", RemoteID: "1", ObservedAtUTC: time.Now().UTC().Format(time.RFC3339Nano)},
		{Status: "existing", Path: "/target/root", RemoteID: "0", ObservedAtUTC: time.Now().UTC().Format(time.RFC3339Nano)},
	} {
		ready := false
		m, err := Open(t.TempDir(), nil, func() bool { return ready })
		if err != nil {
			t.Fatal(err)
		}
		b, err := m.createManifest("/target", nil, []string{"root"})
		if err != nil {
			t.Fatal(err)
		}
		calls := 0
		m.SetDirectoryRunner(func(context.Context, string) (DirectoryReceipt, error) { calls++; return receipt, nil })
		if worked, err := m.processNextDirectory(); err != nil || worked || calls != 0 {
			t.Fatalf("unready admitted %v %v calls=%d", worked, err, calls)
		}
		ready = true
		if _, err = m.processNextDirectory(); err != nil {
			t.Fatal(err)
		}
		got, _ := m.Get(b.ID)
		if got.Directories[0].State != "unknown" {
			t.Fatalf("invalid success released: %+v", got)
		}
		m.Close()
	}
}

func TestDirectoryWorkerReceiptPersistenceFailureLeavesSending(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.createManifest("/target", nil, []string{"root"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.db.Exec(`CREATE TRIGGER reject_confirmed_directory BEFORE INSERT ON upload_directory_events WHEN NEW.state='confirmed' BEGIN SELECT RAISE(ABORT,'receipt failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	m.SetDirectoryRunner(func(_ context.Context, remote string) (DirectoryReceipt, error) {
		return DirectoryReceipt{Status: "confirmed", Path: remote, RemoteID: "1", OperationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ObservedAtUTC: time.Now().UTC().Format(time.RFC3339Nano)}, nil
	})
	if _, err = m.processNextDirectory(); err == nil {
		t.Fatal("receipt failure ignored")
	}
	got, _ := m.Get(b.ID)
	if got.Directories[0].State != "sending" {
		t.Fatalf("receipt failure released: %+v", got)
	}
}

func TestDirectoryWorkerCannotSendWithoutDurableIntent(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.createManifest("/target", nil, []string{"root"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.db.Exec(`CREATE TRIGGER reject_directory_intent BEFORE INSERT ON upload_directory_events WHEN NEW.state='sending' BEGIN SELECT RAISE(ABORT,'intent failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	m.SetDirectoryRunner(func(context.Context, string) (DirectoryReceipt, error) {
		t.Fatal("sent without intent")
		return DirectoryReceipt{}, nil
	})
	if _, err = m.processNextDirectory(); err == nil {
		t.Fatal("intent failure ignored")
	}
	got, _ := m.Get(b.ID)
	if got.Directories[0].State != "queued" {
		t.Fatalf("unsent state changed: %+v", got)
	}
}

func TestDirectoryWorkerExistingDoesNotInventCreationReceipt(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.createManifest("/target", nil, []string{"root"})
	if err != nil {
		t.Fatal(err)
	}
	m.SetDirectoryRunner(func(_ context.Context, remote string) (DirectoryReceipt, error) {
		return DirectoryReceipt{Status: "existing", Path: remote, RemoteID: "42", ObservedAtUTC: time.Now().UTC().Format(time.RFC3339Nano)}, nil
	})
	if _, err = m.processNextDirectory(); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Get(b.ID)
	if got.Directories[0].State != "existing" || got.Directories[0].OperationID != "" {
		t.Fatalf("invented create: %+v", got)
	}
}

func TestDirectoryWorkerExplicitFailureStopsRemainingDirectories(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.createManifest("/target", nil, []string{"root", "root/empty"})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	m.SetDirectoryRunner(func(context.Context, string) (DirectoryReceipt, error) {
		calls++
		return DirectoryReceipt{Status: "failed"}, errors.New("definitely not sent")
	})
	if _, err = m.processNextDirectory(); err != nil {
		t.Fatal(err)
	}
	if worked, err := m.processNextDirectory(); err != nil || worked || calls != 1 {
		t.Fatalf("continued failed batch: %v %v calls=%d", worked, err, calls)
	}
	got, _ := m.Get(b.ID)
	if got.Directories[0].State != "failed" || got.Directories[1].State != "queued" {
		t.Fatalf("batch=%+v", got)
	}
}
