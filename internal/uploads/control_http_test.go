package uploads

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUploadControlHTTPScopesItemsAndNeverResumesUnknown(t *testing.T) {
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
	if err = m.Receive(context.Background(), b.ID, item, strings.NewReader("abc")); err != nil {
		t.Fatal(err)
	}
	call := func(method, batch, action string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/uploads/"+batch+"/"+item+"/"+action, nil)
		r.SetPathValue("batch", batch)
		r.SetPathValue("item", item)
		w := httptest.NewRecorder()
		m.HandleControl(w, r)
		return w
	}
	if w := call("POST", strings.Repeat("f", 32), "pause"); w.Code != 404 {
		t.Fatal("cross-batch control accepted", w.Code)
	}
	if w := call("GET", b.ID, "pause"); w.Code != 405 {
		t.Fatal(w.Code)
	}
	if w := call("POST", b.ID, "pause"); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"paused"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("POST", b.ID, "resume"); w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"queued"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if err = m.update(item, "unknown", 0, "fixture unconfirmed submission"); err != nil {
		t.Fatal(err)
	}
	if w := call("POST", b.ID, "resume"); w.Code != 409 {
		t.Fatal("unknown resume accepted", w.Code)
	}
}
