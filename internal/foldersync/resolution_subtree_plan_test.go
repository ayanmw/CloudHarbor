package foldersync

import (
	"strings"
	"testing"
)

func TestSubtreePlanArchivesBeforeTypeReplacementAndPreservesUnchangedFiles(t *testing.T) {
	hash := strings.Repeat("a", 64)
	other := strings.Repeat("b", 64)
	root := Entry{Path: "folder", ID: "root", Directory: true}
	file := Entry{Path: "folder", ID: "file", Size: 7, SHA256: hash}
	child := Entry{Path: "folder/file", ID: "child", Size: 9, SHA256: other}
	review, err := BuildSubtreeReview("both", "keep_remote", "folder", "", Snapshot{Local: []Entry{file}, Remote: []Entry{root, child, {Path: "folder/empty", ID: "empty", Directory: true}}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := CompileSubtreePlan("both", "keep_remote", "", review)
	if err != nil || len(plan) != 4 || plan[0].Action != "local_archive" || plan[1].Action != "local_mkdir" || plan[2].Action != "local_mkdir" || plan[3].Action != "download" {
		t.Fatalf("replacement %+v %v", plan, err)
	}
	review, err = BuildSubtreeReview("both", "keep_local", "folder", "", Snapshot{Local: []Entry{file}, Remote: []Entry{root, child}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err = CompileSubtreePlan("both", "keep_local", "", review)
	if err != nil || len(plan) != 2 || plan[0].Action != "remote_archive" || plan[1].Action != "upload" || plan[1].Remote != nil {
		t.Fatalf("archive subtree %+v %v", plan, err)
	}
	left := []Entry{root, {Path: "folder/same", ID: "same-l", Size: 7, SHA256: hash}, {Path: "folder/old", ID: "old", Size: 7, SHA256: hash}}
	right := []Entry{root, {Path: "folder/same", ID: "same-r", Size: 7, SHA256: hash}, {Path: "folder/new", ID: "new", Size: 9, SHA256: other}}
	review, err = BuildSubtreeReview("both", "keep_remote", "folder", "", Snapshot{Local: left, Remote: right})
	if err != nil {
		t.Fatal(err)
	}
	plan, err = CompileSubtreePlan("both", "keep_remote", "", review)
	if err != nil || len(plan) != 2 || plan[0].Path != "folder/old" || plan[1].Path != "folder/new" {
		t.Fatalf("unchanged content retransferred: %+v %v", plan, err)
	}
	review, err = BuildSubtreeReview("both", "keep_both", "folder", "folder.copy", Snapshot{Local: left, Remote: right})
	if err != nil {
		t.Fatal(err)
	}
	plan, err = CompileSubtreePlan("both", "keep_both", "folder.copy", review)
	if err != nil || plan[0].Action != "local_move" || plan[0].Path != "folder.copy" {
		t.Fatalf("keep both %+v %v", plan, err)
	}
	for _, s := range plan {
		if s.Action == "upload" && (!strings.HasPrefix(s.Path, "folder.copy/") || s.Local.Path != s.Path) {
			t.Fatalf("copy upload source mismatch %+v", s)
		}
	}
	review, err = BuildSubtreeReview("pull", "keep_local", "folder", "", Snapshot{Local: left, Remote: right})
	if err != nil {
		t.Fatal(err)
	}
	plan, err = CompileSubtreePlan("pull", "keep_local", "", review)
	if err != nil || len(plan) != 0 {
		t.Fatal("pull local choice planned writes")
	}
	review, err = BuildSubtreeReview("both", "keep_remote", "folder", "", Snapshot{Local: left, Remote: []Entry{}})
	if err != nil {
		t.Fatal(err)
	}
	plan, err = CompileSubtreePlan("both", "keep_remote", "", review)
	if err != nil || len(plan) != 1 || plan[0].Action != "local_archive" {
		t.Fatalf("deletion lost subtree backup: %+v %v", plan, err)
	}
}
