package downloader

import (
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
	d.tasks = make(map[int64]*Task, 257)
	for id := int64(1); id <= 257; id++ {
		size := id % 17
		progress := (id * 13) % 23
		d.tasks[id] = &Task{
			FileInfo: baiduapi.FileInfo{FsID: id, Path: "/queue/Name" + string(rune('a'+id%26)), ServerFilename: "fallback", Size: size},
			Status:   []string{"done", "failed", "pending"}[id%3],
			Progress: progress,
		}
	}

	for _, sortBy := range []string{"name", "progress", "size"} {
		expected := make([]*Task, 0, len(d.tasks))
		for _, task := range d.tasks {
			expected = append(expected, task)
		}
		sort.Slice(expected, func(i, j int) bool { return taskComesBefore(expected[i], expected[j], sortBy) })
		for _, offset := range []int{0, 37, 200, 257, 400} {
			page := d.GetTaskPage(offset, 31, sortBy)
			start := offset
			if start > len(expected) {
				start = len(expected)
			}
			end := start + 31
			if end > len(expected) {
				end = len(expected)
			}
			if len(page.Tasks) != end-start || page.Total != len(expected) || page.HasMore != (end < len(expected)) {
				t.Fatalf("sort=%s offset=%d page metadata=%+v", sortBy, offset, page)
			}
			for i, task := range page.Tasks {
				if task.FileInfo.FsID != expected[start+i].FileInfo.FsID {
					t.Fatalf("sort=%s offset=%d item=%d got id=%d want id=%d", sortBy, offset, i, task.FileInfo.FsID, expected[start+i].FileInfo.FsID)
				}
			}
		}
	}
}

func BenchmarkTaskSnapshotsWithLargeHistory(b *testing.B) {
	d := NewDownloader(&config.Config{}, baiduapi.NewClient("", ""))
	defer d.cancel()
	d.tasks = make(map[int64]*Task, 10000)
	for id := int64(1); id <= 10000; id++ {
		d.tasks[id] = &Task{FileInfo: baiduapi.FileInfo{FsID: id, Path: "/queue/file.txt", Size: 1024}, Status: "done", Progress: 1024}
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
