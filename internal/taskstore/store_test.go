package taskstore

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCredentialsAndAccountBindingAreIndependentFromQueueSettings(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveCredentials([]byte(`{"version":1,"access_token":"private-access"}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.BindAccount("3400000000000000123"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	body, err := s.LoadCredentials()
	if err != nil || string(body) != `{"version":1,"access_token":"private-access"}` {
		t.Fatalf("credential roundtrip: %q %v", body, err)
	}
	os.Remove(filepath.Join(dir, "credentials.json"))
	if err = s.BindAccount("999"); !errors.Is(err, ErrAccountMismatch) {
		t.Fatalf("removing credentials bypassed account binding: %v", err)
	}
	jobs, err := s.Load()
	if err != nil || len(jobs) != 0 {
		t.Fatalf("credential became task: %v %v", jobs, err)
	}
}

func TestRecordsSurviveReopenAndScopeCannotChange(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Save(42, []byte(`{"status":"pending"}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	records, err := s.Load()
	if err != nil || string(records[42]) != `{"status":"pending"}` {
		t.Fatalf("records=%s err=%v", records[42], err)
	}
	s.Close()
	if other, err := Open(dir, "/another-queue"); err == nil {
		other.Close()
		t.Fatal("accepted different cloud queue")
	}
}

func TestSaveBatchRoundTripsRecordsAndRejectsInvalidBatchWithoutPublishing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.SaveBatch([]Record{
		{ID: 7, Data: []byte(`{"status":"pending","id":7}`)},
		{ID: 9, Data: []byte(`{"status":"done","id":9}`)},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded[7]) != `{"status":"pending","id":7}` || string(loaded[9]) != `{"status":"done","id":9}` {
		t.Fatalf("batch records = %q, %q", loaded[7], loaded[9])
	}

	err = s.SaveBatch([]Record{
		{ID: 11, Data: []byte(`{"status":"pending"}`)},
		{ID: 12, Data: []byte(`{invalid}`)},
	})
	if err == nil {
		t.Fatal("invalid JSON batch was accepted")
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM jobs WHERE id=11`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("valid prefix of rejected batch was published: count=%d err=%v", count, err)
	}
}

func TestLoadRetainsIndependentRecordBytes(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []int64{1, 2, 3} {
		body := []byte(`{"id":` + strconv.FormatInt(id, 10) + `}`)
		if err := s.Save(id, body); err != nil {
			t.Fatal(err)
		}
	}
	records, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	for id := int64(1); id <= 3; id++ {
		var record struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(records[id], &record); err != nil {
			t.Fatalf("decode record %d: %v", id, err)
		}
		if record.ID != id {
			t.Fatalf("record %d contains id %d", id, record.ID)
		}
	}
}

func TestLoadEachVisitsRecordsInFilenameOrderWithoutBuildingResultMap(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []int64{9, 7} {
		if err := s.Save(id, []byte(`{"id":`+strconv.FormatInt(id, 10)+`}`)); err != nil {
			t.Fatal(err)
		}
	}
	var ids []int64
	if err := s.LoadEach(func(id int64, body json.RawMessage) error {
		ids = append(ids, id)
		if !json.Valid(body) {
			t.Fatalf("callback received invalid JSON for %d", id)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []int64{7, 9}) {
		t.Fatalf("record order=%v, want [7 9]", ids)
	}
}

func TestLoadEachStopsAndReturnsCallbackError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []int64{7, 9} {
		if err := s.Save(id, []byte(`{"id":`+strconv.FormatInt(id, 10)+`}`)); err != nil {
			t.Fatal(err)
		}
	}
	wantErr := errors.New("stop loading")
	var visited []int64
	err = s.LoadEach(func(id int64, _ json.RawMessage) error {
		visited = append(visited, id)
		return wantErr
	})
	if !errors.Is(err, wantErr) || !reflect.DeepEqual(visited, []int64{7}) {
		t.Fatalf("err=%v visited=%v", err, visited)
	}
}

func TestLoadEachRawLeavesJSONValidationToVisitor(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Save(7, []byte(`{"status":"pending"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE jobs SET payload_json=? WHERE id=7`, []byte(`{broken`)); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("visitor rejected malformed JSON")
	visited := false
	err = s.LoadEachRaw(func(id int64, body json.RawMessage) error {
		visited = true
		if id != 7 || json.Valid(body) {
			t.Fatalf("visitor received id=%d body=%q", id, body)
		}
		return wantErr
	})
	if !visited || !errors.Is(err, wantErr) {
		t.Fatalf("visitor validation result: visited=%v err=%v", visited, err)
	}
}

func TestLoadEachRawKeepsEachRecordValidDuringCallback(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []int64{1, 2} {
		if err := s.Save(id, []byte(`{"value":"same-size-record"}`)); err != nil {
			t.Fatal(err)
		}
	}
	visits := 0
	err = s.LoadEachRaw(func(_ int64, body json.RawMessage) error {
		if string(body) != `{"value":"same-size-record"}` {
			t.Fatalf("visitor received corrupt/reused bytes: %q", body)
		}
		// The public contract limits body to this callback; its backing buffer
		// address is intentionally unspecified because records may be prefetched.
		visits++
		return nil
	})
	if err != nil || visits != 2 {
		t.Fatalf("visits=%d err=%v", visits, err)
	}
}

func TestLoadEachRawPreservesRecordSizeLimit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const maxRecordSize = 1024 * 1024
	if err := s.Save(1, []byte(`{}`+strings.Repeat(" ", maxRecordSize-2))); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO jobs(id,payload_json,status,updated_at_utc) VALUES(2,?,'pending',0)`, []byte(`{}`+strings.Repeat(" ", maxRecordSize-1))); err != nil {
		t.Fatal(err)
	}
	visited := 0
	err = s.LoadEachRaw(func(id int64, body json.RawMessage) error {
		if id != 1 || len(body) != maxRecordSize {
			t.Fatalf("unexpected boundary record: id=%d size=%d", id, len(body))
		}
		visited++
		return nil
	})
	if err == nil || visited != 1 {
		t.Fatalf("oversized record was not rejected: visited=%d err=%v", visited, err)
	}
}

func TestStoreRejectsCorruptRecordAndSecondWriter(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if other, err := Open(dir, "/preview"); err == nil {
		other.Close()
		t.Fatal("accepted second writer")
	}
	if err = s.Save(1, []byte(`{broken`)); err == nil {
		t.Fatal("accepted invalid JSON")
	}
	if _, err = s.db.Exec(`INSERT INTO jobs(id,payload_json,status,updated_at_utc) VALUES(1,?,'pending',0)`, []byte(`{broken`)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Load(); err == nil {
		t.Fatal("silently ignored corrupt job")
	}
}

func TestStoreLockReleasedByProcessExit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	cmd := exec.Command(os.Args[0], "-test.run=^TestStoreProcessHelper$")
	cmd.Env = append(os.Environ(), "NAS_STORE_HELPER="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	ready := filepath.Join(dir, "ready")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper failed to acquire lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if s, err := Open(dir, "/preview"); err == nil {
		s.Close()
		t.Fatal("cross-process lock not enforced")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatalf("crash left permanent lock: %v", err)
	}
	s.Close()
}

func TestStoreProcessHelper(t *testing.T) {
	dir := os.Getenv("NAS_STORE_HELPER")
	if dir == "" {
		return
	}
	s, err := Open(dir, "/preview")
	if err != nil {
		os.Exit(2)
	}
	defer s.Close()
	if err = os.WriteFile(filepath.Join(dir, "ready"), []byte("ready"), 0600); err != nil {
		os.Exit(3)
	}
	for {
		time.Sleep(time.Hour)
	}
}
