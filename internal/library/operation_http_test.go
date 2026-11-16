package library

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteHTTPStreamingHistoryAndValidation(t *testing.T) {
	m, _, c, parent := writeFixture(t)
	q := url.Values{"id": {opID('a')}, "root_revision": {c.Revision}, "target": {"网页.txt"}, "parent_identity": {parent}, "size": {"3"}}
	w := httptest.NewRecorder()
	m.Handle(w, httptest.NewRequest("POST", "/api/library/upload?"+q.Encode(), strings.NewReader("abc")))
	if w.Code != 201 {
		t.Fatalf("upload %d %s", w.Code, w.Body)
	}

	q.Set("id", opID('f'))
	conflict := httptest.NewRecorder()
	m.Handle(conflict, httptest.NewRequest("POST", "/api/library/upload?"+q.Encode(), strings.NewReader("abc")))
	if conflict.Code != 409 || !strings.Contains(conflict.Body.String(), "同名项目不会覆盖") {
		t.Fatalf("conflict feedback %d %s", conflict.Code, conflict.Body)
	}
	q.Set("id", opID('a'))
	var op Operation
	if e := json.Unmarshal(w.Body.Bytes(), &op); e != nil || op.Status != "succeeded" {
		t.Fatal(e, op)
	}
	w = httptest.NewRecorder()
	m.Handle(w, httptest.NewRequest("GET", "/api/library/operations?id="+op.ID, nil))
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(op.ID)) {
		t.Fatal("lookup", w.Code)
	}
	w = httptest.NewRecorder()
	m.Handle(w, httptest.NewRequest("GET", "/api/library/operations?offset=0&limit=20", nil))
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"items"`)) {
		t.Fatal("history", w.Code)
	}
	for _, suffix := range []string{"&size=3", "&extra=x"} {
		w = httptest.NewRecorder()
		m.Handle(w, httptest.NewRequest("POST", "/api/library/upload?"+q.Encode()+suffix, strings.NewReader("abc")))
		if w.Code != 400 {
			t.Fatal("duplicate/unknown query", w.Code)
		}
	}
	w = httptest.NewRecorder()
	m.Handle(w, httptest.NewRequest("POST", "/api/library/operations", strings.NewReader(`{"id":"`+opID('b')+`","kind":"mkdir","unknown":1}`)))
	if w.Code != 400 {
		t.Fatal("unknown JSON field", w.Code)
	}
	w = httptest.NewRecorder()
	m.Handle(w, httptest.NewRequest("PUT", "/api/library/operations", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatal("method", w.Code)
	}
}
func TestUploadCancellationNeverPublishesAndCleansOwnedStage(t *testing.T) {
	m, root, c, parent := writeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelingUpload{cancel: cancel}
	req := WriteRequest{ID: opID('a'), Kind: "upload", RootRevision: c.Revision, Target: "cancel.bin", ParentID: parent, Size: 6}
	op, e := m.Write(ctx, req, reader)
	if e == nil || op.Status != "failed" {
		t.Fatal("cancel", op, e)
	}

	files, e := os.ReadDir(filepath.Join(root, privateArea, "staging"))
	if e != nil || len(files) != 0 {
		t.Fatalf("owned failed stage retained: %v %v", files, e)
	}
}

type cancelingUpload struct {
	cancel context.CancelFunc
	done   bool
}

func (r *cancelingUpload) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		copy(p, "abc")
		r.cancel()
		return 3, nil
	}
	return 0, io.EOF
}
