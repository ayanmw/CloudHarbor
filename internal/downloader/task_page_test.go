package downloader

import (
	"fmt"
	"reflect"
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

func TestTaskCandidateProgressRatioMatchesTaskState(t *testing.T) {
	for _, test := range []struct {
		name string
		item taskCandidate
		want float64
	}{
		{name: "done file", item: taskCandidate{job: &queueJob{Info: baiduapi.FileInfo{Size: 10}, Status: "done"}}, want: 1},
		{name: "pending file", item: taskCandidate{job: &queueJob{Info: baiduapi.FileInfo{Size: 10}, Status: "pending"}}, want: 0},
		{name: "done directory", item: taskCandidate{job: &queueJob{Info: baiduapi.FileInfo{Size: 10, IsDir: 1}, Status: "done"}}, want: 0},
		{name: "live transfer", item: taskCandidate{live: &Task{FileInfo: baiduapi.FileInfo{Size: 10}, Progress: 5}}, want: 0.5},
		{name: "zero size", item: taskCandidate{live: &Task{FileInfo: baiduapi.FileInfo{Size: 0}, Progress: 0}}, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.item.progressRatio(test.item.info()); got != test.want {
				t.Fatalf("progress ratio = %v, want %v", got, test.want)
			}
		})
	}
}

func BenchmarkGetTaskPageProgress(b *testing.B) {
	for _, count := range []int{10000, 100000} {
		b.Run(fmt.Sprintf("tasks_%d", count), func(b *testing.B) {
			d := NewDownloader(&config.Config{}, baiduapi.NewClient("", ""))
			defer d.cancel()
			d.queue.jobs = make(map[int64]*queueJob, count)
			d.tasks = make(map[int64]*Task, count/5)
			for id := 1; id <= count; id++ {
				size := int64(5000 + id%1024)
				info := baiduapi.FileInfo{FsID: int64(id), Path: fmt.Sprintf("/queue/file-%06d.bin", id), Size: size}
				status := "done"
				if id%5 == 0 {
					status = "downloading"
					d.tasks[int64(id)] = &Task{FileInfo: info, Status: status, Progress: int64((id * 37) % int(size))}
				}
				d.queue.jobs[int64(id)] = &queueJob{Info: info, Status: status, Mode: DownloadModeKeep}
			}
			if page := d.GetTaskPage(0, 100, "progress"); len(page.Tasks) != min(count, 100) {
				b.Fatalf("prime page returned %d tasks", len(page.Tasks))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if page := d.GetTaskPage(0, 100, "progress"); len(page.Tasks) != min(count, 100) {
					b.Fatalf("page returned %d tasks", len(page.Tasks))
				}
			}
		})
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

func TestProgressPageReusesStableIndexAcrossLiveProgressTicks(t *testing.T) {
	d := NewDownloader(&config.Config{}, baiduapi.NewClient("", ""))
	defer d.cancel()
	d.queue.jobs[1] = &queueJob{Info: baiduapi.FileInfo{FsID: 1, Path: "/queue/done-dir", Size: 100, IsDir: 1}, Status: "done"}
	d.queue.jobs[2] = &queueJob{Info: baiduapi.FileInfo{FsID: 2, Path: "/queue/live", Size: 100}, Status: "downloading"}
	d.queue.jobs[3] = &queueJob{Info: baiduapi.FileInfo{FsID: 3, Path: "/queue/failed", Size: 100}, Status: "failed"}
	d.tasks[2] = &Task{FileInfo: d.queue.jobs[2].Info, Status: "downloading"}

	page := d.GetTaskPage(0, 1, "progress")
	if len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 1 {
		t.Fatalf("initial progress page = %+v, want completed task 1", page.Tasks)
	}
	generation, stableIDs, activeIDs := progressIndexSnapshot(t, d.queue)
	if !reflect.DeepEqual(stableIDs, []int64{1, 3}) {
		t.Fatalf("stable progress IDs = %v, want [1 3]", stableIDs)
	}
	if !reflect.DeepEqual(activeIDs, []int64{2}) {
		t.Fatalf("active progress IDs = %v, want [2]", activeIDs)
	}

	d.updateProgress(2, 75)
	page = d.GetTaskPage(0, 1, "progress")
	if len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 2 {
		t.Fatalf("updated progress page = %+v, want active task 2", page.Tasks)
	}
	gotGeneration, gotStableIDs, gotActiveIDs := progressIndexSnapshot(t, d.queue)
	if gotGeneration != generation || !reflect.DeepEqual(gotStableIDs, stableIDs) || !reflect.DeepEqual(gotActiveIDs, activeIDs) {
		t.Fatalf("live progress rebuilt index: generation %d->%d stable %v->%v active %v->%v", generation, gotGeneration, stableIDs, gotStableIDs, activeIDs, gotActiveIDs)
	}
}

func TestProgressIndexInvalidatesWhenInactiveJobStatusChanges(t *testing.T) {
	d := NewDownloader(&config.Config{}, baiduapi.NewClient("", ""))
	defer d.cancel()
	d.queue.jobs[1] = &queueJob{Info: baiduapi.FileInfo{FsID: 1, Path: "/queue/failed", Size: 100}, Status: "failed"}
	d.queue.jobs[2] = &queueJob{Info: baiduapi.FileInfo{FsID: 2, Path: "/queue/done", Size: 100}, Status: "done"}

	page := d.GetTaskPage(0, 1, "progress")
	if len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 2 {
		t.Fatalf("initial progress page = %+v, want done task 2", page.Tasks)
	}
	generation, ids, activeIDs := progressIndexSnapshot(t, d.queue)
	if !reflect.DeepEqual(ids, []int64{2, 1}) {
		t.Fatalf("initial stable progress IDs = %v, want [2 1]", ids)
	}
	if len(activeIDs) != 0 {
		t.Fatalf("initial active progress IDs = %v, want none", activeIDs)
	}

	d.queue.jobs[2].Status = "failed"
	if err := d.queue.persist(d.queue.jobs[2]); err != nil {
		t.Fatal(err)
	}
	page = d.GetTaskPage(0, 1, "progress")
	if len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 1 {
		t.Fatalf("updated progress page = %+v, want lowest FsID among failed tasks", page.Tasks)
	}
	gotGeneration, gotIDs, gotActiveIDs := progressIndexSnapshot(t, d.queue)
	if gotGeneration == generation || !reflect.DeepEqual(gotIDs, []int64{1, 2}) {
		t.Fatalf("status change left stale progress index: generation %d->%d ids=%v", generation, gotGeneration, gotIDs)
	}
	if len(gotActiveIDs) != 0 {
		t.Fatalf("updated active progress IDs = %v, want none", gotActiveIDs)
	}
}

func progressIndexSnapshot(t *testing.T, q *scheduler) (uint64, []int64, []int64) {
	t.Helper()
	index := reflect.ValueOf(q).Elem().FieldByName("progressIndex")
	if !index.IsValid() || !index.FieldByName("ready").Bool() {
		t.Fatal("stable progress index is not ready")
	}
	idsField := index.FieldByName("ids")
	ids := make([]int64, idsField.Len())
	for i := range ids {
		ids[i] = idsField.Index(i).Int()
	}
	activeField := index.FieldByName("activeIDs")
	activeIDs := make([]int64, activeField.Len())
	for i := range activeIDs {
		activeIDs[i] = activeField.Index(i).Int()
	}
	return index.FieldByName("generation").Uint(), ids, activeIDs
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
			b.Run("progress_cold", func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					d.taskStatsGeneration.Add(1)
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
