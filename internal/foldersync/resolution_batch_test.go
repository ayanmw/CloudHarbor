package foldersync

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolutionBatchIsFixedBoundedAndImmutable(t *testing.T) {
	root := t.TempDir()
	dir := t.TempDir()
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("left"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("right")))
	remote := &fixtureRemote{entries: []Entry{{Path: "a", ID: "ra", Size: 5, SHA256: digest}, {Path: "b", ID: "rb", Size: 5, SHA256: digest}}}
	m, err := Open(dir, remote, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	c, err := m.Create(context.Background(), Config{Name: "batch", Mode: "both", LocalRoot: root, RemoteRoot: "/fixture"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := m.Start(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitRun(t, m, c.ID, run.ID); got.Status != "conflict" {
		t.Fatal(got)
	}
	one, err := m.CreateResolution(context.Background(), run.ID, 1, "keep_remote")
	if err != nil {
		t.Fatal(err)
	}
	two, err := m.CreateResolution(context.Background(), run.ID, 2, "keep_both")
	if err != nil {
		t.Fatal(err)
	}
	batch, err := m.CreateResolutionBatch(context.Background(), run.ID, []string{two.ID, one.ID})
	if err != nil {
		t.Fatal(err)
	}
	if batch.State != "preview" || len(batch.Items) != 2 || batch.Items[0].DecisionID != two.ID || batch.Items[1].DecisionID != one.ID || batch.StepCount != "4" || batch.Created.IsZero() {
		t.Fatalf("batch %+v", batch)
	}
	for _, ids := range [][]string{nil, {one.ID, one.ID}, make([]string, 21)} {
		if _, e := m.CreateResolutionBatch(context.Background(), run.ID, ids); e == nil {
			t.Fatal("invalid selection accepted", ids)
		}
	}
	alternate, e := m.CreateResolution(context.Background(), run.ID, 1, "keep_local")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.CreateResolutionBatch(context.Background(), run.ID, []string{one.ID, alternate.ID}); e == nil {
		t.Fatal("overlapping choices accepted")
	}
	if _, e = m.db.Exec(`UPDATE sync_resolution_batches SET body='{}' WHERE id=?`, batch.ID); e == nil {
		t.Fatal("mutable batch")
	}
	if _, e = m.db.Exec(`DELETE FROM sync_resolution_batches WHERE id=?`, batch.ID); e == nil {
		t.Fatal("deletable batch")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/runs/{run}/batches", m.HandleResolutionBatch)
	mux.HandleFunc("/batches/{batch}", m.HandleResolutionBatch)
	for _, body := range []string{`{"decision_ids":["` + one.ID + `"],"execute":true}`, `{"decision_ids":["` + one.ID + `"]} {}`, `{"decision_ids":[]}`} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("POST", "/runs/"+run.ID+"/batches", strings.NewReader(body)))
		if response.Code != 400 {
			t.Fatal("invalid API body accepted", response.Code, body)
		}
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("GET", "/batches/"+batch.ID, nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), batch.ID) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("POST", "/runs/"+run.ID+"/batches", strings.NewReader(`{"decision_ids":["`+one.ID+`","`+two.ID+`"]}`)))
	var apiBatch ResolutionBatch
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &apiBatch) != nil || len(apiBatch.Items) != 2 || apiBatch.Items[0].DecisionID != one.ID {
		t.Fatal("create API", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest("GET", "/runs/"+run.ID+"/batches?offset=0", nil))
	var history struct {
		Source  string            `json:"source_run"`
		Batches []ResolutionBatch `json:"batches"`
		More    bool              `json:"has_more"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &history) != nil || history.Source != run.ID || len(history.Batches) != 2 || history.More {
		t.Fatal("history API", response.Body.String())
	}
	for _, query := range []string{"offset=-1", "offset=bad", "offset=0&offset=1"} {
		response = httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest("GET", "/runs/"+run.ID+"/batches?"+query, nil))
		if response.Code != 400 {
			t.Fatal("invalid page", query, response.Code)
		}
	}
	var configBody string
	if e := m.db.QueryRow(`SELECT body FROM sync_configs WHERE id=?`, c.ID).Scan(&configBody); e != nil {
		t.Fatal(e)
	}
	changed := c
	changed.Name = "changed"
	if _, e := m.db.Exec(`UPDATE sync_configs SET body=? WHERE id=?`, encoded(changed), c.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := m.CreateResolutionBatch(context.Background(), run.ID, []string{one.ID}); e == nil {
		t.Fatal("changed configuration accepted")
	}
	if _, e := m.db.Exec(`UPDATE sync_configs SET body=? WHERE id=?`, configBody, c.ID); e != nil {
		t.Fatal(e)
	}
	otherRun, e := m.Start(c.ID)
	if e != nil {
		t.Fatal(e)
	}
	_ = waitRun(t, m, c.ID, otherRun.ID)
	other, e := m.CreateResolution(context.Background(), otherRun.ID, 1, "keep_remote")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.CreateResolutionBatch(context.Background(), run.ID, []string{one.ID, other.ID}); e == nil {
		t.Fatal("mixed source run accepted")
	}
	if _, e = m.db.Exec(`INSERT INTO sync_resolution_executions VALUES(?,?)`, one.ID, newID()); e != nil {
		t.Fatal(e)
	}
	if _, e = m.CreateResolutionBatch(context.Background(), run.ID, []string{one.ID}); e == nil {
		t.Fatal("admitted decision accepted as new selection")
	}
	if remote.calls != 0 {
		t.Fatal("preview performed provider mutation")
	}
	if e = m.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := Open(dir, remote, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	stored, e := reopened.ResolutionBatch(context.Background(), batch.ID)
	if e != nil || encoded(stored) != encoded(batch) {
		t.Fatal("reopen changed selection", stored, e)
	}
}
