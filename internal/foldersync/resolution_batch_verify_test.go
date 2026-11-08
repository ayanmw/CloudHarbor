package foldersync

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBatchVerificationRequiresEveryOutputAndAtomicLink(t *testing.T) {
	for _, scenario := range []string{"ready", "second_changed_equally", "link_failure", "already_linked", "single_route_changed"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			dir := t.TempDir()
			for _, name := range []string{"a", "b"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte("left"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			digest := fmt.Sprintf("%x", sha256.Sum256([]byte("right")))
			f := &batchExecutionRemote{fixtureRemote: fixtureRemote{payload: "right", entries: []Entry{{ID: "ra", Path: "a", Size: 5, SHA256: digest}, {ID: "rb", Path: "b", Size: 5, SHA256: digest}}}}
			m, e := Open(dir, f, nil)
			if e != nil {
				t.Fatal(e)
			}
			defer m.Close()
			cfg, e := m.Create(context.Background(), Config{Name: "batchverify", Mode: "both", LocalRoot: root, RemoteRoot: "/fixture"})
			if e != nil {
				t.Fatal(e)
			}
			source, e := m.Start(cfg.ID)
			if e != nil {
				t.Fatal(e)
			}
			_ = waitRun(t, m, cfg.ID, source.ID)
			ids := []string{}
			for i := 1; i <= 2; i++ {
				d, e := m.CreateResolution(context.Background(), source.ID, i, "keep_remote")
				if e != nil {
					t.Fatal(e)
				}
				ids = append(ids, d.ID)
			}
			batch, e := m.CreateResolutionBatch(context.Background(), source.ID, ids)
			if e != nil {
				t.Fatal(e)
			}
			check, e := m.CheckResolutionBatch(context.Background(), batch.ID)
			if e != nil {
				t.Fatal(e)
			}
			exec, e := m.StartResolutionBatch(batch.ID, check.ID)
			if e != nil {
				t.Fatal(e)
			}
			_ = waitRun(t, m, cfg.ID, exec.Run.ID)
			// Wait for the coordinator's manager reservation to be released.
			for i := 0; i < 100; i++ {
				m.mu.Lock()
				active := m.active
				m.mu.Unlock()
				if !active {
					break
				}
				time.Sleep(time.Millisecond)
			}
			var priorLink string
			if scenario == "already_linked" {
				// Model a historical single-item completion written before batch-aware
				// routing existed, without bypassing the current public admission policy.
				single := Run{ID: newID(), ConfigID: cfg.ID, Status: "running", Started: time.Now().UTC()}
				if _, e = m.db.Exec(`INSERT INTO sync_runs VALUES(?,?,?,?)`, single.ID, cfg.ID, single.Status, encoded(single)); e != nil {
					t.Fatal(e)
				}
				m.verifyResolutionScope(cfg, ids[0], single)
				if run := waitRun(t, m, cfg.ID, single.ID); run.Status != "complete" {
					t.Fatal(run)
				}
				priorLink = single.ID
			}

			if scenario == "second_changed_equally" || scenario == "single_route_changed" {
				if e = os.WriteFile(filepath.Join(root, "b"), []byte("new"), 0600); e != nil {
					t.Fatal(e)
				}
				f.entries[1].Size = 3
				f.entries[1].SHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte("new")))
			}
			if scenario == "link_failure" {
				if _, e = m.db.Exec(`CREATE TRIGGER reject_batch_link BEFORE INSERT ON sync_resolution_batch_verifications BEGIN SELECT RAISE(ABORT,'injected link failure'); END;`); e != nil {
					t.Fatal(e)
				}
			}
			var verification Run
			if scenario == "single_route_changed" {
				verification, e = m.StartResolutionVerification(ids[0])
			} else {
				verification, e = m.StartResolutionBatchVerification(batch.ID)
			}
			if e != nil {
				t.Fatal(e)
			}
			if scenario == "single_route_changed" {
				latest, err := m.LatestResolutionVerification(context.Background(), ids[0])
				if err != nil || latest.ID != verification.ID {
					t.Fatal("single route lost batch verification", latest, err)
				}
			}
			done := waitRun(t, m, cfg.ID, verification.ID)
			want := "complete"
			if scenario == "second_changed_equally" || scenario == "single_route_changed" {
				want = "conflict"
			}
			if scenario == "link_failure" {
				want = "failed"
			}
			if done.Status != want {
				t.Fatal(done)
			}
			var count int
			if e = m.db.QueryRow(`SELECT count(*) FROM sync_resolution_batch_verifications WHERE batch_id=?`, batch.ID).Scan(&count); e != nil || (count == 1) != (want == "complete") {
				t.Fatal("batch completion link", count, e)
			}
			if scenario == "link_failure" {
				var links int
				if err := m.db.QueryRow(`SELECT count(*) FROM sync_resolution_verifications WHERE decision_id IN (?,?)`, ids[0], ids[1]).Scan(&links); err != nil || links != 0 {
					t.Fatal("partial individual completion survived rollback", links, err)
				}
			}
			if want != "complete" {
				if e = m.db.QueryRow(`SELECT count(*) FROM sync_baselines WHERE config_id=?`, cfg.ID).Scan(&count); e != nil || count != 0 {
					t.Fatal("failed verification advanced baseline", count, e)
				}
			}
			request := httptest.NewRequest("GET", "/batch/verification", nil)
			request.SetPathValue("batch", batch.ID)
			response := httptest.NewRecorder()
			m.HandleResolutionBatchVerification(response, request)
			if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("verification read API", response.Code)
			}
			request = httptest.NewRequest("POST", "/batch/verification", strings.NewReader(`{"execute":true}`))
			request.SetPathValue("batch", batch.ID)
			response = httptest.NewRecorder()
			m.HandleResolutionBatchVerification(response, request)
			if response.Code != 400 {
				t.Fatal("invalid verification body", response.Code)
			}
			if want == "complete" {
				again, e := m.StartResolutionBatchVerification(batch.ID)
				if e != nil || again.ID != verification.ID {
					t.Fatal("verification replay", again, e)
				}
				if e = m.Close(); e != nil {
					t.Fatal(e)
				}
				reopened, e := Open(dir, f, nil)
				if e != nil {
					t.Fatal(e)
				}
				defer reopened.Close()
				m = reopened
				restored, e := reopened.StartResolutionBatchVerification(batch.ID)
				if e != nil || restored.ID != verification.ID || restored.Status != "complete" {
					t.Fatal("lost completion after restart", restored, e)
				}
			}
			if scenario == "already_linked" {
				linked, e := m.ResolutionVerification(context.Background(), ids[0])
				if e != nil || linked.ID != priorLink {
					t.Fatal("rewrote prior completion", linked, e)
				}
			}
		})
	}
}
