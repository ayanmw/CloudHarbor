package downloader

import (
	"fmt"
	"sort"
	"testing"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
)

func TestGetTaskPageSortsAcrossAllTasksAndReturnsGlobalCounts(t *testing.T) {
	d := NewDownloader(&config.Config{}, baiduapi.NewClient("", ""))
	defer d.cancel()
	d.tasks = map[int64]*Task{
		1: {FileInfo: baiduapi.FileInfo{FsID: 1, Path: "/queue/z.txt", ServerFilename: "z.txt", Size: 100}, Status: "done", Progress: 100},
		2: {FileInfo: baiduapi.FileInfo{FsID: 2, Path: "/queue/a.txt", ServerFilename: "a.txt", Size: 10}, Status: "downloading", Progress: 5},
		3: {FileInfo: baiduapi.FileInfo{FsID: 3, Path: "/queue/m.txt", ServerFilename: "m.txt", Size: 10}, Status: "failed"},
	}

	page := d.GetTaskPage(1, 1, "name")
	if page.Total != 3 || page.Counts["done"] != 1 || page.Counts["downloading"] != 1 || page.Counts["failed"] != 1 {
		t.Fatalf("global summary = total %d counts %+v", page.Total, page.Counts)
	}
	if len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 3 || !page.HasMore {
		t.Fatalf("name page = %+v, want only m.txt and a next page", page)
	}
	page = d.GetTaskPage(0, 2, "size")
	if len(page.Tasks) != 2 || page.Tasks[0].FileInfo.FsID != 1 || page.Tasks[1].FileInfo.FsID != 2 || !page.HasMore {
		t.Fatalf("size page = %+v", page)
	}
	page = d.GetTaskPage(0, 2, "progress")
	if len(page.Tasks) != 2 || page.Tasks[0].FileInfo.FsID != 1 || page.Tasks[1].FileInfo.FsID != 2 {
		t.Fatalf("progress page = %+v", page)
	}
	page = d.GetTaskPage(2, 1, "name")
	if len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 1 || page.HasMore {
		t.Fatalf("last name page = %+v", page)
	}
}

func TestGetTaskPageMatchesFullSortAcrossModesAndOffsets(t *testing.T) {
	d := NewDownloader(&config.Config{}, baiduapi.NewClient("", ""))
	defer d.cancel()
	d.queue.jobs = make(map[int64]*queueJob, 257)
	expected := make([]*Task, 0, 257)
	for id := int64(1); id <= 257; id++ {
		size := id % 17
		info := baiduapi.FileInfo{FsID: id, Path: "/queue/Name" + string(rune('a'+id%26)), ServerFilename: "fallback", Size: size}
		status := []string{"done", "failed", "pending"}[id%3]
		d.queue.jobs[id] = &queueJob{Info: info, Status: status, Mode: DownloadModeKeep}
		task := &Task{FileInfo: info, Status: status}
		if status == "done" {
			task.Progress = size
		}
		expected = append(expected, task)
	}

	for _, sortBy := range []string{"name", "progress", "size"} {
		sorted := append([]*Task(nil), expected...)
		sort.Slice(sorted, func(i, j int) bool { return taskComesBefore(sorted[i], sorted[j], sortBy) })
		for _, offset := range []int{0, 37, 200, 257, 400} {
			page := d.GetTaskPage(offset, 31, sortBy)
			start := offset
			if start > len(sorted) {
				start = len(sorted)
			}
			end := start + 31
			if end > len(sorted) {
				end = len(sorted)
			}
			if len(page.Tasks) != end-start || page.Total != len(sorted) || page.HasMore != (end < len(sorted)) {
				t.Fatalf("sort=%s offset=%d page metadata=%+v", sortBy, offset, page)
			}
			for i, task := range page.Tasks {
				if task.FileInfo.FsID != sorted[start+i].FileInfo.FsID {
					t.Fatalf("sort=%s offset=%d item=%d got id=%d want id=%d", sortBy, offset, i, task.FileInfo.FsID, sorted[start+i].FileInfo.FsID)
				}
			}
		}
	}
	if len(d.tasks) != 0 {
		t.Fatalf("page queries materialized %d task objects; want lazy views", len(d.tasks))
	}
}

func TestGetTaskStatsCachesSummaryAndInvalidatesOnChanges(t *testing.T) {
	d := NewDownloader(&config.Config{}, baiduapi.NewClient("", ""))
	defer d.cancel()
	d.queue.jobs[1] = &queueJob{Info: baiduapi.FileInfo{FsID: 1}, Status: "done"}

	total, counts := d.GetTaskStats()
	if total != 1 || counts["done"] != 1 || !d.queue.statsCache.valid {
		t.Fatalf("initial cached stats = total %d counts %+v cache %+v", total, counts, d.queue.statsCache)
	}
	firstGeneration := d.queue.statsCache.generation
	counts["done"] = 99
	_, counts = d.GetTaskStats()
	if counts["done"] != 1 || d.queue.statsCache.generation != firstGeneration {
		t.Fatalf("cached stats were exposed or rebuilt: counts=%+v cache=%+v", counts, d.queue.statsCache)
	}

	job := d.queue.jobs[1]
	job.Status = "failed"
	if err := d.queue.persist(job); err != nil {
		t.Fatal(err)
	}
	total, counts = d.GetTaskStats()
	if total != 1 || counts["failed"] != 1 || counts["done"] != 0 || d.queue.statsCache.generation == firstGeneration {
		t.Fatalf("persisted status change left stale stats: total=%d counts=%+v cache=%+v", total, counts, d.queue.statsCache)
	}

	d.beginTask(baiduapi.FileInfo{FsID: 2}, "")
	total, counts = d.GetTaskStats()
	if total != 2 || counts["downloading"] != 1 || counts["failed"] != 1 {
		t.Fatalf("new non-root task missing from stats: total=%d counts=%+v", total, counts)
	}
	d.updateTaskStatus(2, "done", "")
	total, counts = d.GetTaskStats()
	if total != 2 || counts["done"] != 1 || counts["failed"] != 1 || counts["downloading"] != 0 {
		t.Fatalf("non-root task status change left stale stats: total=%d counts=%+v", total, counts)
	}
}

func TestGetTaskPageSortIndexesInvalidateOnCatalogChanges(t *testing.T) {
	d := NewDownloader(&config.Config{}, baiduapi.NewClient("", ""))
	defer d.cancel()
	d.queue.jobs[1] = &queueJob{Info: baiduapi.FileInfo{FsID: 1, Path: "/queue/z.txt", ServerFilename: "z.txt", Size: 10}, Status: "done"}
	d.queue.jobs[2] = &queueJob{Info: baiduapi.FileInfo{FsID: 2, Path: "/queue/b.txt", ServerFilename: "b.txt", Size: 20}, Status: "done"}

	page := d.GetTaskPage(0, 1, "name")
	if len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 2 || !d.queue.nameIndex.ready {
		t.Fatalf("initial name index page = %+v, index=%+v", page, d.queue.nameIndex)
	}
	page = d.GetTaskPage(0, 1, "size")
	if len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 2 || !d.queue.sizeIndex.ready {
		t.Fatalf("initial size index page = %+v, index=%+v", page, d.queue.sizeIndex)
	}

	// A non-root task changes membership and the stable name/size order.
	d.beginTask(baiduapi.FileInfo{FsID: 3, Path: "/queue/a.txt", ServerFilename: "a.txt", Size: 100}, "")
	page = d.GetTaskPage(0, 1, "name")
	if page.Total != 3 || len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 3 {
		t.Fatalf("name index did not include new task: %+v", page)
	}
	page = d.GetTaskPage(0, 1, "size")
	if len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 3 {
		t.Fatalf("size index did not include new task: %+v", page)
	}

	// Reconciliation may replace metadata under an existing FsID.
	d.queue.mu.Lock()
	d.queue.started = true // Keep this fixture from launching scheduler workers.
	d.queue.mu.Unlock()
	updated := baiduapi.FileInfo{FsID: 1, Path: "/queue/0.txt", ServerFilename: "0.txt", Size: 200}
	if err := d.queue.reconcile([]baiduapi.FileInfo{updated}, 0); err != nil {
		t.Fatal(err)
	}
	page = d.GetTaskPage(0, 1, "name")
	if len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 1 || page.Tasks[0].FileInfo.Path != updated.Path {
		t.Fatalf("name index retained stale metadata: %+v", page)
	}
	page = d.GetTaskPage(0, 1, "size")
	if len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 1 || page.Tasks[0].FileInfo.Size != updated.Size {
		t.Fatalf("size index retained stale metadata: %+v", page)
	}
}

func TestGetTaskPageProgressSortUsesLatestProgressAfterStableIndexBuild(t *testing.T) {
	d := NewDownloader(&config.Config{}, baiduapi.NewClient("", ""))
	defer d.cancel()
	d.queue.jobs[1] = &queueJob{Info: baiduapi.FileInfo{FsID: 1, Path: "/queue/a", Size: 100}, Status: "downloading"}
	d.queue.jobs[2] = &queueJob{Info: baiduapi.FileInfo{FsID: 2, Path: "/queue/b", Size: 100}, Status: "downloading"}
	d.beginTask(baiduapi.FileInfo{FsID: 1, Path: "/queue/a", Size: 100}, "")
	d.beginTask(baiduapi.FileInfo{FsID: 2, Path: "/queue/b", Size: 100}, "")
	d.GetTaskPage(0, 1, "name")

	d.updateProgress(2, 75)
	page := d.GetTaskPage(0, 1, "progress")
	if len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 2 {
		t.Fatalf("progress sort ignored latest transfer progress: %+v", page)
	}
	d.updateProgress(1, 90)
	page = d.GetTaskPage(0, 1, "progress")
	if len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 1 {
		t.Fatalf("progress sort returned stale ordering: %+v", page)
	}
}

func BenchmarkTaskSnapshotsWithLargeHistory(b *testing.B) {
	d := NewDownloader(&config.Config{}, baiduapi.NewClient("", ""))
	defer d.cancel()
	d.queue.jobs = make(map[int64]*queueJob, 10000)
	for id := int64(1); id <= 10000; id++ {
		d.queue.jobs[id] = &queueJob{
			Info:   baiduapi.FileInfo{FsID: id, Path: "/queue/file.txt", ServerFilename: "file.txt", Size: 1024},
			Status: "done", Mode: DownloadModeKeep,
		}
	}
	b.Run("full", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = d.GetTasks()
		}
	})
	b.Run("page100", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = d.GetTaskPage(0, 100, "name")
		}
	})
}

func BenchmarkTaskPageIndexes(b *testing.B) {
	for _, count := range []int{10000, 100000} {
		b.Run(fmt.Sprintf("tasks_%d", count), func(b *testing.B) {
			d := NewDownloader(&config.Config{}, baiduapi.NewClient("", ""))
			defer d.cancel()
			d.queue.jobs = make(map[int64]*queueJob, count)
			for id := 1; id <= count; id++ {
				name := fmt.Sprintf("file-%06d.bin", id)
				info := baiduapi.FileInfo{FsID: int64(id), Path: "/queue/" + name, ServerFilename: name, Size: int64(id % 4096)}
				d.queue.jobs[int64(id)] = &queueJob{Info: info, Status: "done", Mode: DownloadModeKeep}
			}

			for _, sortBy := range []string{"name", "size"} {
				b.Run(sortBy+"_cold", func(b *testing.B) {
					b.ReportAllocs()
					for i := 0; i < b.N; i++ {
						d.taskCatalogGeneration.Add(1)
						d.taskStatsGeneration.Add(1)
						_ = d.GetTaskPage(0, 100, sortBy)
					}
				})
				b.Run(sortBy+"_warm", func(b *testing.B) {
					_ = d.GetTaskPage(0, 100, sortBy)
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						_ = d.GetTaskPage(0, 100, sortBy)
					}
				})
			}
			b.Run("progress_scan", func(b *testing.B) {
				_ = d.GetTaskPage(0, 100, "progress")
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_ = d.GetTaskPage(0, 100, "progress")
				}
			})
			b.Run("stats_warm", func(b *testing.B) {
				_, _ = d.GetTaskStats()
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					_, _ = d.GetTaskStats()
				}
			})
		})
	}
}
