package downloader

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestDirectoryFileResultsDistinguishPublishedFromVerifiedReuse(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	store := openEventStore(t, d)
	root := file(1900, "tree", 0)
	root.IsDir = 1
	child := file(1901, "file.bin", 3)
	child.Path = root.Path + "/file.bin"
	job := &queueJob{Version: 1, Info: root, Mode: DownloadModeKeep, Status: "pending"}
	d.queue.jobs[root.FsID] = job
	if err := d.queue.persist(job); err != nil {
		t.Fatal(err)
	}
	p.lists[root.Path] = []baiduapi.FileInfo{child}
	ctx := context.Background()
	for _, kind := range []string{"published", "local_verified"} {
		if err := d.downloadLocalFolder(ctx, root, root.ServerFilename); err != nil {
			t.Fatal(err)
		}
		m, err := store.CurrentDownloadManifest(ctx, root.FsID)
		if err != nil || m == nil {
			t.Fatalf("manifest=%+v err=%v", m, err)
		}
		result, err := store.DownloadManifestFileResult(ctx, m.ID, child.FsID)
		if err != nil || result == nil || result.Kind != kind || len(result.SHA256) != 64 || result.RecordedAtUTC.IsZero() {
			t.Fatalf("result=%+v want=%s err=%v", result, kind, err)
		}
	}
	if p.downloadLinkCalls != 1 {
		t.Fatalf("verified reuse requested another download: %d", p.downloadLinkCalls)
	}
	// Polling can copy a live task just before finish changes it to done while
	// its SQL result has already committed. Do not add these bytes twice.
	job.Status = "downloading"
	d.queue.reflect(job)
	d.tasks[child.FsID].Status = "downloading"
	d.tasks[child.FsID].Progress = child.Size
	tasks, err := d.GetTaskWorkspaceSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Root && (task.DownloadHistory.Summary.Completed != 1 || task.DownloadHistory.LiveProgress != 0 || task.DownloadHistory.LiveActive != 0) {
			t.Fatalf("committed file double counted: %+v", task.DownloadHistory)
		}
	}
}

func TestFileResultSaveFailureBlocksCloudPostprocessButKeepsPublishedFile(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	store := openEventStore(t, d)
	info := file(1902, "proof.bin", 3)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeDelete, Status: "downloading"}
	d.queue.jobs[info.FsID] = job
	if err := d.queue.persist(job); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue", "queue.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TRIGGER inject_download_result_failure BEFORE INSERT ON download_manifest_results BEGIN SELECT RAISE(ABORT,'injected download result failure'); END`); err != nil {
		t.Fatal(err)
	}
	err = d.runRoot(context.Background(), info, DownloadModeDelete)
	if err == nil || !strings.Contains(err.Error(), "injected download result failure") {
		t.Fatalf("save failure not propagated: %v", err)
	}
	if len(p.actions) != 0 {
		t.Fatalf("cloud mutation sent despite missing completion evidence: %v", p.actions)
	}
	if p.metadataCalls[info.FsID] != 0 {
		t.Fatal("entered cloud postprocess identity verification after result save failed")
	}
	body, err := os.ReadFile(filepath.Join(d.cfg.LocalDownDir, info.ServerFilename))
	if err != nil || string(body) != "abc" {
		t.Fatalf("published file lost body=%q err=%v", body, err)
	}
	m, err := store.CurrentDownloadManifest(context.Background(), info.FsID)
	if err != nil || m == nil {
		t.Fatalf("manifest=%+v err=%v", m, err)
	}
	if r, err := store.DownloadManifestFileResult(context.Background(), m.ID, info.FsID); err != nil || r != nil {
		t.Fatalf("failed result visible=%+v err=%v", r, err)
	}
	if _, err = db.Exec(`DROP TRIGGER inject_download_result_failure`); err != nil {
		t.Fatal(err)
	}
	// A subsequent attempt must verify the existing published bytes, rather
	// than redownload or pretend to know the previous publication time.
	recorder, err := d.beginDownloadManifest(context.Background(), info, info.ServerFilename)
	if err != nil {
		t.Fatal(err)
	}
	if err = recorder.publish(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = d.downloadLocalFile(recorder.bind(context.Background()), info, info.ServerFilename); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.DownloadManifestFileResult(context.Background(), recorder.id, info.FsID)
	if err != nil || recovered == nil || recovered.Kind != "local_verified" || p.downloadLinkCalls != 1 {
		t.Fatalf("recovery=%+v download calls=%d err=%v", recovered, p.downloadLinkCalls, err)
	}
}

func TestCompletionReceiptBeforePublishDoesNotCreateManifestResult(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	store := openEventStore(t, d)
	info := file(1903, "interrupted.bin", 3)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "downloading"}
	d.queue.jobs[info.FsID] = job
	if err := d.queue.persist(job); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	originalLink := linkDownloadedFile
	linkDownloadedFile = func(string, string) error { cancel(); return context.Canceled }
	t.Cleanup(func() { linkDownloadedFile = originalLink })
	if err := d.runRoot(ctx, info, DownloadModeKeep); err == nil {
		t.Fatal("interrupted publication succeeded")
	}
	key := sha256.Sum256([]byte(info.ServerFilename))
	receipt, err := readRecord(filepath.Join(d.cfg.LocalDownDir, stateDirectory, hex.EncodeToString(key[:]), "complete.json"))
	if err != nil || receipt.SHA256 == "" {
		t.Fatalf("test did not reach receipt-before-publish boundary: %+v err=%v", receipt, err)
	}
	if _, err = os.Stat(filepath.Join(d.cfg.LocalDownDir, info.ServerFilename)); !os.IsNotExist(err) {
		t.Fatalf("final unexpectedly published: %v", err)
	}
	m, err := store.CurrentDownloadManifest(context.Background(), info.FsID)
	if err != nil || m == nil {
		t.Fatalf("manifest=%+v err=%v", m, err)
	}
	if r, err := store.DownloadManifestFileResult(context.Background(), m.ID, info.FsID); err != nil || r != nil {
		t.Fatalf("receipt alone became completion=%+v err=%v", r, err)
	}
	if len(p.actions) != 0 {
		t.Fatal("cloud action after publication interruption")
	}
}
