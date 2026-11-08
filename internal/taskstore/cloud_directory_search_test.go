package taskstore

import (
	"errors"
	"testing"
	"time"
)

func TestDirectorySearchFiltersBeforeCountAndPagination(t *testing.T) {
	store, err := Open(t.TempDir(), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	entries := []CloudInventoryEntry{{FsID: 1, Path: "/lesson-file", Name: "lesson-file", Size: 1}, {FsID: 2, Path: "/lesson-a", Name: "lesson-a", Directory: true}, {FsID: 3, Path: "/lesson-b", Name: "lesson-b", Directory: true}}
	if err = store.UpsertCloudInventoryPage("account", "run", "/", entries, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = store.UpsertCloudInventoryPage("other", "run", "/", []CloudInventoryEntry{{FsID: 4, Path: "/lesson-private", Name: "lesson-private", Directory: true}}, now, nil); err != nil {
		t.Fatal(err)
	}
	page, total, coverage, err := store.SearchCloudDirectoriesAtGeneration("account", "lesson", 0, 1, nil)
	if err != nil || total != 2 || len(page) != 1 || page[0].FsID != 2 || !page[0].Directory {
		t.Fatalf("page=%+v total=%d err=%v", page, total, err)
	}
	page, total, _, err = store.SearchCloudDirectoriesAtGeneration("account", "lesson", 1, 1, &coverage.Generation)
	if err != nil || total != 2 || len(page) != 1 || page[0].FsID != 3 {
		t.Fatalf("second=%+v total=%d err=%v", page, total, err)
	}
	_, total, _, err = store.SearchCloudInventoryAtGeneration("account", "lesson", 0, 50, nil)
	if err != nil || total != 3 {
		t.Fatalf("default mixed search changed: total=%d err=%v", total, err)
	}
	old := coverage.Generation
	if err = store.UpsertCloudInventoryPage("account", "run2", "/", entries[:1], now, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = store.SearchCloudDirectoriesAtGeneration("account", "lesson", 1, 1, &old); !errors.Is(err, ErrCloudInventoryGenerationChanged) {
		t.Fatalf("stale generation accepted: %v", err)
	}
}
