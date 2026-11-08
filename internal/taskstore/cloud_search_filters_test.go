package taskstore

import (
	"errors"
	"path"
	"sort"
	"testing"
	"time"
)

func TestCloudSearchScopeAndKindFilterBeforePagination(t *testing.T) {
	s, err := Open(t.TempDir(), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	at := time.Now().UTC()
	entries := []CloudInventoryEntry{
		{FsID: 1, Path: "/课程%_", Name: "课程%_", Directory: true},
		{FsID: 2, Path: "/课程%_/lesson-a", Name: "lesson-a", Size: 2},
		{FsID: 3, Path: "/课程%_/lesson-folder", Name: "lesson-folder", Directory: true},
		{FsID: 4, Path: "/课程%_/子目录/lesson-b", Name: "lesson-b", Size: 3},
		{FsID: 5, Path: "/课程%_other/lesson-x", Name: "lesson-x", Size: 4},
		{FsID: 6, Path: "/课程XX/lesson-x", Name: "lesson-x", Size: 5},
	}
	pages := map[string][]CloudInventoryEntry{}
	for _, entry := range entries {
		pages[path.Dir(entry.Path)] = append(pages[path.Dir(entry.Path)], entry)
	}
	// Publish parents before descendants: refreshing a parent invalidates cached
	// descendants, so randomized map order would erase this test's child fixture.
	directories := make([]string, 0, len(pages))
	for directory := range pages {
		directories = append(directories, directory)
	}
	sort.Strings(directories)
	for _, directory := range directories {
		if err = s.UpsertCloudInventoryPage("account", "run", directory, pages[directory], at, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.UpsertCloudInventoryPage("other", "run", "/课程%_", []CloudInventoryEntry{{FsID: 7, Path: "/课程%_/lesson-private", Name: "lesson-private"}}, at, nil); err != nil {
		t.Fatal(err)
	}
	filter := CloudInventorySearchFilter{Scope: "/课程%_", Kind: "file"}
	page, total, coverage, err := s.SearchCloudInventoryFilteredAtGeneration("account", "lesson", 0, 1, nil, filter)
	if err != nil || total != 2 || len(page) != 1 || page[0].FsID != 2 {
		t.Fatalf("page=%+v total=%d err=%v", page, total, err)
	}
	page, total, _, err = s.SearchCloudInventoryFilteredAtGeneration("account", "lesson", 1, 1, &coverage.Generation, filter)
	if err != nil || total != 2 || len(page) != 1 || page[0].FsID != 4 {
		t.Fatalf("page2=%+v total=%d err=%v", page, total, err)
	}
	page, total, _, err = s.SearchCloudInventoryFilteredAtGeneration("account", "lesson", 0, 50, nil, CloudInventorySearchFilter{Scope: "/课程%_", Kind: "directory"})
	if err != nil || total != 1 || len(page) != 1 || page[0].FsID != 3 {
		t.Fatalf("directories=%+v total=%d err=%v", page, total, err)
	}
	_, total, _, err = s.SearchCloudInventoryFilteredAtGeneration("account", "课程%_", 0, 50, nil, CloudInventorySearchFilter{Scope: "/课程%_", Kind: "all"})
	if err != nil || total != 3 {
		t.Fatalf("scope must exclude itself total=%d err=%v", total, err)
	}
	old := coverage.Generation
	if err = s.UpsertCloudInventoryPage("account", "run2", "/", entries[:1], at, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = s.SearchCloudInventoryFilteredAtGeneration("account", "lesson", 1, 1, &old, filter); !errors.Is(err, ErrCloudInventoryGenerationChanged) {
		t.Fatalf("stale=%v", err)
	}
}

func TestCloudSearchRejectsInvalidScopeAndKind(t *testing.T) {
	s, err := Open(t.TempDir(), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, filter := range []CloudInventorySearchFilter{{Scope: "/a/../b", Kind: "all"}, {Scope: "/a/", Kind: "all"}, {Scope: "relative", Kind: "all"}, {Scope: "/", Kind: "video"}, {Scope: "/a\t", Kind: "all"}} {
		if _, _, _, err = s.SearchCloudInventoryFilteredAtGeneration("account", "lesson", 0, 10, nil, filter); err == nil {
			t.Fatalf("accepted %+v", filter)
		}
	}
}
