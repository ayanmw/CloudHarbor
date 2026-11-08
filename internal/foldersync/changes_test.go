package foldersync

import "testing"

func TestObservedDirectoryRenameIsRecordedEvenWhenExpandedToFileOperations(t *testing.T) {
	before := Snapshot{Local: []Entry{{ID: "dir-id", Path: "old", Directory: true}, {ID: "file-id", Path: "old/a", Size: 3, SHA256: "same"}}}
	after := Snapshot{Local: []Entry{{ID: "dir-id", Path: "new", Directory: true}, {ID: "file-id", Path: "new/a", Size: 3, SHA256: "same"}}}
	changes := ObservedChanges(before, after)
	if len(changes) != 2 || changes[0].Action != "renamed" || changes[0].Old.Path != "old" || changes[0].New.Path != "new" || changes[1].Action != "moved" {
		t.Fatalf("directory move evidence missing: %+v", changes)
	}
}
