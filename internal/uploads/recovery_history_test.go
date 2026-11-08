package uploads

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestRecoveryHistoryIsReadOnlyAndBatchScoped(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	first, err := m.Create("/target", []Input{{Path: "first", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Create("/other", []Input{{Path: "other", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []Item{first.Items[0], second.Items[0]} {
		if err = m.update(item.ID, "uploading", 2, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err = recoverInterruptedUploads(m.db); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/uploads/"+first.ID+"/recovery?limit=1", nil)
	request.SetPathValue("batch", first.ID)
	response := httptest.NewRecorder()
	m.HandleRecovery(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var body struct {
		Records []struct {
			ItemID   string `json:"item_id"`
			Path     string `json:"path"`
			Previous string `json:"previous_state"`
			State    string `json:"state"`
			Bytes    string `json:"confirmed_part_bytes"`
		} `json:"records"`
		Next *string `json:"next_before_sequence"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Records) != 1 || body.Records[0].ItemID != first.Items[0].ID || body.Records[0].Path != "first" || body.Records[0].Previous != "uploading" || body.Records[0].State != "queued" || body.Records[0].Bytes != "2" || body.Next != nil {
		t.Fatal(response.Body.String())
	}
	before, _ := m.Get(first.ID)
	post := httptest.NewRequest(http.MethodPost, request.URL.String(), strings.NewReader("{}"))
	post.SetPathValue("batch", first.ID)
	denied := httptest.NewRecorder()
	m.HandleRecovery(denied, post)
	after, _ := m.Get(first.ID)
	if denied.Code != 405 || before.Items[0].State != after.Items[0].State {
		t.Fatal("history mutated task")
	}
}

func TestRecoveryHistoryCursorIsExactAndStable(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.db.Exec(`UPDATE sqlite_sequence SET seq=9007199254740992 WHERE name='events'`)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		t.Fatal(err)
	}
	if changed == 0 {
		if _, err = m.db.Exec(`INSERT INTO sqlite_sequence(name,seq) VALUES('events',9007199254740992)`); err != nil {
			t.Fatal(err)
		}
	}
	recoverOne := func() {
		t.Helper()
		if err := m.update(b.Items[0].ID, "uploading", 2, ""); err != nil {
			t.Fatal(err)
		}
		if err := recoverInterruptedUploads(m.db); err != nil {
			t.Fatal(err)
		}
	}
	recoverOne()
	recoverOne()
	page, next, err := m.recoveryEvents(b.ID, 0, 1)
	if err != nil || len(page) != 1 || next == nil {
		t.Fatalf("page %+v %v %v", page, next, err)
	}
	before, err := strconv.ParseInt(*next, 10, 64)
	if err != nil || before <= 9007199254740992 {
		t.Fatal("large sequence lost precision")
	}
	recoverOne()
	older, last, err := m.recoveryEvents(b.ID, before, 1)
	if err != nil || len(older) != 1 || last != nil || older[0].Sequence >= page[0].Sequence {
		t.Fatalf("unstable older page %+v %v %v", older, last, err)
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=1&limit=2", "?before_sequence=0", "?before_sequence=01", "?before_sequence=9223372036854775808", "?unknown=1"} {
		r := httptest.NewRequest("GET", "/recovery"+query, nil)
		r.SetPathValue("batch", b.ID)
		w := httptest.NewRecorder()
		m.HandleRecovery(w, r)
		if w.Code != 400 {
			t.Fatalf("query %s: %d", query, w.Code)
		}
	}
}

func TestRecoveryHistoryEmptyAndCorruptEvidenceAreDistinct(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	get := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/recovery", nil)
		r.SetPathValue("batch", b.ID)
		w := httptest.NewRecorder()
		m.HandleRecovery(w, r)
		return w
	}
	empty := get()
	if empty.Code != 200 || !strings.Contains(empty.Body.String(), `"records":[]`) {
		t.Fatal(empty.Code, empty.Body.String())
	}
	if _, err = m.db.Exec(`INSERT INTO events(item,state,sent,recorded,error) VALUES(?,'queued',0,?,'restart_recovery: malformed')`, b.Items[0].ID, now()); err != nil {
		t.Fatal(err)
	}
	corrupt := get()
	if corrupt.Code != 500 {
		t.Fatalf("corrupt history presented as empty: %d %s", corrupt.Code, corrupt.Body.String())
	}
}
