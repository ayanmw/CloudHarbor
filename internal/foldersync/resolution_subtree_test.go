package foldersync

import (
	"context"
	"strings"
	"testing"
)

func TestSubtreeReviewIncludesEveryDescendantAndKeepsDirection(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	snapshot := Snapshot{Local: []Entry{{Path: "folder", ID: "ld", Directory: true}, {Path: "folder/a", ID: "la", Size: 7, SHA256: a}, {Path: "folder/only", ID: "lo", Size: 9007199254740993, SHA256: a}}, Remote: []Entry{{Path: "folder", ID: "rd", Directory: true}, {Path: "folder/a", ID: "ra", Size: 7, SHA256: b}, {Path: "folder/new", ID: "rn", Size: 3, SHA256: b}}}
	review, err := BuildSubtreeReview("both", "keep_remote", "folder", "", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(review.Items) != 4 || review.LocalBytes != "9007199254741000" || review.RemoteBytes != "10" {
		t.Fatalf("review %+v", review)
	}
	items := map[string]SubtreeImpact{}
	for _, item := range review.Items {
		items[item.Path] = item
	}
	if items["folder/a"].LocalAction != "backup_then_download" || items["folder/only"].LocalAction != "backup_then_remove" || items["folder/new"].LocalAction != "download" {
		t.Fatalf("actions %+v", items)
	}
	copies, err := BuildSubtreeReview("both", "keep_both", "folder", "folder.copy", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range copies.Items {
		if item.Local != nil && item.RemoteTarget != "folder.copy"+strings.TrimPrefix(item.Path, "folder") {
			t.Fatalf("copy target %+v", item)
		}
	}
	pull, err := BuildSubtreeReview("pull", "keep_local", "folder", "", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range pull.Items {
		if item.RemoteAction != "keep" || item.LocalAction != "keep" {
			t.Fatal("pull local choice planned writes")
		}
	}
	missing := Snapshot{Local: snapshot.Local, Remote: []Entry{}}
	deleted, err := BuildSubtreeReview("both", "keep_remote", "folder", "", missing)
	if err != nil || len(deleted.Items) != 3 {
		t.Fatalf("deletion %+v %v", deleted, err)
	}
	missing.Local = missing.Local[1:]
	if _, err = BuildSubtreeReview("both", "keep_remote", "folder", "", missing); err == nil {
		t.Fatal("missing root mistaken for complete subtree")
	}
	swap := Snapshot{Local: []Entry{{Path: "folder", ID: "local-file", Size: 3, SHA256: a}}, Remote: snapshot.Remote}
	replaced, err := BuildSubtreeReview("both", "keep_local", "folder", "", swap)
	if err != nil || replaced.Items[0].RemoteAction != "backup_then_replace_type" || len(replaced.Items) != 3 {
		t.Fatalf("type replacement %+v %v", replaced, err)
	}
}

func TestDirectoryConflictDecisionPersistsFullReviewWithoutExecution(t *testing.T) {
	m, err := Open(t.TempDir(), &fixtureRemote{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	run := newID()
	c := Config{ID: newID(), Account: "account", Mode: "both", LocalRoot: "D:/fixture", RemoteRoot: "/fixture"}
	l := Entry{Path: "folder", ID: "local-dir", Directory: true}
	r := Entry{Path: "folder", ID: "remote-dir", Directory: true}
	snapshot := Snapshot{Local: []Entry{l, {Path: "folder/a", ID: "local-file", Size: 7, SHA256: strings.Repeat("a", 64)}}, Remote: []Entry{r, {Path: "folder/a", ID: "remote-file", Size: 9, SHA256: strings.Repeat("b", 64)}}}
	if err = m.event(run, "run_started", c); err != nil {
		t.Fatal(err)
	}
	if err = m.event(run, "snapshot_before", snapshot); err != nil {
		t.Fatal(err)
	}
	if err = m.saveStep(run, StepRecord{Ordinal: 1, Status: "conflict", Step: Step{Action: "conflict", Path: "folder", Local: &l, Remote: &r}}); err != nil {
		t.Fatal(err)
	}
	decision, err := m.CreateResolution(context.Background(), run, 1, "keep_both")
	if err != nil {
		t.Fatal(err)
	}
	if decision.State != "review_required" || decision.SubtreeReview == nil || len(decision.SubtreeReview.Items) != 2 || decision.LocalCopy == "" || len(decision.Effects) != 0 {
		t.Fatalf("review %+v", decision)
	}
	read, err := m.Resolution(context.Background(), decision.ID)
	if err != nil || read.SubtreeReview.LocalBytes != "7" {
		t.Fatal("review not persisted")
	}
	check, err := m.CheckResolution(context.Background(), decision.ID)
	if err != nil || check.Status != "rejected" || check.Reason != "configuration_missing" || check.ExecutionSupported == nil {
		t.Fatal("subtree verification did not preserve execution/configuration boundaries")
	}
}
