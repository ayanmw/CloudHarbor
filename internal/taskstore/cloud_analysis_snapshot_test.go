package taskstore

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCloudAnalysisSnapshotBindsHashesAndCoverage(t *testing.T) {
	s, err := Open(t.TempDir(), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	item := CloudInventoryEntry{FsID: 3, Path: "/a/file", Name: "file", Size: 9007199254740993, ProviderMtime: 7}
	for _, p := range []struct {
		root    string
		entries []CloudInventoryEntry
	}{
		{"/", []CloudInventoryEntry{{FsID: 1, Path: "/a", Name: "a", Directory: true}, {FsID: 2, Path: "/b", Name: "b", Directory: true}}},
		{"/a", []CloudInventoryEntry{item}}, {"/b", nil},
	} {
		if err = s.UpsertCloudInventoryPage("acct", "run", p.root, p.entries, now, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.SaveVerifiedCloudContentHash("acct", item, strings.Repeat("a", 64), now); err != nil {
		t.Fatal(err)
	}
	got, err := s.CloudAnalysisSnapshot(context.Background(), "acct", []string{"/a", "/b"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation <= 0 || !got.Inputs[0].Complete || !got.Inputs[1].Complete || len(got.Inputs[1].Entries) != 0 || len(got.Inputs[0].Entries) != 1 {
		t.Fatalf("snapshot %+v", got)
	}
	e := got.Inputs[0].Entries[0]
	if e.Path != "file" || e.Size != "9007199254740993" || e.SHA256 != strings.Repeat("a", 64) || e.Observed == nil {
		t.Fatalf("entry %+v", e)
	}
	// A changed object must never borrow the hash of its previous observation.
	if _, err = s.db.Exec(`UPDATE cloud_inventory SET provider_mtime=8 WHERE account_fingerprint='acct' AND fs_id=3`); err != nil {
		t.Fatal(err)
	}
	got, err = s.CloudAnalysisSnapshot(context.Background(), "acct", []string{"/a", "/missing"})
	if err != nil || got.Inputs[0].Entries[0].SHA256 != "" || got.Inputs[1].Complete {
		t.Fatalf("stale hash/coverage %+v %v", got, err)
	}
	other, err := s.CloudAnalysisSnapshot(context.Background(), "other", []string{"/a"})
	if err != nil || other.Inputs[0].Complete || len(other.Inputs[0].Entries) != 0 {
		t.Fatalf("account leak %+v %v", other, err)
	}
	if err = s.InvalidateCloudInventory("acct", []string{"/a"}); err != nil {
		t.Fatal(err)
	}
	stale, err := s.CloudAnalysisSnapshot(context.Background(), "acct", []string{"/a"})
	if err != nil || stale.Inputs[0].Complete {
		t.Fatalf("stale coverage %+v %v", stale, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.CloudAnalysisSnapshot(ctx, "acct", []string{"/a"}); err == nil {
		t.Fatal("cancellation ignored")
	}
	for _, roots := range [][]string{nil, {"/a/../b"}, {"relative"}, {"/a", "/b", "/c"}} {
		if _, err = s.CloudAnalysisSnapshot(context.Background(), "acct", roots); err == nil {
			t.Fatalf("accepted %v", roots)
		}
	}
}
