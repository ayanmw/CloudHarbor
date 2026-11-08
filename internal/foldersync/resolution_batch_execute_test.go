package foldersync

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBatchExecutionReservesOnceRunsSeriallyAndStopsFailure(t *testing.T) {
	for _, scenario := range []string{"complete", "failed_second", "changed_after_check", "unknown_second", "admission_conflict", "recovered_intent"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			dir := t.TempDir()
			for _, name := range []string{"a", "b", "c"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte("left"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			digest := fmt.Sprintf("%x", sha256.Sum256([]byte("right")))
			f := &batchExecutionRemote{fixtureRemote: fixtureRemote{payload: "right", entries: []Entry{{ID: "ra", Path: "a", Size: 5, SHA256: digest}, {ID: "rb", Path: "b", Size: 5, SHA256: digest}, {ID: "rc", Path: "c", Size: 5, SHA256: digest}}}}
			m, err := Open(dir, f, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			cfg, err := m.Create(context.Background(), Config{Name: "execute", Mode: "both", LocalRoot: root, RemoteRoot: "/fixture"})
			if err != nil {
				t.Fatal(err)
			}
			source, err := m.Start(cfg.ID)
			if err != nil {
				t.Fatal(err)
			}
			_ = waitRun(t, m, cfg.ID, source.ID)
			ids := []string{}
			for i := 1; i <= 3; i++ {
				d, e := m.CreateResolution(context.Background(), source.ID, i, "keep_remote")
				if e != nil {
					t.Fatal(e)
				}
				ids = append(ids, d.ID)
			}
			batch, err := m.CreateResolutionBatch(context.Background(), source.ID, ids)
			if err != nil {
				t.Fatal(err)
			}
			check, err := m.CheckResolutionBatch(context.Background(), batch.ID)
			if err != nil || check.Status != "verified" {
				t.Fatal(check, err)
			}
			if scenario == "failed_second" {
				f.fail = "b"
			}
			if scenario == "changed_after_check" {
				f.entries[1].ID = "changed"
			}
			if scenario == "admission_conflict" {
				if _, e := m.db.Exec(`INSERT INTO sync_resolution_executions VALUES(?,?)`, ids[1], newID()); e != nil {
					t.Fatal(e)
				}
				if _, e := m.StartResolutionBatch(batch.ID, check.ID); e == nil {
					t.Fatal("conflicting reservation accepted")
				}
				var n int
				if e := m.db.QueryRow(`SELECT count(*) FROM sync_resolution_batch_executions WHERE batch_id=?`, batch.ID).Scan(&n); e != nil || n != 0 {
					t.Fatal("partial batch admission", n, e)
				}
				if e := m.db.QueryRow(`SELECT count(*) FROM sync_resolution_executions WHERE decision_id=?`, ids[0]).Scan(&n); e != nil || n != 0 || len(f.downloads) != 0 {
					t.Fatal("partial child admission", n, e)
				}
				return
			}
			if scenario == "unknown_second" {
				if _, e := m.db.Exec(`CREATE TRIGGER batch_receipt_failure BEFORE UPDATE ON sync_steps WHEN NEW.status='confirmed' AND json_extract(NEW.body,'$.plan.path')='b' BEGIN SELECT RAISE(ABORT,'injected confirmation failure'); END;`); e != nil {
					t.Fatal(e)
				}
			}
			var execution ResolutionBatchExecution
			if scenario == "complete" {
				request := httptest.NewRequest("POST", "/batch/execution", strings.NewReader(`{"check_id":"`+check.ID+`","confirm":false}`))
				request.SetPathValue("batch", batch.ID)
				response := httptest.NewRecorder()
				m.HandleResolutionBatchExecution(response, request)
				if response.Code != 400 || len(f.downloads) != 0 {
					t.Fatal("confirmation missing")
				}
				request = httptest.NewRequest("POST", "/batch/execution", strings.NewReader(`{"check_id":"`+check.ID+`","confirm":true}`))
				request.SetPathValue("batch", batch.ID)
				response = httptest.NewRecorder()
				m.HandleResolutionBatchExecution(response, request)
				if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &execution) != nil {
					t.Fatal("execution API", response.Code, response.Body.String())
				}
			} else {
				execution, err = m.StartResolutionBatch(batch.ID, check.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			again, err := m.StartResolutionBatch(batch.ID, check.ID)
			if err != nil || again.Run.ID != execution.Run.ID {
				t.Fatal("duplicate batch admission", again, err)
			}
			// Full-suite disk contention exposed the old 2.5s polling limit while
			// the batch was still running. Reuse the terminal/worker-idle gate so
			// receipt and restart assertions observe a finished coordinator.
			_ = waitRun(t, m, cfg.ID, execution.Run.ID)
			execution, err = m.ResolutionBatchExecution(context.Background(), batch.ID)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "complete", "recovered_intent":
				if execution.Run.Status != "verification_pending" || execution.Run.Confirmed != 3 || len(f.downloads) != 3 {
					t.Fatal(execution, f.downloads)
				}
			case "failed_second":
				if execution.Run.Status != "failed" || execution.Run.Confirmed != 1 || len(f.downloads) != 2 || execution.Items[2].Run.Status != "interrupted" {
					t.Fatal(execution, f.downloads)
				}
			case "unknown_second":
				if execution.Run.Status != "unknown" || execution.Run.Confirmed != 1 || len(f.downloads) != 2 || execution.Items[2].Run.Status != "interrupted" {
					t.Fatal(execution, f.downloads)
				}
				body, e := os.ReadFile(filepath.Join(root, "b"))
				if e != nil || string(body) != "right" {
					t.Fatal("unknown write disappeared", string(body), e)
				}
			case "changed_after_check":
				if execution.Run.Status != "conflict" || len(f.downloads) != 0 {
					t.Fatal(execution, f.downloads)
				}
			}
			var baselines int
			if e := m.db.QueryRow(`SELECT count(*) FROM sync_baselines WHERE config_id=?`, cfg.ID).Scan(&baselines); e != nil || baselines != 0 {
				t.Fatal("batch advanced baseline", baselines, e)
			}
			if scenario == "recovered_intent" {
				// Model the durable state of a crash after a visible write but before its
				// receipt: startup recovery must isolate it rather than call Download.
				parent := execution.Run
				parent.Status = "running"
				parent.Finished = nil
				parent.Confirmed = 2
				child := execution.Items[2].Run
				child.Status = "running"
				child.Finished = nil
				child.Confirmed = 0
				for _, run := range []Run{parent, child} {
					if _, e := m.db.Exec(`UPDATE sync_runs SET status=?,body=? WHERE id=?`, run.Status, encoded(run), run.ID); e != nil {
						t.Fatal(e)
					}
				}
				records, e := m.Steps(child.ID)
				if e != nil || len(records) != 1 {
					t.Fatal(records, e)
				}
				records[0].Status = "intent"
				records[0].Result = nil
				if e = m.saveStep(child.ID, records[0]); e != nil {
					t.Fatal(e)
				}
				execution.Run.Status = "interrupted"
			}
			if err = m.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, e := Open(dir, f, nil)
			if e != nil {
				t.Fatal(e)
			}
			defer reopened.Close()
			restored, e := reopened.StartResolutionBatch(batch.ID, check.ID)
			if e != nil || restored.Run.ID != execution.Run.ID || restored.Run.Status != execution.Run.Status || len(f.downloads) > 3 {
				t.Fatal("restart replay", restored, e)
			}
			if scenario == "recovered_intent" {
				records, e := reopened.Steps(restored.Items[2].Run.ID)
				if e != nil || records[0].Status != "unknown" || restored.Items[2].Run.Status != "interrupted" {
					t.Fatal("crash intent not isolated", records, e)
				}
			}

		})
	}
}

type batchExecutionRemote struct {
	fixtureRemote
	fail      string
	downloads []string
}

func (f *batchExecutionRemote) Download(ctx context.Context, c Config, e Entry, target string) error {
	f.downloads = append(f.downloads, e.Path)
	if e.Path == f.fail {
		return &NotStartedError{fmt.Errorf("injected download failure")}
	}
	return f.fixtureRemote.Download(ctx, c, e, target)
}
