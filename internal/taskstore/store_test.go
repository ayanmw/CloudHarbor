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

func TestOpenRemovesOnlyAbandonedAtomicWriteTemps(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, subdir := range []string{"", "intake", "jobs"} {
		location := dir
		if subdir != "" {
			location = filepath.Join(dir, subdir)
			if err = os.MkdirAll(location, 0700); err != nil {
				t.Fatal(err)
			}
		}
		if err = os.WriteFile(filepath.Join(location, "record-abandoned.tmp"), []byte("incomplete"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(filepath.Join(dir, "notes.tmp"), []byte("user file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(filepath.Join(dir, "record-directory.tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	store, err = Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, subdir := range []string{"", "intake", "jobs"} {
		location := dir
		if subdir != "" {
			location = filepath.Join(dir, subdir)
		}
		if _, statErr := os.Stat(filepath.Join(location, "record-abandoned.tmp")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("abandoned atomic temp remains in %s: %v", location, statErr)
		}
	}
	for _, name := range []string{"notes.tmp", "record-directory.tmp"} {
		if _, statErr := os.Stat(filepath.Join(dir, name)); statErr != nil {
			t.Fatalf("non-temp entry %s was removed: %v", name, statErr)
		}
	}
}

func TestOpenDoesNotCleanTempsBeforeAcquiringWriterLock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	first, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	temp := filepath.Join(dir, "record-active.tmp")
	if err = os.WriteFile(temp, []byte("active writer temp"), 0600); err != nil {
		t.Fatal(err)
	}
	if second, secondErr := Open(dir, "/preview"); secondErr == nil {
		second.Close()
		t.Fatal("second writer unexpectedly acquired the queue")
	}
	if _, err = os.Stat(temp); err != nil {
		t.Fatalf("failed second opener removed temp under active writer: %v", err)
	}
}

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

func TestEventPageUsesStableExclusiveKeysetAndAscendingOrder(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 1; i <= 5; i++ {
		if err := s.SaveWithEvent(42, []byte(`{"status":"pending"}`), Event{Kind: "event-" + strconv.Itoa(i)}); err != nil {
			t.Fatal(err)
		}
	}
	dir := s.dir
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	first, before, more, err := s.EventPage(42, 0, 2)
	if err != nil || !more || len(first) != 2 || first[0].Kind != "event-4" || first[1].Kind != "event-5" || before != first[0].Sequence {
		t.Fatalf("latest page = events %#v cursor %d more %v err %v", first, before, more, err)
	}
	if err := s.SaveWithEvent(42, []byte(`{"status":"pending"}`), Event{Kind: "appended-between-pages"}); err != nil {
		t.Fatal(err)
	}
	second, nextBefore, more, err := s.EventPage(42, before, 2)
	if err != nil || !more || len(second) != 2 || second[0].Kind != "event-2" || second[1].Kind != "event-3" || nextBefore != second[0].Sequence {
		t.Fatalf("older page = events %#v cursor %d more %v err %v", second, nextBefore, more, err)
	}
	last, cursor, more, err := s.EventPage(42, nextBefore, 2)
	if err != nil || more || cursor != 0 || len(last) != 1 || last[0].Kind != "event-1" {
		t.Fatalf("oldest page = events %#v cursor %d more %v err %v", last, cursor, more, err)
	}
	if err := s.SaveWithEvent(43, []byte(`{"status":"pending"}`), Event{Kind: "other-task-only"}); err != nil {
		t.Fatal(err)
	}
	other, _, otherMore, err := s.EventPage(43, 0, 2)
	if err != nil || otherMore || len(other) != 1 || other[0].Kind != "other-task-only" {
		t.Fatalf("task-scoped event query = %#v more %v err %v", other, otherMore, err)
	}
	mainEvents, _, _, err := s.EventPage(42, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range mainEvents {
		if event.Kind == "other-task-only" {
			t.Fatal("task event query returned another task's evidence")
		}
	}
	if _, err := s.db.Exec(`INSERT INTO jobs(id,payload_json,status,updated_at_utc) VALUES(44, ?, 'pending', 0)`, []byte(`{"status":"pending"}`)); err != nil {
		t.Fatal(err)
	}
	empty, emptyCursor, emptyMore, err := s.EventPage(44, 0, 2)
	if err != nil || len(empty) != 0 || emptyCursor != 0 || emptyMore {
		t.Fatalf("empty event history = %#v cursor %d more %v err %v", empty, emptyCursor, emptyMore, err)
	}
	if _, _, _, err = s.EventPage(45, 0, 2); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing task error = %v, want os.ErrNotExist", err)
	}
	for _, tc := range []struct {
		before int64
		limit  int
	}{{before: -1, limit: 2}, {limit: 0}, {limit: 51}} {
		if _, _, _, err = s.EventPage(42, tc.before, tc.limit); err == nil {
			t.Fatalf("accepted invalid cursor/limit: %#v", tc)
		}
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

func TestLoadEachRawVisitsRecordsInPrimaryKeyOrder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, id := range []int64{2, 10, 1} {
		if err := s.Save(id, []byte(`{"id":`+strconv.FormatInt(id, 10)+`}`)); err != nil {
			t.Fatal(err)
		}
	}
	var ids []int64
	if err := s.LoadEachRaw(func(id int64, _ json.RawMessage) error {
		ids = append(ids, id)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []int64{1, 2, 10}) {
		t.Fatalf("raw record order = %v, want primary-key order [1 2 10]", ids)
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
