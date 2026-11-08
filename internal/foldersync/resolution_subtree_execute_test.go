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

func TestSubtreeWorkerPersistsOrderedStepsAndStopsPartialFailure(t *testing.T) {
	for _, scenario := range []string{"complete", "download_failure", "changed_before", "between_steps", "after_write", "delete_tree", "verified_child_changed", "unrelated_conflict"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			folder := filepath.Join(root, "folder")
			if err := os.Mkdir(folder, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(folder, "child"), []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			f := &subtreeWorkerRemote{resolutionTestRemote: resolutionTestRemote{fixtureRemote: fixtureRemote{payload: "right", entries: []Entry{{ID: "dir", Path: "folder", Directory: true}, {ID: "child", Path: "folder/child", Size: 3, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("old")))}}}}}
			storeDir := t.TempDir()
			m, err := Open(storeDir, f, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			c, err := m.Create(context.Background(), Config{Name: "worker", Mode: "both", LocalRoot: root, RemoteRoot: "/fixture"})
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
			if scenario == "delete_tree" {
				if err = os.WriteFile(filepath.Join(folder, "child"), []byte("right"), 0600); err != nil {
					t.Fatal(err)
				}
				f.entries = []Entry{}
			} else {
				if err = os.Remove(filepath.Join(folder, "child")); err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(folder); err != nil {
					t.Fatal(err)
				}
				f.entries[1].Size = 5
				f.entries[1].SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte("right")))
			}
			source, err := m.Start(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := waitRun(t, m, c.ID, source.ID); got.Status != "conflict" {
				t.Fatal(got)
			}
			d, err := m.CreateResolution(context.Background(), source.ID, 1, "keep_remote")
			if err != nil {
				t.Fatal(err)
			}
			d, err = m.Resolution(context.Background(), d.ID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "download_failure" {
				f.failDownload = true
			}
			if scenario == "between_steps" || scenario == "after_write" {
				f.inject = true
				f.scans = 0
				f.injectAt = 2
				if scenario == "after_write" {
					f.injectAt = 3
				}
			}
			if scenario == "changed_before" {
				f.entries[1].ID = "replaced"
			}
			run := Run{ID: newID(), ConfigID: c.ID, Status: "running", Started: time.Now().UTC(), StepCount: 2}
			if scenario == "delete_tree" {
				run.StepCount = 1
			}
			if scenario == "complete" {
				check, e := m.CheckResolution(context.Background(), d.ID)
				if e != nil || check.Status != "verified" || check.ExecutionSupported == nil || !*check.ExecutionSupported {
					t.Fatal(check, e)
				}
				run, e = m.StartResolution(d.ID, check.ID)
				if e != nil {
					t.Fatal(e)
				}
				repeat, e := m.StartResolution(d.ID, check.ID)
				if e != nil || repeat.ID != run.ID {
					t.Fatal("duplicate admission", repeat, e)
				}
			} else {
				if _, err = m.db.Exec(`INSERT INTO sync_runs VALUES(?,?,?,?)`, run.ID, c.ID, run.Status, encoded(run)); err != nil {
					t.Fatal(err)
				}
				if _, err = m.db.Exec(`INSERT INTO sync_resolution_executions VALUES(?,?)`, d.ID, run.ID); err != nil {
					t.Fatal(err)
				}
				m.executeSubtreeResolution(d, run)
			}
			got := waitRun(t, m, c.ID, run.ID)
			switch scenario {
			case "complete", "verified_child_changed", "unrelated_conflict":
				if got.Status != "verification_pending" || got.Confirmed != 2 {
					t.Fatal(got)
				}
				body, e := os.ReadFile(filepath.Join(folder, "child"))
				if e != nil || string(body) != "right" {
					t.Fatal(string(body), e)
				}
			case "delete_tree":
				if got.Status != "verification_pending" || got.Confirmed != 1 {
					t.Fatal(got)
				}
				if _, e := os.Stat(folder); !os.IsNotExist(e) {
					t.Fatal("removed tree still present", e)
				}
				body, e := os.ReadFile(filepath.Join(root, privateDirectory, run.ID, "archive", "folder", "child"))
				if e != nil || string(body) != "right" {
					t.Fatal("old tree not preserved", e)
				}
			case "download_failure":
				if got.Status != "failed" || got.Confirmed != 1 {
					t.Fatal(got)
				}
				if _, e := os.Stat(folder); e != nil {
					t.Fatal("confirmed mkdir lost", e)
				}
			case "after_write":
				if got.Status != "unknown" || got.Confirmed != 0 {
					t.Fatal(got)
				}
				if _, e := os.Stat(folder); e != nil {
					t.Fatal("created directory lost", e)
				}
			case "changed_before", "between_steps":
				if got.Status != "conflict" || got.Confirmed != 0 {
					t.Fatal(got)
				}
				if _, e := os.Stat(folder); !os.IsNotExist(e) {
					t.Fatal("unexpected local write", e)
				}
			}
			var confirmed, unknown int
			if err = m.db.QueryRow(`SELECT count(*) FROM sync_steps WHERE run_id=? AND status='confirmed'`, run.ID).Scan(&confirmed); err != nil || confirmed != got.Confirmed {
				t.Fatal("confirmation receipts", confirmed, err)
			}
			if err = m.db.QueryRow(`SELECT count(*) FROM sync_steps WHERE run_id=? AND status='unknown'`, run.ID).Scan(&unknown); err != nil || (scenario == "after_write") != (unknown == 1) {
				t.Fatal("unknown evidence", unknown, err)
			}
			var baseline string
			if err = m.db.QueryRow(`SELECT run_id FROM sync_baselines WHERE config_id=?`, c.ID).Scan(&baseline); err != nil || baseline != initial.ID {
				t.Fatal("worker advanced baseline", baseline, err)
			}
			if err = m.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, e := Open(storeDir, f, nil)
			if e != nil {
				t.Fatal(e)
			}
			defer reopened.Close()
			restored := waitRun(t, reopened, c.ID, run.ID)
			if restored.Status != got.Status || restored.Confirmed != got.Confirmed {
				t.Fatal("restart changed outcome", restored, got)
			}
			if got.Status == "verification_pending" {
				if scenario == "verified_child_changed" {
					f.entries[1].ID = "replaced-after-execution"
				}
				if scenario == "unrelated_conflict" {
					f.entries = append(f.entries, Entry{Path: "unrelated", ID: "other", Size: 5, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("right")))})
				}
				verification, e := reopened.StartResolutionVerification(d.ID)
				if e != nil {
					t.Fatal(e)
				}
				outcome := waitRun(t, reopened, c.ID, verification.ID)
				want := "complete"
				if scenario == "verified_child_changed" || scenario == "unrelated_conflict" {
					want = "conflict"
				}
				if outcome.Status != want {
					t.Fatal("full scope verification", outcome)
				}
				if err = reopened.db.QueryRow(`SELECT run_id FROM sync_baselines WHERE config_id=?`, c.ID).Scan(&baseline); err != nil {
					t.Fatal(err)
				}
				if want == "complete" && baseline != verification.ID || want != "complete" && baseline != initial.ID {
					t.Fatal("baseline gate", baseline)
				}
				if want == "complete" {
					linked, e := reopened.ResolutionVerification(context.Background(), d.ID)
					if e != nil || linked.ID != verification.ID {
						t.Fatal("missing completion link", linked, e)
					}
				}
			}

		})
	}
}

type subtreeWorkerRemote struct {
	resolutionTestRemote
	inject   bool
	scans    int
	injectAt int
}

func (f *subtreeWorkerRemote) Scan(ctx context.Context, c Config, cache []Entry) ([]Entry, error) {
	f.scans++
	if f.inject && f.scans == f.injectAt {
		f.entries = append(f.entries, Entry{ID: "unplanned", Path: "other", Size: 5, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("right")))})
	}
	// A successful complete empty traversal uses [], matching production Scan.
	// The older shared fixture returns nil for no entries, which means missing
	// traversal evidence to immutable conflict previews.
	entries, err := f.fixtureRemote.Scan(ctx, c, cache)
	if err == nil && entries == nil {
		entries = []Entry{}
	}
	return entries, err
}
