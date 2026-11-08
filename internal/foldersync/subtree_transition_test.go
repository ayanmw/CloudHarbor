package foldersync

import "testing"

func TestSubtreeTransitionOnlyAdoptsIntendedChanges(t *testing.T) {
	parent := Entry{Path: "folder", ID: "parent", Directory: true, Mtime: 1}
	original := Entry{Path: "other", ID: "unrelated", Size: 1, SHA256: "unchanged", Mtime: 1}
	remote := Entry{Path: "folder/child", ID: "remote", Size: 3, SHA256: "content", Mtime: 1}
	step := Step{Action: "download", Path: remote.Path, Remote: &remote}
	before := Snapshot{Local: []Entry{parent, original}, Remote: []Entry{parent, remote}}
	child := remote
	child.ID = "new-local"
	child.Mtime = 22
	for _, scenario := range []string{"valid", "unrelated", "unexpected_child", "wrong_content", "remote_changed"} {
		t.Run(scenario, func(t *testing.T) {
			after := Snapshot{Local: []Entry{parent, original, child}, Remote: []Entry{parent, remote}}
			after.Local[0].Mtime = 22
			switch scenario {
			case "unrelated":
				after.Local[1].Mtime++
			case "unexpected_child":
				after.Local = append(after.Local, Entry{Path: "folder/extra", ID: "extra"})
			case "wrong_content":
				after.Local[2].SHA256 = "wrong"
			case "remote_changed":
				after.Remote[1].ID = "replaced"
			}
			err := verifySubtreeTransition(before, after, step)
			if (err == nil) != (scenario == "valid") {
				t.Fatal(scenario, err)
			}
		})
	}
}

func TestWholeObservationFingerprintIncludesAllPaths(t *testing.T) {
	a := []Entry{{Path: "folder", ID: "one", Directory: true}}
	b := append(append([]Entry{}, a...), Entry{Path: "other", ID: "two", Directory: true})
	ha, ca := subtreeIdentity(a, "")
	hb, cb := subtreeIdentity(b, "")
	if ha == hb || ca != "1" || cb != "2" {
		t.Fatal("whole observation ignored paths", ca, cb)
	}
}
