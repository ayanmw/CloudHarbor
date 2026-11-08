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

func TestBatchCheckUsesOneObservationAndPreservesEveryResult(t *testing.T) {
	for _, scenario := range []string{"matching", "one_changed", "copy_occupied", "incomplete", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			dir := t.TempDir()
			for _, name := range []string{"a", "b"} {
				if err := os.WriteFile(filepath.Join(root, name), []byte("left"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			digest := fmt.Sprintf("%x", sha256.Sum256([]byte("right")))
			remote := &batchCheckRemote{fixtureRemote: fixtureRemote{entries: []Entry{{Path: "a", ID: "ra", Size: 5, SHA256: digest}, {Path: "b", ID: "rb", Size: 5, SHA256: digest}}}}
			m, err := Open(dir, remote, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			c, err := m.Create(context.Background(), Config{Name: "batchcheck", Mode: "both", LocalRoot: root, RemoteRoot: "/fixture"})
			if err != nil {
				t.Fatal(err)
			}
			source, err := m.Start(c.ID)
			if err != nil {
				t.Fatal(err)
			}
			_ = waitRun(t, m, c.ID, source.ID)
			one, err := m.CreateResolution(context.Background(), source.ID, 1, "keep_remote")
			if err != nil {
				t.Fatal(err)
			}
			two, err := m.CreateResolution(context.Background(), source.ID, 2, "keep_both")
			if err != nil {
				t.Fatal(err)
			}
			batch, err := m.CreateResolutionBatch(context.Background(), source.ID, []string{one.ID, two.ID})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "one_changed" {
				remote.entries[1].ID = "replaced"
			}
			if scenario == "copy_occupied" {
				if err = os.WriteFile(filepath.Join(root, two.LocalCopy), []byte("occupied"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			remote.scans = 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "incomplete" {
				remote.incomplete = true
			}
			if scenario == "cancelled" {
				remote.onScan = cancel
			}
			check, err := m.CheckResolutionBatch(ctx, batch.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := "verified"
			if scenario == "one_changed" || scenario == "copy_occupied" {
				want = "rejected"
			}
			if scenario == "incomplete" || scenario == "cancelled" {
				want = "failed"
			}
			if check.Status != want || len(check.Items) != 2 || remote.scans != 1 || remote.trace != check.ID || remote.calls != 0 {
				t.Fatalf("check %+v scans %d trace %s", check, remote.scans, remote.trace)
			}
			if want == "failed" {
				if check.Snapshot != nil || check.Items[0].Status != "not_checked" || check.Finished.IsZero() {
					t.Fatal("incomplete observation invented results", check)
				}
			} else {
				if check.Snapshot == nil || check.Items[0].Status != "verified" {
					t.Fatal(check)
				}
				if want == "rejected" && check.Items[1].Status != "rejected" {
					t.Fatal("individual rejection missing")
				}
			}
			if scenario == "matching" {
				r := httptest.NewRequest("GET", "/checks/"+check.ID, nil)
				r.SetPathValue("check", check.ID)
				w := httptest.NewRecorder()
				m.HandleResolutionBatchCheck(w, r)
				if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("check API", w.Code)
				}
				r = httptest.NewRequest("POST", "/batches/"+batch.ID+"/checks", strings.NewReader(`{"execute":true}`))
				r.SetPathValue("batch", batch.ID)
				w = httptest.NewRecorder()
				m.HandleResolutionBatchCheck(w, r)
				if w.Code != 400 || remote.scans != 1 {
					t.Fatal("nonempty check body accepted")
				}
			}
			pending := ResolutionBatchCheck{ID: newID(), BatchID: batch.ID, Status: "incomplete", Reason: "awaiting_completion", Started: time.Now().UTC(), Items: []ResolutionBatchCheckItem{}}
			if _, e := m.db.Exec(`INSERT INTO sync_resolution_batch_check_intents VALUES(?,?,?)`, pending.ID, batch.ID, encoded(pending)); e != nil {
				t.Fatal(e)
			}

			if _, err = m.db.Exec(`UPDATE sync_resolution_batch_checks SET body='{}' WHERE id=?`, check.ID); err == nil {
				t.Fatal("mutable check")
			}
			if err = m.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, e := Open(dir, remote, nil)
			if e != nil {
				t.Fatal(e)
			}
			defer reopened.Close()
			stored, e := reopened.ResolutionBatchCheck(context.Background(), check.ID)
			if e != nil || encoded(stored) != encoded(check) {
				t.Fatal("restart changed check", e)
			}
			interrupted, e := reopened.ResolutionBatchCheck(context.Background(), pending.ID)
			if e != nil || interrupted.Status != "incomplete" || !interrupted.Finished.IsZero() || remote.scans != 1 {
				t.Fatal("restart fabricated completion or rescanned", interrupted, e)
			}
			history, more, e := reopened.ResolutionBatchChecks(context.Background(), batch.ID, 0)
			if e != nil || more || len(history) != 2 || history[1].Snapshot != nil {
				t.Fatal("history should omit complete snapshot", history, e)
			}

		})
	}
}

type batchCheckRemote struct {
	fixtureRemote
	scans      int
	trace      string
	incomplete bool
	onScan     func()
}

func (f *batchCheckRemote) Scan(ctx context.Context, c Config, cache []Entry) ([]Entry, error) {
	f.scans++
	f.trace = ContextRunID(ctx)
	if f.onScan != nil {
		f.onScan()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if f.incomplete {
		return nil, nil
	}
	return f.fixtureRemote.Scan(ctx, c, cache)
}
