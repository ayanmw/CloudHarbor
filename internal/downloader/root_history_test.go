package downloader

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestRootDestinationSurvivesRestartWithoutInferringOldPaths(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	openEventStore(t, d)
	info := file(1601, "history.bin", 3)
	job := &queueJob{Version: 1, Info: info, Mode: DownloadModeKeep, Status: "paused", PausedFromStatus: "pending", Created: time.Now()}
	d.queue.jobs[info.FsID] = job
	local := filepath.Join(d.cfg.LocalDownDir, info.ServerFilename)
	if err := d.recordRootDestination(info, local); err != nil {
		t.Fatal(err)
	}
	first := *job.Destination
	if first.Path != local || first.RecordedAtUTC.IsZero() || first.RecordedAtUTC.Location() != time.UTC {
		t.Fatalf("snapshot=%+v", first)
	}
	if err := d.recordRootDestination(info, local); err != nil {
		t.Fatal(err)
	}
	if *job.Destination != first {
		t.Fatal("same attempt rewrote destination observation")
	}
	events, err := d.queue.store.Events(info.FsID)
	if err != nil || len(events) != 1 || events[0].Kind != "download_destination_recorded" || decodeEventEvidence(t, events[0])["path"] != local {
		t.Fatalf("destination evidence=%+v err=%v", events, err)
	}
	old := &queueJob{Version: 1, Info: file(1602, "old.bin", 3), Mode: DownloadModeKeep, Status: "paused", PausedFromStatus: "pending"}
	d.queue.jobs[old.Info.FsID] = old
	if err := d.queue.persist(old); err != nil {
		t.Fatal(err)
	}
	if err := d.queue.store.Close(); err != nil {
		t.Fatal(err)
	}
	d.queue.store = nil
	restored, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, restored)
	tasks := restored.GetTasks()
	if len(tasks) != 2 {
		t.Fatalf("restored %d roots, want 2", len(tasks))
	}
	for _, task := range tasks {
		if task.FileInfo.FsID == info.FsID && (task.LocalPath != local || task.Destination == nil || *task.Destination != first) {
			t.Fatalf("restored=%+v", task)
		}
		if task.FileInfo.FsID == old.Info.FsID && (task.LocalPath != "" || task.Destination != nil) {
			t.Fatalf("invented old destination: %+v", task)
		}
	}
}

func TestRootDestinationPersistenceFailureDoesNotPublishSnapshot(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	openEventStore(t, d)
	job := &queueJob{Version: 1, Info: file(1603, "failed.bin", 3), Mode: DownloadModeKeep, Status: "pending"}
	d.queue.jobs[job.Info.FsID] = job
	if err := d.queue.store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.recordRootDestination(job.Info, filepath.Join(d.cfg.LocalDownDir, "failed.bin")); err == nil {
		t.Fatal("closed store accepted destination")
	}
	if job.Destination != nil {
		t.Fatal("failed save exposed uncommitted destination")
	}
	// runRoot must propagate this failure before any provider/local transfer.
	if err := d.runRoot(context.Background(), job.Info, job.Mode); err == nil {
		t.Fatal("root continued after destination persistence failed")
	}
	d.queue.store = nil
}

func TestRootDestinationNewAttemptRetainsPreviousEventAndReturnsCopies(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	job := &queueJob{Version: 1, Info: file(1604, "retry.bin", 3), Mode: DownloadModeKeep, Status: "pending", AttemptID: "11111111111111111111111111111111"}
	d.queue.jobs[job.Info.FsID] = job
	firstPath := filepath.Join(d.cfg.LocalDownDir, "first", "retry.bin")
	if err := d.recordRootDestination(job.Info, firstPath); err != nil {
		t.Fatal(err)
	}
	job.AttemptID = "22222222222222222222222222222222"
	secondPath := filepath.Join(d.cfg.LocalDownDir, "second", "retry.bin")
	if err := d.recordRootDestination(job.Info, secondPath); err != nil {
		t.Fatal(err)
	}
	events, err := store.Events(job.Info.FsID)
	if err != nil || len(events) != 2 {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	if decodeEventEvidence(t, events[0])["path"] != firstPath || decodeEventEvidence(t, events[1])["path"] != secondPath || events[1].AttemptID != job.AttemptID {
		t.Fatal("lost attempt-specific target history")
	}
	task := d.GetTasks()[0]
	task.Destination.Path = "caller modification"
	if job.Destination.Path != secondPath {
		t.Fatal("display exposed mutable durable snapshot")
	}
}
