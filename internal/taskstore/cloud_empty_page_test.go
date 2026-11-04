package taskstore

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestEmptyDirectoryPagePinsGenerationAndProtectsQueue(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	pages := []struct {
		path    string
		entries []CloudInventoryEntry
	}{{"/", []CloudInventoryEntry{{FsID: 1, Path: "/a", Name: "a", Directory: true}, {FsID: 3, Path: "/queue", Name: "queue", Directory: true}, {FsID: 4, Path: "/queue-safe", Name: "queue-safe", Directory: true}, {FsID: 5, Path: "/z", Name: "z", Directory: true}}}, {"/a", []CloudInventoryEntry{{FsID: 2, Path: "/a/b", Name: "b", Directory: true}}}, {"/a/b", nil}, {"/queue", nil}, {"/queue-safe", nil}, {"/z", nil}}
	for _, p := range pages {
		if err = s.UpsertCloudInventoryPage("acct", "inventory", p.path, p.entries, now, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.SaveCloudScanState("acct", "inventory", "complete", "complete", "/", 0, 6, 5, now, now, ""); err != nil {
		t.Fatal(err)
	}
	first, err := s.ListEmptyCloudDirectoryPage("acct", "/queue", 0, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != 1 || first.Entries[0].Path != "/a/b" || first.Total != 3 || first.Coverage != "complete" {
		t.Fatalf("first=%+v", first)
	}
	second, err := s.ListEmptyCloudDirectoryPage("acct", "/queue", 1, 1, &first.Generation)
	if err != nil || len(second.Entries) != 1 || second.Entries[0].Path != "/queue-safe" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if err = s.InvalidateCloudInventory("acct", []string{"/z"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ListEmptyCloudDirectoryPage("acct", "/queue", 1, 1, &first.Generation); !errors.Is(err, ErrCloudInventoryGenerationChanged) {
		t.Fatalf("stale page accepted: %v", err)
	}
}
