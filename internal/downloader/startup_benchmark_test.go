package downloader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func BenchmarkOpenDownloaderHistory(b *testing.B) {
	for _, count := range []int{100, 1000, 10000, 100000} {
		b.Run(fmt.Sprintf("tasks_%d", count), func(b *testing.B) {
			root, queuePath := benchmarkHistoryFixture(b, count)
			b.Run("store_load", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					store, err := taskstore.Open(queuePath, "/preview")
					if err != nil {
						b.Fatal(err)
					}
					records, loadErr := store.Load()
					closeErr := store.Close()
					if loadErr != nil || closeErr != nil {
						b.Fatalf("load=%v close=%v", loadErr, closeErr)
					}
					runtime.KeepAlive(records)
				}
			})
			b.Run("open_downloader", func(b *testing.B) {
				cfg := &config.Config{LocalDownDir: root, QueueDir: "/preview"}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					d, openErr := OpenDownloader(cfg, baiduapi.NewClient("", ""))
					if openErr != nil {
						b.Fatal(openErr)
					}
					d.cancel()
					if closeErr := d.queue.store.Close(); closeErr != nil {
						b.Fatal(closeErr)
					}
				}
			})
		})
	}
}

func BenchmarkOpenDownloaderRetainedHeap(b *testing.B) {
	for _, count := range []int{10000, 100000} {
		b.Run(fmt.Sprintf("tasks_%d", count), func(b *testing.B) {
			root, _ := benchmarkHistoryFixture(b, count)
			cfg := &config.Config{LocalDownDir: root, QueueDir: "/preview"}
			var retained uint64
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				runtime.GC()
				var before runtime.MemStats
				runtime.ReadMemStats(&before)
				b.StartTimer()
				d, err := OpenDownloader(cfg, baiduapi.NewClient("", ""))
				if err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				runtime.GC()
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				runtime.KeepAlive(d)
				retained = after.HeapAlloc - before.HeapAlloc
				d.cancel()
				if err := d.queue.store.Close(); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
			b.ReportMetric(float64(retained)/1024/1024, "heap-MiB")
			b.ReportMetric(float64(retained)/float64(count), "heap-B/task")
		})
	}
}

func BenchmarkReconcileRetainedHeap(b *testing.B) {
	for _, count := range []int{10000, 100000} {
		b.Run(fmt.Sprintf("tasks_%d", count), func(b *testing.B) {
			var retained uint64
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				d := NewDownloader(&config.Config{QueueDir: "/preview"}, baiduapi.NewClient("", ""))
				d.queue.jobs = make(map[int64]*queueJob, count)
				d.queue.started = true // Prevent this memory probe from launching workers.
				files := make([]baiduapi.FileInfo, count)
				for id := 1; id <= count; id++ {
					name := fmt.Sprintf("file-%06d.bin", id)
					path := "/preview/" + name
					info := baiduapi.FileInfo{FsID: int64(id), Path: path, ServerFilename: name, Size: int64(id), Mtime: 123, MD5: "0123456789abcdef0123456789abcdef"}
					jobInfo := info
					jobInfo.Path = strings.Clone(info.Path)
					jobInfo.ServerFilename = strings.Clone(info.ServerFilename)
					jobInfo.MD5 = strings.Clone(info.MD5)
					d.queue.jobs[int64(id)] = &queueJob{Version: 1, Info: jobInfo, Mode: DownloadModeKeep, Status: "done"}
					files[id-1] = info
				}
				runtime.GC()
				b.StartTimer()
				if err := d.queue.reconcile(files, 0); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				files = nil
				runtime.GC()
				var stats runtime.MemStats
				runtime.ReadMemStats(&stats)
				runtime.KeepAlive(d)
				retained = stats.HeapAlloc
				d.cancel()
				d = nil
			}
			b.ReportMetric(float64(retained)/1024/1024, "heap-MiB")
			b.ReportMetric(float64(retained)/float64(count), "heap-B/task")
		})
	}
}

func BenchmarkStoreLoadEachValidationMode(b *testing.B) {
	_, queuePath := benchmarkHistoryFixture(b, 10000)
	for _, mode := range []string{"validated", "raw"} {
		b.Run(mode, func(b *testing.B) {
			store, err := taskstore.Open(queuePath, "/preview")
			if err != nil {
				b.Fatal(err)
			}
			defer store.Close()
			visit := func(int64, json.RawMessage) error { return nil }
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var loadErr error
				if mode == "raw" {
					loadErr = store.LoadEachRaw(visit)
				} else {
					loadErr = store.LoadEach(visit)
				}
				if loadErr != nil {
					b.Fatal(loadErr)
				}
			}
		})
	}
}

func benchmarkHistoryFixture(b *testing.B, count int) (string, string) {
	b.Helper()
	root := b.TempDir()
	queuePath := filepath.Join(root, stateDirectory, "queue")
	store, err := taskstore.Open(queuePath, "/preview")
	if err != nil {
		b.Fatal(err)
	}
	if err = store.Close(); err != nil {
		b.Fatal(err)
	}
	jobsPath := filepath.Join(queuePath, "jobs")
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for id := 1; id <= count; id++ {
		job := queueJob{
			Version: 1,
			Info:    baiduapi.FileInfo{FsID: int64(id), Path: fmt.Sprintf("/preview/file-%d.bin", id), ServerFilename: fmt.Sprintf("file-%d.bin", id), Size: 1024},
			Mode:    DownloadModeKeep, Status: "done", Created: created, Updated: created,
		}
		body, marshalErr := json.Marshal(job)
		if marshalErr != nil {
			b.Fatal(marshalErr)
		}
		if writeErr := os.WriteFile(filepath.Join(jobsPath, fmt.Sprintf("%d.json", id)), body, 0600); writeErr != nil {
			b.Fatal(writeErr)
		}
	}
	return root, queuePath
}
