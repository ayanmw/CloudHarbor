package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func previewFixture(t *testing.T) (*Downloader, *provider) {
	t.Helper()
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { t.Error("preview must not download content") })
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	d.queue.store = store
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	return d, p
}
func assertPreviewReadOnly(t *testing.T, d *Downloader, p *provider) {
	t.Helper()
	if len(p.actions) != 0 {
		t.Fatalf("provider writes=%v", p.actions)
	}
	operations, err := d.queue.store.LoadRemoteOperations()
	if err != nil || len(operations) != 0 {
		t.Fatalf("operation journal changed: %d %v", len(operations), err)
	}
}
func TestCloudBatchPreviewSourcesCompareExactIdentitiesWithoutWrites(t *testing.T) {
	d, p := previewFixture(t)
	a := baiduapi.FileInfo{FsID: 9007199254740993, Path: "/source/a", ServerFilename: "a", Size: 7, Mtime: 1}
	b := baiduapi.FileInfo{FsID: 92, Path: "/source/b", ServerFilename: "b", Size: 8, Mtime: 1}
	p.lists["/source"] = []baiduapi.FileInfo{a, b}
	changed := b
	changed.Size++
	result, err := d.PreviewCloudBatch(context.Background(), CloudBatchPreviewRequest{RequestID: "source-1", Phase: "sources", Operation: "move", DestinationDirectory: "/target", Expected: []baiduapi.FileInfo{a, changed}})
	if err != nil || len(result.Rows) != 2 || result.Rows[0].Status != "matched" || result.Rows[1].Status != "changed" || result.RequestID != "source-1" {
		t.Fatalf("preview=%+v err=%v", result, err)
	}
	body, _ := json.Marshal(result)
	if !strings.Contains(string(body), `"source_id":"9007199254740993"`) {
		t.Fatal(string(body))
	}
	if _, err = time.Parse(time.RFC3339Nano, result.ObservedAtUTC); err != nil {
		t.Fatal(err)
	}
	assertPreviewReadOnly(t, d, p)
}
func TestCloudBatchPreviewDestinationUsesPagedBoundaryAndUnicodeNameChecks(t *testing.T) {
	d, p := previewFixture(t)
	source := baiduapi.FileInfo{FsID: 91, Path: "/source/σ.txt", ServerFilename: "σ.txt", Size: 7, Mtime: 1}
	files := make([]baiduapi.FileInfo, 101)
	for i := range files {
		files[i] = baiduapi.FileInfo{FsID: int64(1000 + i), Path: fmt.Sprintf("/target/f%03d", i), ServerFilename: fmt.Sprintf("f%03d", i), Mtime: 1}
	}
	files[100] = baiduapi.FileInfo{FsID: 9007199254740995, Path: "/target/ς.txt", ServerFilename: "ς.txt", Size: 7, Mtime: 1}
	p.lists["/target"] = files
	request := CloudBatchPreviewRequest{RequestID: "target-1", Phase: "destination", Operation: "copy", DestinationDirectory: "/target", Expected: []baiduapi.FileInfo{source}}
	first, err := d.PreviewCloudBatch(context.Background(), request)
	if err != nil || first.CheckedCount != 100 || len(first.Matches) != 0 || first.NextOffset == nil || *first.NextOffset != 100 || len(first.NextBoundary) != 64 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	request.Offset = 100
	request.ExpectedBoundary = first.NextBoundary
	request.RequestID = "target-2"
	last, err := d.PreviewCloudBatch(context.Background(), request)
	if err != nil || last.NextOffset != nil || len(last.Matches) != 1 || last.Matches[0].ConflictID != "9007199254740995" || last.Matches[0].SourceID != "91" {
		t.Fatalf("last=%+v err=%v", last, err)
	}
	p.lists["/target"][100].Mtime++
	if _, err = d.PreviewCloudBatch(context.Background(), request); !errors.Is(err, ErrIntakeStale) {
		t.Fatalf("changed boundary=%v", err)
	}
	assertPreviewReadOnly(t, d, p)
}
func TestCloudBatchPreviewInvalidAndCancelledReadsNeverBecomeComplete(t *testing.T) {
	d, p := previewFixture(t)
	source := baiduapi.FileInfo{FsID: 91, Path: "/source/a", ServerFilename: "a", Mtime: 1}
	request := CloudBatchPreviewRequest{RequestID: "r", Phase: "sources", Operation: "move", DestinationDirectory: "/target", Expected: []baiduapi.FileInfo{source}}
	tooMany := request
	tooMany.Expected = make([]baiduapi.FileInfo, 11)
	if _, err := d.PreviewCloudBatch(context.Background(), tooMany); !errors.Is(err, ErrIntakeInvalid) {
		t.Fatal(err)
	}
	invalid := request
	invalid.Operation = "delete"
	if _, err := d.PreviewCloudBatch(context.Background(), invalid); !errors.Is(err, ErrIntakeInvalid) {
		t.Fatal(err)
	}
	if len(p.metadataCalls) != 0 || len(p.listRequests) != 0 {
		t.Fatal("invalid input reached provider")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.PreviewCloudBatch(ctx, request); err == nil {
		t.Fatal("cancelled read succeeded")
	}
	assertPreviewReadOnly(t, d, p)
}
func TestCloudBatchPreviewReportsIntraBatchDestinationNameCollisions(t *testing.T) {
	d, p := previewFixture(t)
	a := baiduapi.FileInfo{FsID: 91, Path: "/source/A", ServerFilename: "A"}
	b := baiduapi.FileInfo{FsID: 92, Path: "/other/a", ServerFilename: "a"}
	request := CloudBatchPreviewRequest{RequestID: "batch-1", Phase: "destination", Operation: "move", DestinationDirectory: "/target", Expected: []baiduapi.FileInfo{a, b}}
	result, err := d.PreviewCloudBatch(context.Background(), request)
	if err != nil || len(result.BatchConflicts) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	assertPreviewReadOnly(t, d, p)
}

func TestCloudBatchPreviewConflictOutputIsBoundedAndReadsCanFail(t *testing.T) {
	d, p := previewFixture(t)
	expected := make([]baiduapi.FileInfo, 100)
	for i := range expected {
		expected[i] = baiduapi.FileInfo{FsID: int64(1000 + i), Path: fmt.Sprintf("/source%d/A", i), ServerFilename: "A"}
	}
	p.lists["/target"] = []baiduapi.FileInfo{{FsID: 9, Path: "/target/A", ServerFilename: "A"}, {FsID: 10, Path: "/target/a", ServerFilename: "a"}}
	request := CloudBatchPreviewRequest{RequestID: "bounded", Phase: "destination", Operation: "copy", DestinationDirectory: "/target", Expected: expected}
	result, err := d.PreviewCloudBatch(context.Background(), request)
	if err != nil || len(result.Matches) != 100 || len(result.BatchConflicts) != 99 {
		t.Fatalf("matches=%d batch=%d err=%v", len(result.Matches), len(result.BatchConflicts), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := len(p.listRequests)
	if _, err = d.PreviewCloudBatch(ctx, request); err == nil || len(p.listRequests) != before {
		t.Fatalf("cancelled target read err=%v reads=%d", err, len(p.listRequests)-before)
	}
	p.listFailures["/target"] = 1
	if _, err = d.PreviewCloudBatch(context.Background(), request); err == nil {
		t.Fatal("failed target read became a complete result")
	}
	assertPreviewReadOnly(t, d, p)
}

func TestCloudBatchPreviewRejectsObservationFromAnOldAuthenticationEpoch(t *testing.T) {
	d, p := previewFixture(t)
	source := baiduapi.FileInfo{FsID: 91, Path: "/source/a", ServerFilename: "a", Size: 7, Mtime: 1}
	p.metadataOverride = func(_ int64, _ int) *baiduapi.FileInfo {
		d.SetAuthentication(d.getClient(), false, time.Time{})
		return &source
	}
	result, err := d.PreviewCloudBatch(context.Background(), CloudBatchPreviewRequest{RequestID: "old-auth", Phase: "sources", Operation: "move", DestinationDirectory: "/target", Expected: []baiduapi.FileInfo{source}})
	if !errors.Is(err, ErrAuthenticationWaiting) || len(result.Rows) != 0 {
		t.Fatalf("old-epoch result=%+v err=%v", result, err)
	}
	assertPreviewReadOnly(t, d, p)
}
