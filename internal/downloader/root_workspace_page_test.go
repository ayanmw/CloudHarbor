package downloader

import (
	"fmt"
	"math"
	"net/http"
	"testing"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func TestRootWorkspacePageBoundsCopiesAndExcludesChildren(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) { t.Error("root selection must not request cloud") })
	for id := int64(1); id <= 100000; id++ {
		info := file(id, fmt.Sprintf("root-%08d", id), 0)
		info.IsDir = 1
		d.queue.jobs[id] = &queueJob{Info: info, Status: []string{"done", "pending", "failed"}[id%3], Mode: DownloadModeKeep}
	}
	d.tasks[200001] = &Task{FileInfo: baiduapi.FileInfo{FsID: 200001, Path: "/queue/root-00000002/child"}, Status: "downloading"}
	d.tasks[200002] = &Task{FileInfo: baiduapi.FileInfo{FsID: 200002, Path: "/queue/orphan"}, Status: "failed", Root: true}
	page, err := d.GetRootWorkspacePage(3, 2, "failed", "")
	if err != nil || page.Total != 33333 || page.QueueTotal != 100000 || len(page.Tasks) != 2 || page.Tasks[0].FileInfo.FsID != 11 || page.Tasks[1].FileInfo.FsID != 14 || page.Counts["pending"] != 33334 || page.Counts["downloading"] != 0 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	for _, task := range page.Tasks {
		if !task.Root || task.DownloadHistory != nil {
			t.Fatalf("selection copied history/non-root: %+v", task)
		}
	}
	// A small first page must not allocate a Task for every queued root. Leave
	// ample room for compiler/runtime differences rather than pinning a count.
	allocs := testing.AllocsPerRun(2, func() {
		p, e := d.GetRootWorkspacePage(0, 5, "", "")
		if e != nil || len(p.Tasks) != 5 {
			t.Fatalf("allocation fixture page=%+v err=%v", p, e)
		}
	})
	if allocs > 200 {
		t.Fatalf("five rows allocated %.0f objects for 100000 roots", allocs)
	}
	page, err = d.GetRootWorkspacePage(math.MaxInt, 200, "", "")
	if err != nil || page.Total != 100000 || len(page.Tasks) != 0 {
		t.Fatalf("huge offset=%+v err=%v", page, err)
	}
	page, err = d.GetRootWorkspacePage(0, 50, "", "ROOT-00099999")
	if err != nil || page.Total != 1 || len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 99999 {
		t.Fatalf("search=%+v err=%v", page, err)
	}
	if _, err = d.GetRootWorkspacePage(0, 50, "invalid", ""); err == nil {
		t.Fatal("invalid filter accepted")
	}
}

func TestRootWorkspacePagePreservesStatusPathAndExactIDOrder(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	for _, id := range []int64{9007199254740993, 9007199254740992} {
		d.queue.jobs[id] = &queueJob{Info: file(id, "same", 0), Status: "pending", Mode: DownloadModeKeep}
	}
	d.queue.jobs[1] = &queueJob{Info: file(1, "z-active", 0), Status: "downloading", Mode: DownloadModeKeep}
	d.queue.jobs[2] = &queueJob{Info: file(2, "a-done", 0), Status: "done", Mode: DownloadModeKeep}
	page, err := d.GetRootWorkspacePage(0, 50, "", "")
	if err != nil || len(page.Tasks) != 4 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	for i, id := range []int64{1, 9007199254740992, 9007199254740993, 2} {
		if page.Tasks[i].FileInfo.FsID != id || page.Tasks[i].TaskID != fmt.Sprint(id) {
			t.Fatalf("order/identity lost at %d: %+v", i, page.Tasks[i])
		}
	}
	d.queue.jobs[3] = &queueJob{Info: file(3, "Ä课程", 0), Status: "paused", Mode: DownloadModeKeep}
	page, err = d.GetRootWorkspacePage(0, 50, "", "ä课程")
	if err != nil || page.Total != 1 || len(page.Tasks) != 1 || page.Tasks[0].FileInfo.FsID != 3 {
		t.Fatalf("Unicode root filter=%+v err=%v", page, err)
	}
}
