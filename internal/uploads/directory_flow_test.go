package uploads

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestDirectoryHTTPBatchRunsOnSharedWorkerAndRetainsResults(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, func(context.Context, string, string, func(string, int64) error, func(baiduapi.UploadTransferSample) error) (baiduapi.UploadReceipt, error) {
		t.Error("directory entered file runner")
		return baiduapi.UploadReceipt{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	m.SetDirectoryRunner(func(_ context.Context, remote string) (DirectoryReceipt, error) {
		return DirectoryReceipt{Status: "confirmed", Path: remote, RemoteID: "123", OperationID: strings.Repeat("a", 32), ObservedAtUTC: time.Now().UTC().Format(time.RFC3339Nano)}, nil
	})
	r := httptest.NewRequest("POST", "/api/uploads", strings.NewReader(`{"target":"/target","directories":["root","root/empty"]}`))
	w := httptest.NewRecorder()
	m.Handle(w, r)
	if w.Code != 200 {
		m.Close()
		t.Fatalf("create=%d %s", w.Code, w.Body.String())
	}
	var batch Batch
	if err = json.Unmarshal(w.Body.Bytes(), &batch); err != nil {
		m.Close()
		t.Fatal(err)
	}
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		got, e := m.Get(batch.ID)
		if e != nil {
			m.Close()
			t.Fatal(e)
		}
		if got.DirectorySummary != nil && got.DirectorySummary.Confirmed == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	r = httptest.NewRequest("GET", "/api/uploads/"+batch.ID, nil)
	r.SetPathValue("batch", batch.ID)
	w = httptest.NewRecorder()
	m.Handle(w, r)
	var got Batch
	if err = json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || got.DirectorySummary == nil || got.DirectorySummary.Confirmed != 2 || len(got.Items) != 0 {
		t.Fatalf("restored=%+v status=%d", got, w.Code)
	}
	if err = m.RestoreDirectoryIsolation(func(string) error { t.Error("confirmed directory isolated"); return nil }); err != nil {
		t.Fatal(err)
	}
}
