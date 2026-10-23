package downloader

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func BenchmarkOpenDownloaderHistory(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
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
