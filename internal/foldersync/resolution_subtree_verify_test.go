package foldersync

import "testing"

func TestConfirmedDirectoryOutputsRequireEntireSubtreeAndSupportAbsence(t *testing.T) {
	root := Entry{Path: "folder", ID: "root", Directory: true}
	child := Entry{Path: "folder/child", ID: "file", Size: 3, SHA256: "hash", Mtime: 1}
	d := Resolution{SubtreeReview: &SubtreeReview{}, Conflict: StepRecord{Step: Step{Path: "folder"}}}
	for _, scenario := range []string{"same", "child_changed", "extra", "removed", "both_absent", "resurrected"} {
		t.Run(scenario, func(t *testing.T) {
			before := Snapshot{Local: []Entry{root, child}, Remote: []Entry{root, child}}
			current := Snapshot{Local: append([]Entry{}, before.Local...), Remote: append([]Entry{}, before.Remote...)}
			switch scenario {
			case "child_changed":
				current.Remote[1].ID = "replacement"
			case "extra":
				current.Local = append(current.Local, Entry{Path: "folder/extra", ID: "extra"})
			case "removed":
				current.Local = current.Local[:1]
			case "both_absent", "resurrected":
				before = Snapshot{Local: []Entry{}, Remote: []Entry{}}
				current = Snapshot{Local: []Entry{}, Remote: []Entry{}}
				if scenario == "resurrected" {
					current.Remote = []Entry{root}
				}
			}
			got := confirmedResolutionMatches(pendingResolution{decision: d, after: before}, current)
			if got != (scenario == "same" || scenario == "both_absent") {
				t.Fatal(scenario, got)
			}
		})
	}
}

func TestDirectoryVerificationDoesNotFollowRemovedChildrenIntoRetainedCopy(t *testing.T) {
	oldDir := Entry{Path: "folder", ID: "old-dir", Directory: true}
	oldChild := Entry{Path: "folder/child", ID: "old-child", Size: 3, SHA256: "old"}
	base := Snapshot{Local: []Entry{oldDir, oldChild}, Remote: []Entry{oldDir, oldChild}}
	file := Entry{Path: "folder", ID: "new-file", Size: 5, SHA256: "new"}
	copyDir := oldDir
	copyDir.Path = "folder.copy"
	copyChild := oldChild
	copyChild.Path = "folder.copy/child"
	current := Snapshot{Local: []Entry{file, copyDir, copyChild}, Remote: []Entry{file}}
	d := Resolution{ID: "decision", SubtreeReview: &SubtreeReview{}, Conflict: StepRecord{Step: Step{Path: "folder"}}}
	rebindResolutionBaseline(&base, d, current)
	if base.Local[1].ID == oldChild.ID || base.Remote[1].ID == oldChild.ID {
		t.Fatal("removed child retained original identity")
	}
	if steps := Plan("pull", base, current); len(steps) != 0 {
		t.Fatalf("retained copy misread as a move: %+v", steps)
	}
}
