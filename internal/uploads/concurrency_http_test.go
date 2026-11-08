package uploads

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadConcurrencyHTTPStrictValuesAndReadback(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	call := func(method, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/api/uploads/settings/concurrency", strings.NewReader(body))
		w := httptest.NewRecorder()
		m.HandleConcurrency(w, r)
		return w
	}
	for _, body := range []string{`{}`, `{"limit":null}`, `{"limit":0}`, `{"limit":3}`, `{"limit":"2"}`, `{"limit":1.5}`, `{"limit":2,"other":true}`, `{"limit":2}{}`} {
		if w := call("POST", body); w.Code != 400 {
			t.Fatalf("accepted %s status=%d", body, w.Code)
		}
	}
	if w := call("DELETE", ""); w.Code != 405 {
		t.Fatalf("delete=%d", w.Code)
	}
	w := call("POST", `{"limit":2}`)
	if w.Code != 200 {
		t.Fatalf("save=%d %s", w.Code, w.Body.String())
	}
	var status UploadConcurrencyStatus
	if err = json.Unmarshal(w.Body.Bytes(), &status); err != nil || status.Limit != 2 || status.UpdatedAtUTC == "" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	w = call("GET", "")
	if err = json.Unmarshal(w.Body.Bytes(), &status); err != nil || status.Limit != 2 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("readback=%+v status=%d err=%v", status, w.Code, err)
	}
}

func TestUploadConcurrencyHTTPReportsStorageFailureWithoutChangingLimit(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err = m.db.Exec(`CREATE TRIGGER fail_concurrency_http BEFORE INSERT ON upload_setting_events BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	m.HandleConcurrency(w, httptest.NewRequest("POST", "/api/uploads/settings/concurrency", strings.NewReader(`{"limit":2}`)))
	if w.Code != 409 || m.ConcurrencyStatus().Limit != 1 {
		t.Fatalf("save=%d effective=%+v", w.Code, m.ConcurrencyStatus())
	}
}
