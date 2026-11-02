package downloader

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestDirectoryPreparationMakesUnstartedFilesVisibleAsPending(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { t.Error("preparation must not start file transfers") })
	root := file(1, "tree", 0)
	root.IsDir = 1
	f := file(2, "later.bin", 50)
	f.Path = root.Path + "/later.bin"
	p.lists[root.Path] = []baiduapi.FileInfo{f}
	if _, err := d.prepareDirectoryTree(context.Background(), root, "tree"); err != nil {
		t.Fatal(err)
	}
	task := d.GetTaskByFsID(2)
	if task == nil || task.Status != "pending" || task.Progress != 0 || task.LocalPath == "" {
		t.Fatalf("unstarted file=%+v", task)
	}
	if len(p.actions) != 0 || p.downloadLinkCalls != 0 {
		t.Fatal("preparation performed provider writes or requested download links")
	}
}

func TestDirectoryTreeExistsBeforeFirstFileTransferAndListsAreReused(t *testing.T) {
	var local string
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		for _, relative := range []string{"tree", "tree/sub", "tree/sub/empty"} {
			if info, err := os.Stat(filepath.Join(local, relative)); err != nil || !info.IsDir() {
				t.Errorf("directory not prepared before file body: %s err=%v", relative, err)
			}
		}
		io.WriteString(w, "abc")
	})
	local = d.cfg.LocalDownDir
	root := file(1, "tree", 0)
	root.IsDir = 1
	sub := file(2, "sub", 0)
	sub.Path = root.Path + "/sub"
	sub.IsDir = 1
	empty := file(3, "empty", 0)
	empty.Path = sub.Path + "/empty"
	empty.IsDir = 1
	f := file(4, "data.txt", 3)
	f.Path = root.Path + "/data.txt"
	p.lists[root.Path] = []baiduapi.FileInfo{f, sub}
	p.lists[sub.Path] = []baiduapi.FileInfo{empty}
	if err := d.downloadLocalFolder(context.Background(), root, "tree"); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.listRequests) != 3 {
		t.Fatalf("directory pages were listed again during transfer: %v", p.listRequests)
	}
}
