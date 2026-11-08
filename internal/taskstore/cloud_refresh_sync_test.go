package taskstore

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCacheSyncObservationReduction(t *testing.T) {
	a := &CloudInventoryRefreshChangeItem{FsID: 1, Path: "/a/old", Name: "old", Size: 3}
	b := &CloudInventoryRefreshChangeItem{FsID: 1, Path: "/b/new", Name: "new", Size: 3}
	for _, tc := range []struct {
		name         string
		rows         []cacheObservation
		old, newItem *CloudInventoryRefreshChangeItem
		ambiguous    bool
	}{
		{"source_first", []cacheObservation{{a, nil}, {nil, b}}, a, b, false},
		{"destination_first", []cacheObservation{{nil, b}, {a, nil}}, a, b, false},
		{"added_then_modified", []cacheObservation{{nil, a}, {a, b}}, nil, b, false},
		{"modified_then_removed", []cacheObservation{{a, b}, {b, nil}}, a, nil, false},
		{"appeared_and_disappeared", []cacheObservation{{nil, a}, {a, nil}}, nil, nil, true},
		{"discontinuous", []cacheObservation{{a, b}, {a, nil}}, a, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old, newItem, ambiguous := reduceCacheObservations(tc.rows)
			if old != tc.old || newItem != tc.newItem || ambiguous != tc.ambiguous {
				t.Fatalf("old=%+v new=%+v ambiguous=%v", old, newItem, ambiguous)
			}
		})
	}
}

func TestRefreshMoveIntoDirectoryPreservesPreviousGlobalPath(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "q"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	for dir, items := range map[string][]CloudInventoryEntry{"/from": {{FsID: 1, Path: "/from/a", Name: "a", Size: 3}}, "/to": {}} {
		if err = s.UpsertCloudInventoryPage("acct", "initial", dir, items, now, nil); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	delta, known, err := cloudInventoryDirectoryDelta(tx, "acct", "/to", []CloudInventoryEntry{{FsID: 1, Path: "/to/b", Name: "b", Size: 3}}, now)
	if err != nil || !known || len(delta.Changes) != 1 || delta.Changes[0].Old == nil || delta.Changes[0].Old.Path != "/from/a" || delta.Changes[0].New.Path != "/to/b" {
		t.Fatalf("lost move source %+v known=%v err=%v", delta, known, err)
	}
}
