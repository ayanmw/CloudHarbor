package taskstore

import (
	"testing"
	"time"
)

func TestCloudSearchExtensionsAndTimeBeforeCounting(t *testing.T) {
	s, err := Open(t.TempDir(), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entries := []CloudInventoryEntry{
		{FsID: 1, Path: "/lesson-a.PDF", Name: "lesson-a.PDF", ProviderMtime: 100},
		{FsID: 2, Path: "/lesson-b.mp4", Name: "lesson-b.mp4", ProviderMtime: 199},
		{FsID: 3, Path: "/lesson-c.pdf", Name: "lesson-c.pdf", ProviderMtime: 200},
		{FsID: 4, Path: "/lesson-d.pdf", Name: "lesson-d.pdf", ProviderMtime: 99},
		{FsID: 5, Path: "/lesson-e.pdf", Name: "lesson-e.pdf", ProviderMtime: 0},
		{FsID: 6, Path: "/lesson-f.pdf.txt", Name: "lesson-f.pdf.txt", ProviderMtime: 150},
		{FsID: 7, Path: "/lesson-folder.pdf", Name: "lesson-folder.pdf", Directory: true, ProviderMtime: 150},
	}
	if err = s.UpsertCloudInventoryPage("account", "run", "/", entries, time.Now().UTC(), nil); err != nil {
		t.Fatal(err)
	}
	if err = s.UpsertCloudInventoryPage("account", "run", "/lesson-folder.pdf", []CloudInventoryEntry{{FsID: 8, Path: "/lesson-folder.pdf/lesson-noextension", Name: "lesson-noextension", ProviderMtime: 150}}, time.Now().UTC(), nil); err != nil {
		t.Fatal(err)
	}
	f := CloudInventorySearchFilter{Scope: "/", Kind: "all", Extensions: "pdf,mp4", ModifiedFrom: 100, ModifiedBefore: 200}
	page, total, c, err := s.SearchCloudInventoryFilteredAtGeneration("account", "lesson", 0, 1, nil, f)
	if err != nil || total != 2 || len(page) != 1 || page[0].FsID != 1 {
		t.Fatalf("page=%+v total=%d err=%v", page, total, err)
	}
	page, total, _, err = s.SearchCloudInventoryFilteredAtGeneration("account", "lesson", 1, 1, &c.Generation, f)
	if err != nil || total != 2 || len(page) != 1 || page[0].FsID != 2 {
		t.Fatalf("page2=%+v total=%d err=%v", page, total, err)
	}
	f.ModifiedFrom = 0
	_, total, _, err = s.SearchCloudInventoryFilteredAtGeneration("account", "lesson", 0, 50, nil, f)
	if err != nil || total != 3 {
		t.Fatalf("upper-only must exclude unknown: total=%d err=%v", total, err)
	}
	f.ModifiedBefore = 0
	_, total, _, err = s.SearchCloudInventoryFilteredAtGeneration("account", "lesson", 0, 50, nil, f)
	if err != nil || total != 5 {
		t.Fatalf("without date filter unknown retained total=%d err=%v", total, err)
	}
	f.ModifiedFrom = 200
	page, total, _, err = s.SearchCloudInventoryFilteredAtGeneration("account", "lesson", 0, 50, nil, f)
	if err != nil || total != 1 || len(page) != 1 || page[0].FsID != 3 {
		t.Fatalf("lower-only inclusive: page=%+v total=%d err=%v", page, total, err)
	}
}

func TestCloudSearchExtraFiltersRejectInvalidValues(t *testing.T) {
	for _, f := range []CloudInventorySearchFilter{
		{Scope: "/", Kind: "directory", Extensions: "pdf"}, {Scope: "/", Kind: "all", Extensions: "PDF"}, {Scope: "/", Kind: "all", Extensions: "pdf,pdf"}, {Scope: "/", Kind: "all", Extensions: "p%f"}, {Scope: "/", Kind: "all", Extensions: ".pdf"}, {Scope: "/", Kind: "all", ModifiedFrom: -1}, {Scope: "/", Kind: "all", ModifiedFrom: 200, ModifiedBefore: 100}, {Scope: "/", Kind: "all", ModifiedFrom: 100, ModifiedBefore: 100}, {Scope: "/", Kind: "all", ModifiedBefore: 253402300801},
	} {
		if f.Valid() {
			t.Fatalf("accepted %+v", f)
		}
	}
	for _, extensions := range []string{"a,b,c,d,e,f,g,h,i,j,k", "abcdefghijklmnopq", "pdf,", "pdf,,mp4"} {
		if (CloudInventorySearchFilter{Scope: "/", Kind: "file", Extensions: extensions}).Valid() {
			t.Fatalf("accepted extensions=%s", extensions)
		}
	}
}
