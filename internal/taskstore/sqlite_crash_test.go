package taskstore

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteProcessCrashRollsBackUncommittedProjectionAndEvent(t *testing.T) {
	if dir := os.Getenv("NAS_SQLITE_CRASH_HELPER"); dir != "" {
		sqliteCrashHelper(t, dir)
		return
	}

	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SaveWithEvent(1, []byte(`{"status":"pending"}`), Event{Kind: "job_registered"}); err != nil {
		store.Close()
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestSQLiteProcessCrashRollsBackUncommittedProjectionAndEvent$")
	cmd.Env = append(os.Environ(), "NAS_SQLITE_CRASH_HELPER="+dir)
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(dir, "crash-transaction-ready")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err = os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatal("helper did not persist its uncommitted projection and event")
		}
		time.Sleep(10 * time.Millisecond)
	}

	journal := filepath.Join(dir, "queue.sqlite-journal")
	if info, statErr := os.Stat(journal); statErr != nil || info.Size() == 0 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("SQLite rollback journal was not present before crash: info=%v err=%v", info, statErr)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()

	store, err = Open(dir, "/queue")
	if err != nil {
		t.Fatalf("reopen after process crash: %v", err)
	}
	defer store.Close()
	var payload []byte
	if err = store.db.QueryRow(`SELECT payload_json FROM jobs WHERE id=1`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if string(payload) != `{"status":"pending"}` {
		t.Fatalf("uncommitted projection survived process crash: %s", payload)
	}
	events, err := store.Events(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "job_registered" {
		t.Fatalf("uncommitted event survived process crash: %#v", events)
	}
}

func sqliteCrashHelper(t *testing.T, dir string) {
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatalf("helper open: %v", err)
	}
	tx, err := store.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("helper begin: %v", err)
	}
	evidence, _ := json.Marshal(struct {
		From string `json:"from_status"`
		To   string `json:"to_status"`
	}{"pending", "done"})
	if err = saveJobTx(context.Background(), tx, store.runID, 1, []byte(`{"status":"done"}`), Event{Kind: "job_completed", Evidence: evidence}); err != nil {
		t.Fatalf("helper save transaction: %v", err)
	}
	if err = os.WriteFile(filepath.Join(dir, "crash-transaction-ready"), []byte("ready"), 0600); err != nil {
		t.Fatalf("helper signal: %v", err)
	}
	for {
		time.Sleep(time.Hour)
	}
}
