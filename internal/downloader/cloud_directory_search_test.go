package downloader

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func TestSearchCachedCloudDirectoriesStaysLocalAndAccountScoped(t *testing.T) {
	d, p := setup(t, func(http.ResponseWriter, *http.Request) {})
	store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	d.queue.store = store
	client := d.getClient()
	now := time.Now().UTC()
	if err = store.UpsertCloudInventoryPage(client.Fingerprint(), "run", "/", []taskstore.CloudInventoryEntry{{FsID: 1, Path: "/lesson-dir", Name: "lesson-dir", Directory: true}, {FsID: 2, Path: "/lesson-file", Name: "lesson-file", Size: 1}}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("other-account", "run", "/", []taskstore.CloudInventoryEntry{{FsID: 3, Path: "/lesson-private", Name: "lesson-private", Directory: true}}, now, nil); err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(client, false, time.Time{})
	filter := taskstore.CloudInventorySearchFilter{Scope: "/", Kind: "file"}
	if _, _, _, err = d.SearchCachedCloudFiltered(context.Background(), "lesson", 0, 50, nil, filter); err == nil {
		t.Fatal("unauthorized filtered cache search allowed")
	}
	if _, _, _, err = d.SearchCachedCloudDirectories(context.Background(), "lesson", 0, 50, nil); err == nil {
		t.Fatal("unauthorized cache search allowed")
	}
	d.SetAuthentication(client, true, now.Add(time.Hour))
	files, count, _, filteredErr := d.SearchCachedCloudFiltered(context.Background(), "lesson", 0, 50, nil, filter)
	if filteredErr != nil || count != 1 || len(files) != 1 || files[0].Path != "/lesson-file" {
		t.Fatalf("filtered=%+v count=%d err=%v", files, count, filteredErr)
	}
	filter.ModifiedBefore = 200
	if files, count, _, err = d.SearchCachedCloudFiltered(context.Background(), "lesson", 0, 50, nil, filter); err != nil || count != 0 || len(files) != 0 {
		t.Fatalf("unknown provider time was not excluded: entries=%+v count=%d err=%v", files, count, err)
	}
	filter.ModifiedBefore = 0
	filter.Extensions = "pdf"
	if files, count, _, err = d.SearchCachedCloudFiltered(context.Background(), "lesson", 0, 50, nil, filter); err != nil || count != 0 || len(files) != 0 {
		t.Fatalf("extension filter was not forwarded: entries=%+v count=%d err=%v", files, count, err)
	}
	entries, total, _, err := d.SearchCachedCloudDirectories(context.Background(), "lesson", 0, 50, nil)
	if err != nil || total != 1 || len(entries) != 1 || entries[0].Path != "/lesson-dir" {
		t.Fatalf("entries=%+v total=%d err=%v", entries, total, err)
	}
	if len(p.listRequests) != 0 || len(p.metadataCalls) != 0 || len(p.actions) != 0 {
		t.Fatalf("local search contacted provider: lists=%v metadata=%v actions=%v", p.listRequests, p.metadataCalls, p.actions)
	}
}
