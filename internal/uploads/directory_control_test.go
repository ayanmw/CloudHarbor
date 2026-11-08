package uploads

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDirectoryCancelOnlyStopsQueuedAndPreservesUnknown(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.createManifest("/target", []Input{{Path: "file", Size: "1"}}, []string{"queued", "sending", "unknown", "confirmed", "existing", "failed"})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range b.Directories {
		if _, err = m.db.Exec(`UPDATE upload_directories SET state=? WHERE id=?`, d.Path, d.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err = m.Cancel(b.ID); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range got.Directories {
		expected := d.Path
		if expected == "queued" {
			expected = "cancelled"
		}
		if d.State != expected {
			t.Fatalf("directory=%+v", d)
		}
	}
	if got.Items[0].State != "cancelled" {
		t.Fatalf("file=%+v", got.Items[0])
	}
	if got.DirectorySummary == nil || got.DirectorySummary.Cancelled != 1 || got.DirectorySummary.Unknown != 1 || got.DirectorySummary.Sending != 1 {
		t.Fatalf("summary=%+v", got.DirectorySummary)
	}
}

func TestDirectoryCancelDuringRequestPreservesReturnedReceipt(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.createManifest("/target", nil, []string{"root", "root/empty"})
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	m.SetDirectoryRunner(func(_ context.Context, remote string) (DirectoryReceipt, error) {
		close(started)
		<-release
		return DirectoryReceipt{Status: "confirmed", Path: remote, RemoteID: "42", OperationID: strings.Repeat("a", 32), ObservedAtUTC: time.Now().UTC().Format(time.RFC3339Nano)}, nil
	})
	done := make(chan error, 1)
	go func() { _, e := m.processNextDirectory(); done <- e }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("directory runner not started")
	}
	if err = m.Cancel(b.ID); err != nil {
		close(release)
		<-done
		t.Fatal(err)
	}
	got, _ := m.Get(b.ID)
	if got.Directories[0].State != "sending" || got.Directories[1].State != "cancelled" {
		close(release)
		<-done
		t.Fatalf("cancel=%+v", got)
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	got, _ = m.Get(b.ID)
	if got.Directories[0].State != "confirmed" || got.Directories[1].State != "cancelled" {
		t.Fatalf("late receipt lost: %+v", got)
	}
}

func TestDirectoryRetryEvidenceFailureLeavesFailedState(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.createManifest("/target", nil, []string{"root"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.db.Exec(`UPDATE upload_directories SET state='failed' WHERE batch=?`, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.db.Exec(`CREATE TRIGGER fail_directory_retry BEFORE INSERT ON upload_directory_events WHEN NEW.state='queued' BEGIN SELECT RAISE(ABORT,'retry evidence failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err = m.Retry(b.ID); err == nil {
		t.Fatal("retry evidence failure ignored")
	}
	got, _ := m.Get(b.ID)
	if got.Directories[0].State != "failed" {
		t.Fatalf("failed state lost: %+v", got)
	}
}

func TestDirectoryHTTPKeepsLegacyProtocolAndRequiresRunner(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"target":"/target","files":[{"path":"file","size":"0"}]}`, 200},
		{`{"target":"/target","directories":["root"]}`, 409},
		{`{"target":"/target","files":[{"path":"file","size":"0"}],"ignored":true}`, 400},
	} {
		r := httptest.NewRequest("POST", "/api/uploads", strings.NewReader(tc.body))
		w := httptest.NewRecorder()
		m.Handle(w, r)
		if w.Code != tc.status {
			t.Fatalf("status=%d expected=%d body=%s", w.Code, tc.status, w.Body.String())
		}
		if tc.status == 200 {
			var response map[string]any
			if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if _, exists := response["directory_summary"]; exists {
				t.Fatal("directory summary invented for legacy file batch")
			}
		}
	}
	listed, err := m.List()
	if err != nil || len(listed) != 1 {
		t.Fatalf("rejected requests persisted: %+v %v", listed, err)
	}
}

func TestDirectoryCancelEvidenceFailureRollsBackFilesAndDirectories(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.createManifest("/target", []Input{{Path: "file", Size: "1"}}, []string{"root"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.db.Exec(`CREATE TRIGGER fail_directory_cancel BEFORE INSERT ON upload_directory_events WHEN NEW.state='cancelled' BEGIN SELECT RAISE(ABORT,'cancel evidence failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err = m.Cancel(b.ID); err == nil {
		t.Fatal("cancellation evidence failure ignored")
	}
	got, err := m.Get(b.ID)
	if err != nil || got.Items[0].State != "receiving" || got.Directories[0].State != "queued" {
		t.Fatalf("partial cancellation %+v %v", got, err)
	}
}

func TestDirectoryRetryOnlyRequeuesDefiniteFailureAndRecordsPriorTime(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.createManifest("/target", nil, []string{"failed", "unknown", "sending", "cancelled"})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range b.Directories {
		if _, err = m.db.Exec(`UPDATE upload_directories SET state=?,updated='2026-11-01T01:02:03Z' WHERE id=?`, d.Path, d.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err = m.Retry(b.ID); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range got.Directories {
		expected := d.Path
		if expected == "failed" {
			expected = "queued"
		}
		if d.State != expected {
			t.Fatalf("directory=%+v", d)
		}
		if d.Path == "failed" {
			var evidence string
			if err = m.db.QueryRow(`SELECT evidence FROM upload_directory_events WHERE directory=? ORDER BY sequence DESC LIMIT 1`, d.ID).Scan(&evidence); err != nil || !strings.Contains(evidence, "2026-11-01T01:02:03Z") {
				t.Fatalf("evidence=%s err=%v", evidence, err)
			}
		}
	}
	if worked, err := m.processNextDirectory(); err != nil || worked {
		t.Fatalf("isolated batch admitted %v %v", worked, err)
	}
}

func TestDirectoryHTTPPureBatchAndSummaryPreserveFileCounts(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.SetDirectoryRunner(func(context.Context, string) (DirectoryReceipt, error) { return DirectoryReceipt{}, nil })
	r := httptest.NewRequest("POST", "/api/uploads", strings.NewReader(`{"target":"/target","directories":["root","root/empty"]}`))
	w := httptest.NewRecorder()
	m.Handle(w, r)
	if w.Code != 200 {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var created Batch
	if err = json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if len(created.Items) != 0 || len(created.Directories) != 2 || created.DirectorySummary == nil || created.DirectorySummary.Total != 2 || created.Total != 0 {
		t.Fatalf("created=%+v", created)
	}
	listed, err := m.List()
	if err != nil || len(listed) != 1 || listed[0].DirectorySummary == nil || listed[0].DirectorySummary.Queued != 2 || listed[0].Total != 0 {
		t.Fatalf("list=%+v err=%v", listed, err)
	}
	r = httptest.NewRequest("PUT", "/api/uploads/content", strings.NewReader(""))
	r.SetPathValue("batch", created.ID)
	r.SetPathValue("item", created.Directories[0].ID)
	w = httptest.NewRecorder()
	m.Handle(w, r)
	if w.Code != 409 {
		t.Fatalf("directory accepted as content: %d", w.Code)
	}
}
