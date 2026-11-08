package foldersync

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolutionCheckReobservesBothSidesAndNeverWritesFiles(t *testing.T) {
	for _, scenario := range []string{"ready", "local_changed", "remote_changed", "copy_occupied", "config_changed"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("left"), 0600); err != nil {
				t.Fatal(err)
			}
			f := &fixtureRemote{entries: []Entry{{ID: "remote", Path: "a.txt", Size: 5, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("right")))}}}
			m, err := Open(t.TempDir(), f, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			c, err := m.Create(context.Background(), Config{Name: "conflict", Mode: "both", LocalRoot: root, RemoteRoot: "/fixture"})
			if err != nil {
				t.Fatal(err)
			}
			run, err := m.Start(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := waitRun(t, m, c.ID, run.ID); got.Status != "conflict" {
				t.Fatalf("run %+v", got)
			}
			decision, err := m.CreateResolution(context.Background(), run.ID, 1, "keep_both")
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "local_changed":
				os.WriteFile(filepath.Join(root, "a.txt"), []byte("edited"), 0600)
			case "remote_changed":
				f.entries[0].ID = "replacement"
			case "copy_occupied":
				os.WriteFile(filepath.Join(root, decision.LocalCopy), []byte("occupied"), 0600)
			case "config_changed":
				c.Mode = "pull"
				if _, err = m.db.Exec(`UPDATE sync_configs SET body=? WHERE id=?`, encoded(c), c.ID); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := m.Events(run.ID)
			result, err := m.CheckResolution(context.Background(), decision.ID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "ready" && result.Status != "verified" || scenario != "ready" && result.Status != "rejected" {
				t.Fatalf("check %+v", result)
			}
			if result.DecisionID != decision.ID || result.ID == "" || result.Finished.IsZero() || result.Started.IsZero() || f.calls != 0 {
				t.Fatalf("evidence %+v calls %d", result, f.calls)
			}
			after, _ := m.Events(run.ID)
			if len(before) != len(after) {
				t.Fatal("check modified original run")
			}
			reread, err := m.ResolutionCheck(context.Background(), result.ID)
			if err != nil || reread.Status != result.Status {
				t.Fatal("check not persisted")
			}
		})
	}
}

func TestResolutionCheckIncompleteEvidenceSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, &fixtureRemote{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	intent := ResolutionCheck{ID: newID(), DecisionID: newID(), Started: time.Now().UTC(), Status: "incomplete", Reason: "no_completion_evidence"}
	if _, err = m.db.Exec(`INSERT INTO sync_resolution_check_intents VALUES(?,?,?)`, intent.ID, intent.DecisionID, encoded(intent)); err != nil {
		m.Close()
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = Open(dir, &fixtureRemote{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	got, err := m.ResolutionCheck(context.Background(), intent.ID)
	if err != nil || got.Status != "incomplete" || !got.Finished.IsZero() {
		t.Fatalf("unfinished check inferred success: %+v %v", got, err)
	}
	checks, more, err := m.ResolutionChecks(context.Background(), intent.DecisionID, 0)
	if err != nil || more || len(checks) != 1 || checks[0].ID != intent.ID || checks[0].Status != "incomplete" {
		t.Fatalf("unfinished check not discoverable: %+v %v", checks, err)
	}
	if _, err = m.db.Exec(`UPDATE sync_resolution_check_intents SET body='{}' WHERE id=?`, intent.ID); err == nil {
		t.Fatal("mutable intent")
	}
	if _, err = m.db.Exec(`DELETE FROM sync_resolution_check_intents WHERE id=?`, intent.ID); err == nil {
		t.Fatal("deletable intent")
	}
	var count int
	if err = m.db.QueryRow(`SELECT count(*) FROM sync_runs`).Scan(&count); err != nil || count != 0 {
		t.Fatal("reopen started sync")
	}
}
