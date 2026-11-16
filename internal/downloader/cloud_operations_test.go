package downloader

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func TestCloudOperationHistoryReadsStoreAcrossTokenRotationWithoutProviderCalls(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	defer store.Close()
	op := taskstore.RemoteOperation{ID: strings.Repeat("f", 32), AccountFingerprint: "previous-access-token", Kind: "rename", ObjectID: 88, SourcePath: "/before", DestinationPath: "/after", Status: "prepared", CreatedAtUTC: time.Date(2026, 10, 30, 3, 0, 0, 0, time.UTC), Evidence: json.RawMessage(`{"expected":{"path":"/before"}}`)}
	if err = store.CreateRemoteOperation(op); err != nil {
		t.Fatal(err)
	}
	if err = store.UpdateRemoteOperation(op.ID, "confirmed", json.RawMessage(`{"provider_response":"synchronous_success"}`), op.CreatedAtUTC.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	page, total, err := d.CloudOperationPage(context.Background(), taskstore.RemoteOperationFilter{Status: "confirmed", Query: "before"}, 0, 20)
	if err != nil || total != 1 || len(page) != 1 || page[0].ID != op.ID || page[0].Status != "confirmed" {
		t.Fatalf("operation page=%+v total=%d err=%v", page, total, err)
	}
	cursorPage, cursorTotal, next, err := d.CloudOperationCursorPage(context.Background(), taskstore.RemoteOperationFilter{Status: "confirmed", Query: "before"}, nil, 20)
	if err != nil || cursorTotal != 1 || len(cursorPage) != 1 || cursorPage[0].ID != op.ID || next != nil {
		t.Fatalf("cursor operation page=%+v total=%d next=%+v err=%v", cursorPage, cursorTotal, next, err)
	}
	exported, err := d.CloudOperationExport(context.Background(), taskstore.RemoteOperationFilter{Query: "before"})
	if err != nil || len(exported) != 1 || exported[0].ID != op.ID {
		t.Fatalf("operation export=%+v err=%v", exported, err)
	}
	events, _, more, err := d.CloudOperationEventPage(context.Background(), op.ID, taskstore.RemoteOperationEventFilter{}, 0, 20)
	if err != nil || more || len(events) != 2 || events[0].Status != "confirmed" {
		t.Fatalf("operation events=%+v more=%v err=%v", events, more, err)
	}
	evidence, err := d.CloudOperationEvidenceExport(context.Background(), op.ID)
	if err != nil || evidence.Operation.ID != op.ID || len(evidence.Events) != 2 || evidence.Events[0].Status != "prepared" || evidence.Events[1].Status != "confirmed" {
		t.Fatalf("operation evidence export=%+v err=%v", evidence, err)
	}
	if len(p.listRequests) != 0 || len(p.actions) != 0 {
		t.Fatalf("local history read called provider: lists=%v mutations=%v", p.listRequests, p.actions)
	}
}

func TestCloudOperationHistoryFailsWhenStoreIsUnavailable(t *testing.T) {
	d, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("payload")) })
	if _, _, err := d.CloudOperationPage(context.Background(), taskstore.RemoteOperationFilter{}, 0, 20); err == nil {
		t.Fatal("history read succeeded without a task store")
	}
	if _, _, _, err := d.CloudOperationCursorPage(context.Background(), taskstore.RemoteOperationFilter{}, nil, 20); err == nil {
		t.Fatal("cursor history read succeeded without a task store")
	}
	if _, err := d.CloudOperationExport(context.Background(), taskstore.RemoteOperationFilter{}); err == nil {
		t.Fatal("history export succeeded without a task store")
	}
	if _, err := d.CloudOperationEvidenceExport(context.Background(), strings.Repeat("a", 32)); err == nil {
		t.Fatal("evidence export succeeded without a task store")
	}
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = store
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err = d.CloudOperationPage(context.Background(), taskstore.RemoteOperationFilter{}, 0, 20); err == nil {
		t.Fatal("history read succeeded with a closed task store")
	}
	if _, _, _, err = d.CloudOperationCursorPage(context.Background(), taskstore.RemoteOperationFilter{}, nil, 20); err == nil {
		t.Fatal("cursor history read succeeded with a closed task store")
	}
	if _, err = d.CloudOperationExport(context.Background(), taskstore.RemoteOperationFilter{}); err == nil {
		t.Fatal("history export succeeded with a closed task store")
	}
	if _, err = d.CloudOperationEvidenceExport(context.Background(), strings.Repeat("a", 32)); err == nil {
		t.Fatal("evidence export succeeded with a closed task store")
	}
}
