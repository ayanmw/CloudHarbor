package downloader

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func TestDirectoryPreparationPublishesDurableManifestAndKeepsOldOnFailure(t *testing.T) {
	d, p := setup(t, func(http.ResponseWriter, *http.Request) { t.Error("preparation downloaded a file") })
	store := openEventStore(t, d)
	root := file(1701, "tree", 0)
	root.IsDir = 1
	job := &queueJob{Version: 1, Info: root, Mode: DownloadModeKeep, Status: "pending"}
	d.queue.jobs[root.FsID] = job
	if err := d.queue.persist(job); err != nil {
		t.Fatal(err)
	}
	child := file(1702, "文件.bin", 123)
	child.Path = root.Path + "/文件.bin"
	p.lists[root.Path] = []baiduapi.FileInfo{child}
	ctx := context.Background()
	if _, err := d.prepareDirectoryTree(ctx, root, root.ServerFilename); err != nil {
		t.Fatal(err)
	}
	first, err := store.CurrentDownloadManifest(ctx, root.FsID)
	if err != nil || first == nil {
		t.Fatalf("missing durable manifest %+v err=%v", first, err)
	}
	entries, err := store.DownloadManifestEntries(ctx, first.ID, 0, 10)
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries=%+v err=%v", entries, err)
	}
	p.lists[root.Path] = []baiduapi.FileInfo{child, child}
	if _, err = d.prepareDirectoryTree(ctx, root, root.ServerFilename); err == nil {
		t.Fatal("duplicate cloud identity accepted")
	}
	after, err := store.CurrentDownloadManifest(ctx, root.FsID)
	if err != nil || after == nil || after.ID != first.ID {
		t.Fatalf("incomplete attempt replaced manifest %+v err=%v", after, err)
	}
	if d.GetTaskByFsID(root.FsID).DirectoryPrepared {
		t.Fatal("failed preparation marked complete")
	}
}

func TestDirectoryManifestStreamsMoreThanOneBatch(t *testing.T) {
	d, p := setup(t, func(http.ResponseWriter, *http.Request) { t.Error("preparation must not transfer") })
	store := openEventStore(t, d)
	root := file(1800, "many", 0)
	root.IsDir = 1
	job := &queueJob{Version: 1, Info: root, Mode: DownloadModeKeep, Status: "pending"}
	d.queue.jobs[root.FsID] = job
	if err := d.queue.persist(job); err != nil {
		t.Fatal(err)
	}
	children := make([]baiduapi.FileInfo, 600)
	for i := range children {
		children[i] = file(int64(1801+i), fmt.Sprintf("%04d.bin", i), int64(i))
		children[i].Path = root.Path + "/" + children[i].ServerFilename
	}
	p.lists[root.Path] = children
	ctx := context.Background()
	if _, err := d.prepareDirectoryTree(ctx, root, root.ServerFilename); err != nil {
		t.Fatal(err)
	}
	m, err := store.CurrentDownloadManifest(ctx, root.FsID)
	if err != nil || m == nil {
		t.Fatalf("manifest=%+v err=%v", m, err)
	}
	total := 0
	for offset := 0; ; offset += 200 {
		entries, err := store.DownloadManifestEntries(ctx, m.ID, offset, 200)
		if err != nil {
			t.Fatal(err)
		}
		total += len(entries)
		if len(entries) < 200 {
			break
		}
	}
	if total != 601 {
		t.Fatalf("lost entries across batches: %d", total)
	}
}
