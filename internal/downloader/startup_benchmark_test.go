package downloader

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func BenchmarkOpenDownloaderHistory(b *testing.B) {
	for _, count := range []int{100, 1000, 10000, 100000} {
		b.Run(fmt.Sprintf("tasks_%d", count), func(b *testing.B) {
			b.Run("store_load", func(b *testing.B) {
				_, queuePath := benchmarkHistoryFixture(b, count)
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
				root, _ := benchmarkHistoryFixture(b, count)
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

func TestBenchmarkHistoryFixtureProducesRestorableRecords(t *testing.T) {
	root, queuePath := benchmarkHistoryFixture(t, 3)
	if !filepath.IsAbs(root) {
		t.Fatalf("fixture root is not absolute: %q", root)
	}
	store, err := taskstore.Open(queuePath, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	records, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("fixture record count = %d, want 3", len(records))
	}
	for id := int64(1); id <= 3; id++ {
		var job queueJob
		body, ok := records[id]
		if !ok {
			t.Fatalf("fixture record %d is missing", id)
		}
		if err := json.Unmarshal(body, &job); err != nil {
			t.Fatalf("decode fixture job %d: %v", id, err)
		}
		if job.Info.FsID != id || job.Status != "done" || job.Mode != DownloadModeKeep {
			t.Fatalf("fixture job %d = %+v", id, job)
		}
	}
}

func TestOpenDownloaderSharesRepeatedPersistedEnumStrings(t *testing.T) {
	root, _ := benchmarkHistoryFixture(t, 2)
	d, err := OpenDownloader(&config.Config{LocalDownDir: root, QueueDir: "/preview"}, baiduapi.NewClient("", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer d.cancel()
	defer d.queue.store.Close()
	first, second := d.queue.jobs[1], d.queue.jobs[2]
	if first == nil || second == nil {
		t.Fatal("fixture jobs were not restored")
	}
	// Go 1.22 decodes each persisted enum to a separate string; canonicalization
	// must share the immutable values retained by the queue after restoration.
	if first.Info.Path != "/preview/file-1.bin" || second.Info.Path != "/preview/file-2.bin" {
		t.Fatalf("restored paths were overwritten: first=%q second=%q", first.Info.Path, second.Info.Path)
	}
	if unsafe.StringData(first.Status) != unsafe.StringData(second.Status) {
		t.Fatal("repeated persisted status strings were not shared")
	}
	if unsafe.StringData(string(first.Mode)) != unsafe.StringData(string(second.Mode)) {
		t.Fatal("repeated persisted mode strings were not shared")
	}
}

func benchmarkHistoryFixture(tb testing.TB, count int) (string, string) {
	tb.Helper()
	root := tb.TempDir()
	queuePath := filepath.Join(root, stateDirectory, "queue")
	store, err := taskstore.Open(queuePath, "/preview")
	if err != nil {
		tb.Fatal(err)
	}
	if err = store.Close(); err != nil {
		tb.Fatal(err)
	}
	jobsPath := filepath.Join(queuePath, "jobs")
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	workers := min(runtime.GOMAXPROCS(0), 16)
	if workers < 1 {
		workers = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ids := make(chan int)
	writeErr := make(chan error, 1)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case id, ok := <-ids:
					if !ok {
						return
					}
					job := queueJob{
						Version: 1,
						Info:    baiduapi.FileInfo{FsID: int64(id), Path: fmt.Sprintf("/preview/file-%d.bin", id), ServerFilename: fmt.Sprintf("file-%d.bin", id), Size: 1024},
						Mode:    DownloadModeKeep, Status: "done", Created: created, Updated: created,
					}
					body, marshalErr := json.Marshal(job)
					if marshalErr == nil {
						marshalErr = os.WriteFile(filepath.Join(jobsPath, fmt.Sprintf("%d.json", id)), body, 0600)
					}
					if marshalErr != nil {
						select {
						case writeErr <- marshalErr:
						default:
						}
						cancel()
						return
					}
				}
			}
		}()
	}
	for id := 1; id <= count; id++ {
		select {
		case ids <- id:
		case <-ctx.Done():
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(ids)
	wg.Wait()
	select {
	case err := <-writeErr:
		tb.Fatal(err)
	default:
	}
	return root, queuePath
}
