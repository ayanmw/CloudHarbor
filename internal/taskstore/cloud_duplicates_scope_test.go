package taskstore

import (
	"strings"
	"testing"
	"time"
)

func TestDuplicatePreviewScopeExcludesOutsideCopiesAndSiblingPrefixes(t *testing.T) {
	s, err := Open(t.TempDir(), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	files := []CloudInventoryEntry{{FsID: 1, Path: "/a/one", Name: "one", Size: 7}, {FsID: 2, Path: "/a/two", Name: "two", Size: 7}, {FsID: 3, Path: "/ab/outside", Name: "outside", Size: 7}}
	for _, root := range []string{"/a", "/ab"} {
		var entries []CloudInventoryEntry
		for _, f := range files {
			if strings.HasPrefix(f.Path, root+"/") {
				entries = append(entries, f)
			}
		}
		if err = s.UpsertCloudInventoryPage("acct", "run", root, entries, now, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range files {
		if err = s.SaveVerifiedCloudContentHash("acct", f, strings.Repeat("a", 64), now); err != nil {
			t.Fatal(err)
		}
	}
	groups, total, deleted, err := s.ListVerifiedCloudDuplicateGroupsInDirectory("acct", "/a", 0, 10)
	if err != nil || total != 1 || deleted != 1 || len(groups) != 1 || groups[0].Keep.Path != "/a/one" || len(groups[0].Delete) != 1 || groups[0].Delete[0].Path != "/a/two" {
		t.Fatalf("scope %+v %d %d %v", groups, total, deleted, err)
	}
	groups, total, deleted, err = s.ListVerifiedCloudDuplicateGroupsInDirectory("acct", "/ab", 0, 10)
	if err != nil || len(groups) != 0 || total != 0 || deleted != 0 {
		t.Fatalf("singleton %+v %d %d %v", groups, total, deleted, err)
	}
	groups, total, deleted, err = s.ListVerifiedCloudDuplicateGroupsInDirectory("acct", "/", 0, 10)
	if err != nil || total != 1 || deleted != 2 || len(groups[0].Delete) != 2 {
		t.Fatalf("root %+v %d %d %v", groups, total, deleted, err)
	}
	if _, _, _, err = s.ListVerifiedCloudDuplicateGroupsInDirectory("acct", "/a/../ab", 0, 10); err == nil {
		t.Fatal("invalid scope accepted")
	}
}
