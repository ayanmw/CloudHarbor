package downloader

import (
	"context"
	"net/http"
	"testing"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestRootDownloadHistoryRestoresSummaryWithoutRehydratingChildren(t *testing.T) {
	d, p := setup(t, func(http.ResponseWriter, *http.Request) { t.Error("history must not transfer") })
	openEventStore(t, d)
	root := file(2000, "history", 0)
	root.IsDir = 1
	job := &queueJob{Version: 1, Info: root, Mode: DownloadModeKeep, Status: "paused", PausedFromStatus: "pending", AttemptID: "77777777777777777777777777777777"}
	d.queue.jobs[root.FsID] = job
	if err := d.queue.persist(job); err != nil {
		t.Fatal(err)
	}
	child := file(2001, "a", 123)
	child.Path = root.Path + "/a"
	p.lists[root.Path] = []baiduapi.FileInfo{child}
	ctx := context.Background()
	before, err := d.GetRootDownloadHistory(ctx, root.FsID)
	if err != nil || before.Source != "unrecorded" {
		t.Fatalf("old history=%+v err=%v", before, err)
	}
	if _, err = d.prepareDirectoryTree(ctx, root, root.ServerFilename); err != nil {
		t.Fatal(err)
	}
	if err = d.queue.store.Close(); err != nil {
		t.Fatal(err)
	}
	d.queue.store = nil
	restored, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, restored)
	tasks, err := restored.GetTaskWorkspaceSnapshot(ctx)
	if err != nil || len(tasks) != 1 || tasks[0].DownloadHistory == nil || tasks[0].DownloadHistory.Source != "current_attempt" || tasks[0].DownloadHistory.Summary.Files != 1 || tasks[0].DownloadHistory.Summary.Size != 123 {
		t.Fatalf("recovered tasks=%+v err=%v", tasks, err)
	}
	if len(restored.tasks) != 0 {
		t.Fatalf("historical children loaded into live task map: %d", len(restored.tasks))
	}
	page, available, err := restored.GetDownloadHistoryFiles(ctx, tasks[0], "pending", "a", 0, 1)
	if err != nil || !available || page.Total != 1 || len(page.Tasks) != 1 || page.Tasks[0].Status != "pending" || page.Tasks[0].FileEvidence == nil || page.Tasks[0].FileEvidence.Historical || page.Tasks[0].LocalPath != "" {
		t.Fatalf("recovered detail=%+v err=%v", page, err)
	}
	if len(restored.tasks) != 0 {
		t.Fatal("file paging rehydrated history into live map")
	}
	restored.queue.jobs[root.FsID].AttemptID = "88888888888888888888888888888888"
	changed, err := restored.GetRootDownloadHistory(ctx, root.FsID)
	if err != nil || changed.Source != "previous_attempt" {
		t.Fatalf("old attempt presented current=%+v err=%v", changed, err)
	}
	previous, err := restored.GetTaskWorkspaceSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	page, available, err = restored.GetDownloadHistoryFiles(ctx, previous[0], "", "", 0, 50)
	if err != nil || !available || len(page.Tasks) != 1 || !page.Tasks[0].FileEvidence.Historical || page.Tasks[0].LocalPath != "" {
		t.Fatalf("historical page guessed current path=%+v err=%v", page, err)
	}
	restored.queue.jobs[root.FsID].AttemptID = job.AttemptID
	restored.queue.jobs[root.FsID].Info.Mtime++
	changed, err = restored.GetRootDownloadHistory(ctx, root.FsID)
	if err != nil || changed.Source != "previous_attempt" {
		t.Fatalf("changed identity presented current=%+v err=%v", changed, err)
	}
}
