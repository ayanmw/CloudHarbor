package foldersync

import "testing"

func file(p, id, hash string) Entry { return Entry{Path: p, ID: id, SHA256: hash, Size: 3} }
func TestThreeWayPlan(t *testing.T) {
	b := Snapshot{Local: []Entry{file("a", "l1", "old")}, Remote: []Entry{file("a", "r1", "old")}}
	for _, tc := range []struct {
		name, mode    string
		base          Snapshot
		local, remote []Entry
		action        string
	}{
		{"first_remote", "pull", Snapshot{}, nil, []Entry{file("a", "r1", "old")}, "download"},
		{"first_local_preserved", "pull", Snapshot{}, []Entry{file("a", "l1", "old")}, nil, ""},
		{"first_local_upload", "both", Snapshot{}, []Entry{file("a", "l1", "old")}, nil, "upload"},
		{"first_conflict", "both", Snapshot{}, []Entry{file("a", "l1", "new")}, []Entry{file("a", "r1", "old")}, "conflict"},
		{"local_rename", "both", b, []Entry{file("b", "l1", "old")}, b.Remote, "remote_move"},
		{"remote_rename", "both", b, b.Local, []Entry{file("b", "r1", "old")}, "local_move"},
		{"local_edit", "both", b, []Entry{file("a", "l1", "new")}, b.Remote, "upload"},
		{"pull_local_edit_keeps_common_baseline", "pull", b, []Entry{file("a", "l1", "new")}, b.Remote, "conflict"},
		{"remote_edit", "pull", b, b.Local, []Entry{file("a", "r1", "new")}, "download"},
		{"local_delete", "both", b, nil, b.Remote, "remote_delete"},
		{"remote_delete", "pull", b, b.Local, nil, "local_delete"},
		{"edit_delete_conflict", "both", b, []Entry{file("a", "l1", "new")}, nil, "conflict"},
		{"both_edit_conflict", "both", b, []Entry{file("a", "l1", "new")}, []Entry{file("a", "r1", "different")}, "conflict"},
		{"unchanged", "both", b, b.Local, b.Remote, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ops := Plan(tc.mode, tc.base, Snapshot{Local: tc.local, Remote: tc.remote})
			if tc.action == "" {
				if len(ops) != 0 {
					t.Fatalf("unexpected: %+v", ops)
				}
				return
			}
			if len(ops) != 1 || ops[0].Action != tc.action {
				t.Fatalf("ops=%+v want %s", ops, tc.action)
			}
		})
	}
}
