package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestQueuePausePersistsAcrossRestartAndReadsLegacySettings(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	state, err := d.SetQueuePaused(true)
	if err != nil || !state.Paused || state.UpdatedAtUTC == nil {
		t.Fatalf("pause state = %+v, err=%v", state, err)
	}
	duplicate, err := d.SetQueuePaused(true)
	if err != nil || duplicate.UpdatedAtUTC == nil || !duplicate.UpdatedAtUTC.Equal(*state.UpdatedAtUTC) {
		t.Fatalf("idempotent pause changed its timestamp: first=%+v duplicate=%+v err=%v", state, duplicate, err)
	}
	settings, err := store.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]json.RawMessage
	if err := json.Unmarshal(settings, &saved); err != nil || len(saved["queue_paused"]) == 0 || len(saved["queue_pause_updated_at_utc"]) == 0 {
		t.Fatalf("pause setting was not persisted: %s err=%v", settings, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	d.queue.store = nil
	restored, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, restored)
	reloaded := restored.QueuePauseStatus()
	if !reloaded.Paused || reloaded.UpdatedAtUTC == nil || !reloaded.UpdatedAtUTC.Equal(*state.UpdatedAtUTC) {
		t.Fatalf("reloaded queue pause = %+v, want %+v", reloaded, state)
	}
}

func TestQueuePauseDoesNotCancelActiveDownload(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	openEventStore(t, d)
	ctx, cancel := context.WithCancel(context.Background())
	d.queue.mu.Lock()
	d.queue.active[999] = cancel
	d.queue.mu.Unlock()
	state, err := d.SetQueuePaused(true)
	if err != nil || state.ActiveTasks != 1 || ctx.Err() != nil {
		t.Fatalf("pause interrupted active task: state=%+v ctx=%v err=%v", state, ctx.Err(), err)
	}
	cancel()
	d.queue.mu.Lock()
	delete(d.queue.active, 999)
	d.queue.mu.Unlock()
}

func TestQueuePauseReadsLegacySettingsWithoutPauseFields(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	if err := store.SaveSettings([]byte(`{"download_mode":"keep"}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	d.queue.store = nil
	legacy, err := OpenDownloader(d.cfg, d.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, legacy)
	if got := legacy.QueuePauseStatus(); got.Paused || got.UpdatedAtUTC != nil {
		t.Fatalf("legacy settings without pause fields = %+v", got)
	}
}

func TestQueuePauseStopsClaimsButKeepsMonitoringAndDoesNotClearMaintenance(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	openEventStore(t, d)
	client := baiduapi.NewClient("token", "app")
	d.SetAuthentication(client, true, time.Now().Add(time.Hour))
	info := file(701, "queued.bin", 3)
	if err := d.queue.reconcileObserved([]baiduapi.FileInfo{info}, d.authEpoch, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SetQueuePaused(true); err != nil {
		t.Fatal(err)
	}
	if job, _, _ := d.queue.claimAt(time.Now()); job != nil {
		t.Fatalf("paused queue claimed task %+v", job)
	}
	if got := d.QueuePauseStatus(); got.ActiveTasks != 0 {
		t.Fatalf("no active downloads expected, got %+v", got)
	}

	newInfo := file(702, "arrived-while-paused.bin", 4)
	if err := d.queue.reconcileObserved([]baiduapi.FileInfo{info, newInfo}, d.authEpoch, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if job := d.queue.jobs[newInfo.FsID]; job == nil || job.Status != "pending" {
		t.Fatalf("monitoring did not keep new task pending while paused: %+v", job)
	}

	d.queue.mu.Lock()
	d.queue.maintenance = true
	d.queue.mu.Unlock()
	if _, err := d.SetQueuePaused(false); err != nil {
		t.Fatal(err)
	}
	d.queue.mu.Lock()
	maintenanceStillHeld := d.queue.maintenance
	d.queue.mu.Unlock()
	if !maintenanceStillHeld {
		t.Fatal("resume cleared the separate maintenance gate")
	}
	if job, _, _ := d.queue.claimAt(time.Now()); job != nil {
		t.Fatalf("maintenance gate allowed a claim after resume: %+v", job)
	}
	d.EndMaintenance()
	claimed, ctx, _ := d.queue.claimAt(time.Now())
	if claimed == nil || claimed.Info.FsID != info.FsID || ctx == nil {
		t.Fatalf("queue did not resume after both gates cleared: job=%+v ctx=%v", claimed, ctx)
	}
	d.queue.complete(claimed, errors.New("test cleanup"))
}

func TestQueuePausePersistenceFailureDoesNotChangeState(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	store := openEventStore(t, d)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	d.queue.store = nil
	if _, err := d.SetQueuePaused(true); err == nil {
		t.Fatal("pause without durable settings storage unexpectedly succeeded")
	}
	if got := d.QueuePauseStatus(); got.Paused || got.UpdatedAtUTC != nil {
		t.Fatalf("failed pause changed in-memory state: %+v", got)
	}
}
