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

func TestResolutionVerificationRebindsOnlyConfirmedOutputsAndCommitsWholeScope(t *testing.T) {
	for _, scenario := range []string{"ready", "unrelated_conflict", "chosen_file_changed", "link_persistence_failure"} {
		t.Run(scenario, func(t *testing.T) {
			sum := func(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
			root := t.TempDir()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			f := &fixtureRemote{payload: "old", entries: []Entry{{ID: "remote", Path: "a.txt", Size: 3, SHA256: sum("old")}}}
			m, err := Open(dir, f, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			c, err := m.Create(context.Background(), Config{Name: "verification", Mode: "pull", LocalRoot: root, RemoteRoot: "/fixture"})
			if err != nil {
				t.Fatal(err)
			}
			initial, err := m.Start(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := waitRun(t, m, c.ID, initial.ID); got.Status != "complete" {
				t.Fatal(got)
			}
			if err = os.WriteFile(filepath.Join(root, "a.txt"), []byte("left"), 0600); err != nil {
				t.Fatal(err)
			}
			f.payload = "right"
			f.entries[0].Size = 5
			f.entries[0].SHA256 = sum("right")
			conflict, err := m.Start(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := waitRun(t, m, c.ID, conflict.ID); got.Status != "conflict" {
				t.Fatal(got)
			}
			decision, err := m.CreateResolution(context.Background(), conflict.ID, 1, "keep_both")
			if err != nil {
				t.Fatal(err)
			}
			check, err := m.CheckResolution(context.Background(), decision.ID)
			if err != nil || check.Status != "verified" {
				t.Fatal(check, err)
			}
			execution, err := m.StartResolution(decision.ID, check.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := waitRun(t, m, c.ID, execution.ID); got.Status != "verification_pending" {
				t.Fatal(got)
			}
			if scenario == "unrelated_conflict" {
				os.WriteFile(filepath.Join(root, "b.txt"), []byte("left"), 0600)
				f.entries = append(f.entries, Entry{ID: "other", Path: "b.txt", Size: 5, SHA256: sum("right")})
			}
			if scenario == "chosen_file_changed" {
				os.WriteFile(filepath.Join(root, "a.txt"), []byte("edited"), 0600)
			}
			if scenario == "link_persistence_failure" {
				if _, err = m.db.Exec(`CREATE TRIGGER fail_verification_link BEFORE INSERT ON sync_resolution_verifications BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
					t.Fatal(err)
				}
			}
			var before string
			if err = m.db.QueryRow(`SELECT run_id FROM sync_baselines WHERE config_id=?`, c.ID).Scan(&before); err != nil {
				t.Fatal(err)
			}
			// Wait for the previous worker's deferred lease release, not merely its row.
			for i := 0; i < 100; i++ {
				m.mu.Lock()
				active := m.active
				m.mu.Unlock()
				if !active {
					break
				}
				time.Sleep(time.Millisecond)
			}
			verification, err := m.StartResolutionVerification(decision.ID)
			if err != nil {
				t.Fatal(err)
			}
			done := waitRun(t, m, c.ID, verification.ID)
			var baseline string
			if err = m.db.QueryRow(`SELECT run_id FROM sync_baselines WHERE config_id=?`, c.ID).Scan(&baseline); err != nil {
				t.Fatal(err)
			}
			if scenario == "ready" {
				if done.Status != "complete" || baseline != verification.ID {
					t.Fatalf("verification %+v baseline %s", done, baseline)
				}
				link, err := m.ResolutionVerification(context.Background(), decision.ID)
				if err != nil || link.ID != verification.ID {
					t.Fatal("completion link missing")
				}
				m.Close()
				m, err = Open(dir, f, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer m.Close()
				read, err := m.ResolutionVerification(context.Background(), decision.ID)
				if err != nil || read.ID != verification.ID || read.Status != "complete" {
					t.Fatal("completion lost on reopen")
				}
				repeat, err := m.StartResolutionVerification(decision.ID)
				if err != nil || repeat.ID != verification.ID {
					t.Fatal("completed verification rescanned on repeat")
				}
			} else if (scenario != "link_persistence_failure" && done.Status != "conflict" || scenario == "link_persistence_failure" && done.Status != "failed") || baseline != before {
				t.Fatalf("partial resolution committed scope: %+v baseline %s", done, baseline)
			}
			if f.calls != 0 {
				t.Fatal("verification performed a remote write")
			}
		})
	}
}
