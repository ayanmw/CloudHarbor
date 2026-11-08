package downloader

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestTransferStatisticsCountNetworkBytesButNotLocalReuse(t *testing.T) {
	base, provider := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	d, err := OpenDownloader(base.cfg, base.getClient())
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(t, d)
	info := file(902, "received.bin", 3)
	provider.lists["/queue"] = []baiduapi.FileInfo{info}
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, d, info.FsID, "done")
	stats := d.GetTasks()[0].TransferStats
	if stats == nil || stats.NetworkBytes != 3 || stats.ActiveNanos <= 0 || stats.RecordedAtUTC.IsZero() {
		t.Fatalf("network transfer not recorded: %+v", stats)
	}
	if err = d.downloadLocalFile(d.ctx, info, info.ServerFilename); err != nil {
		t.Fatal(err)
	}
	after := d.GetTasks()[0].TransferStats
	if after.NetworkBytes != stats.NetworkBytes || after.ActiveNanos != stats.ActiveNanos {
		t.Fatalf("local reuse counted as transfer: before=%+v after=%+v", stats, after)
	}
}

func TestTransferStatisticsUnionExcludesIdleAndRestart(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	openEventStore(t, d)
	job := &queueJob{Version: 1, Info: file(901, "stats.bin", 1000), Mode: DownloadModeKeep, Status: "pending", Created: time.Now()}
	d.queue.jobs[901] = job
	start := time.Now()
	for _, s := range []struct{ second, bytes, active int64 }{{0, 0, 1}, {2, 200, 1}, {4, 200, -1}, {6, 200, -1}, {106, 0, 1}, {108, 200, -1}} {
		if err := d.queue.recordTransferStats(901, s.bytes, int(s.active), start.Add(time.Duration(s.second)*time.Second), true); err != nil {
			t.Fatal(err)
		}
	}
	if job.TransferStats.ActiveNanos != int64(8*time.Second) || job.TransferStats.NetworkBytes != 800 {
		t.Fatalf("union stats: %+v", job.TransferStats)
	}
	// Simulate losing the process with a transfer open: never persist its clock.
	if err := d.queue.recordTransferStats(901, 0, 1, start.Add(110*time.Second), true); err != nil {
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
	for _, s := range []struct{ second, bytes, active int64 }{{10000, 0, 1}, {10002, 200, -1}} {
		if err := restored.queue.recordTransferStats(901, s.bytes, int(s.active), start.Add(time.Duration(s.second)*time.Second), true); err != nil {
			t.Fatal(err)
		}
	}
	stats := restored.GetTasks()[0].TransferStats
	if stats == nil || stats.ActiveNanos != int64(10*time.Second) || stats.NetworkBytes != 1000 {
		t.Fatalf("restart counted offline time: %+v", stats)
	}
}

func TestRecoveredActiveTransferMarksExistingMeasurementsPartialWithoutChangingCounts(t *testing.T) {
	for _, status := range []string{"downloading", "pending", "done"} {
		t.Run(status, func(t *testing.T) {
			d, _ := setup(t, func(http.ResponseWriter, *http.Request) { t.Error("recovery must not request the provider") })
			store := openEventStore(t, d)
			measured := &TransferStatistics{ActiveNanos: int64(2 * time.Second), NetworkBytes: 100, RecordedAtUTC: time.Now().UTC()}
			job := &queueJob{Version: 1, Info: file(19101, "measured.bin", 1000), Mode: DownloadModeKeep, Status: status, Attempts: 1, TransferStats: measured, Created: time.Now().UTC()}
			d.queue.jobs[job.Info.FsID] = job
			if err := d.queue.persist(job); err != nil {
				t.Fatal(err)
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
			got := restored.queue.jobs[job.Info.FsID].TransferStats
			if got == nil || got.ActiveNanos != measured.ActiveNanos || got.NetworkBytes != measured.NetworkBytes || !got.RecordedAtUTC.Equal(measured.RecordedAtUTC) {
				t.Fatalf("recovery invented measurements: %+v", got)
			}
			if got.Partial != (status == "downloading") {
				t.Fatalf("interrupted measurements not distinguished from inactive history: status=%s statistics=%+v", status, got)
			}
			if status == "downloading" {
				events, e := restored.queue.store.Events(job.Info.FsID)
				if e != nil {
					t.Fatal(e)
				}
				found := false
				for _, event := range events {
					if event.Kind == "startup_recovery" {
						var evidence struct {
							Incomplete bool `json:"transfer_statistics_may_be_incomplete"`
						}
						if json.Unmarshal(event.Evidence, &evidence) != nil || !evidence.Incomplete {
							t.Fatal("recovery has no incomplete-statistics evidence")
						}
						found = true
					}
				}
				if !found {
					t.Fatal("recovery event missing")
				}
			}
			shutdown(t, restored)
			again, e := OpenDownloader(d.cfg, d.getClient())
			if e != nil {
				t.Fatal(e)
			}
			defer shutdown(t, again)
			persisted := again.queue.jobs[job.Info.FsID].TransferStats
			if *persisted != *got {
				t.Fatalf("statistic coverage changed on second reopen: %+v -> %+v", got, persisted)
			}
		})
	}
}
