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
)

func TestResolutionExecutionUsesDurableStepsAndNeverReplays(t *testing.T) {
	for _, scenario := range []string{"keep_remote", "keep_both", "changed_after_check", "recovered_intent", "partial_download_failure", "remote_unknown"} {
		t.Run(scenario, func(t *testing.T) {
			choice := scenario
			if scenario == "partial_download_failure" || scenario == "remote_unknown" {
				choice = "keep_both"
			}
			if choice != "keep_both" {
				choice = "keep_remote"
			}
			root := t.TempDir()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("left"), 0600); err != nil {
				t.Fatal(err)
			}
			f := &resolutionTestRemote{fixtureRemote: fixtureRemote{payload: "right", entries: []Entry{{ID: "remote", Path: "a.txt", Size: 5, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("right")))}}}, failDownload: scenario == "partial_download_failure"}
			m, err := Open(dir, f, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { m.Close() })
			mode := "pull"
			if scenario == "remote_unknown" {
				mode = "both"
			}
			c, err := m.Create(context.Background(), Config{Name: "conflict", Mode: mode, LocalRoot: root, RemoteRoot: "/fixture"})
			if err != nil {
				t.Fatal(err)
			}
			original, err := m.Start(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := waitRun(t, m, c.ID, original.ID); got.Status != "conflict" {
				t.Fatalf("source %+v", got)
			}
			decision, err := m.CreateResolution(context.Background(), original.ID, 1, choice)
			if err != nil {
				t.Fatal(err)
			}
			check, err := m.CheckResolution(context.Background(), decision.ID)
			if err != nil || check.Status != "verified" {
				t.Fatalf("check %+v %v", check, err)
			}
			if scenario == "recovered_intent" {
				admitted := Run{ID: newID(), ConfigID: c.ID, Status: "running"}
				if _, err = m.db.Exec(`INSERT INTO sync_runs VALUES(?,?,?,?)`, admitted.ID, c.ID, "running", encoded(admitted)); err != nil {
					t.Fatal(err)
				}
				if _, err = m.db.Exec(`INSERT INTO sync_resolution_executions VALUES(?,?)`, decision.ID, admitted.ID); err != nil {
					t.Fatal(err)
				}
				if err = m.saveStep(admitted.ID, StepRecord{Ordinal: 1, Status: "intent", Step: Step{Action: "download", Path: "a.txt"}}); err != nil {
					t.Fatal(err)
				}
				m.Close()
				m, err = Open(dir, f, nil)
				if err != nil {
					t.Fatal(err)
				}
				read, err := m.StartResolution(decision.ID, check.ID)
				if err != nil || read.ID != admitted.ID || read.Status != "interrupted" {
					t.Fatal("interrupted admission replayed")
				}
				steps, err := m.Steps(admitted.ID)
				if err != nil || len(steps) != 1 || steps[0].Status != "unknown" {
					t.Fatal("intent was not quarantined")
				}
				if _, err = m.Start(c.ID); err == nil {
					t.Fatal("unknown resolution allowed new sync")
				}
				content, err := os.ReadFile(filepath.Join(root, "a.txt"))
				if err != nil || string(content) != "left" {
					t.Fatal("recovery wrote file")
				}
				return
			}
			if scenario == "changed_after_check" {
				if err = os.WriteFile(filepath.Join(root, "a.txt"), []byte("edited"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			request := httptest.NewRequest("POST", "/execution", strings.NewReader(fmt.Sprintf(`{"check_id":%q,"confirm":false}`, check.ID)))
			request.SetPathValue("decision", decision.ID)
			response := httptest.NewRecorder()
			m.HandleResolutionExecution(response, request)
			if response.Code != 400 {
				t.Fatal("unconfirmed decision was admitted")
			}
			var admissions int
			if err = m.db.QueryRow(`SELECT count(*) FROM sync_resolution_executions WHERE decision_id=?`, decision.ID).Scan(&admissions); err != nil || admissions != 0 {
				t.Fatal("invalid HTTP confirmation changed admission")
			}
			execution, err := m.StartResolution(decision.ID, check.ID)
			if err != nil {
				t.Fatal(err)
			}
			done := waitRun(t, m, c.ID, execution.ID)
			if scenario == "partial_download_failure" || scenario == "remote_unknown" {
				want := "failed"
				confirmed := 1
				if scenario == "remote_unknown" {
					want = "unknown"
					confirmed = 2
				}
				if done.Status != want || done.Confirmed != confirmed {
					t.Fatalf("partial result %+v", done)
				}
				content, e := os.ReadFile(filepath.Join(root, decision.LocalCopy))
				if e != nil || string(content) != "left" {
					t.Fatal("partial execution lost original local content")
				}
				steps, e := m.Steps(execution.ID)
				if e != nil || steps[len(steps)-1].Status != want {
					t.Fatalf("partial steps %+v %v", steps, e)
				}
				read, e := m.StartResolution(decision.ID, check.ID)
				if e != nil || read.ID != execution.ID {
					t.Fatal("partial execution replayed")
				}
				if scenario == "remote_unknown" {
					if _, e = m.Start(c.ID); e == nil {
						t.Fatal("unknown partial write did not block new sync")
					}
				}
				return
			}
			if scenario == "changed_after_check" {
				if done.Status != "conflict" || done.Confirmed != 0 {
					t.Fatalf("stale check executed %+v", done)
				}
				content, err := os.ReadFile(filepath.Join(root, "a.txt"))
				if err != nil || string(content) != "edited" {
					t.Fatal("stale source overwritten")
				}
				read, err := m.StartResolution(decision.ID, check.ID)
				if err != nil || read.ID != execution.ID {
					t.Fatal("failed decision admitted twice")
				}
				return
			}
			if done.Status != "verification_pending" {
				t.Fatalf("execution %+v", done)
			}
			content, err := os.ReadFile(filepath.Join(root, "a.txt"))
			if err != nil || string(content) != "right" {
				t.Fatal("remote version not applied")
			}
			if choice == "keep_both" {
				content, err = os.ReadFile(filepath.Join(root, decision.LocalCopy))
				if err != nil || string(content) != "left" {
					t.Fatal("local version not retained")
				}
			}
			steps, err := m.Steps(execution.ID)
			if err != nil || len(steps) != len(decision.Effects) {
				t.Fatalf("steps %+v %v", steps, err)
			}
			for _, step := range steps {
				if step.Status != "confirmed" {
					t.Fatalf("step %+v", step)
				}
			}
			var count int
			if err = m.db.QueryRow(`SELECT count(*) FROM sync_baselines WHERE config_id=?`, c.ID).Scan(&count); err != nil || count != 0 {
				t.Fatal("resolution advanced baseline before new sync")
			}
			replay, err := m.StartResolution(decision.ID, check.ID)
			if err != nil || replay.ID != execution.ID {
				t.Fatal("duplicate admission replayed effects")
			}
			m.Close()
			m, err = Open(dir, f, nil)
			if err != nil {
				t.Fatal(err)
			}
			replay, err = m.StartResolution(decision.ID, check.ID)
			if err != nil || replay.ID != execution.ID || replay.Status != "verification_pending" {
				t.Fatal("restart replayed resolution")
			}
		})
	}
}

type resolutionTestRemote struct {
	fixtureRemote
	failDownload bool
}

func (f *resolutionTestRemote) Download(ctx context.Context, c Config, e Entry, target string) error {
	if f.failDownload {
		return fmt.Errorf("injected download failure before publication")
	}
	return f.fixtureRemote.Download(ctx, c, e, target)
}
