package foldersync

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestConflictDecisionPreservesDirectionAndImmutableEvidence(t *testing.T) {
	for _, mode := range []string{"pull", "both"} {
		t.Run(mode, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "state")
			m, err := Open(dir, &fixtureRemote{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { m.Close() })
			run := newID()
			c := Config{ID: newID(), Account: "account-app", Mode: mode, LocalRoot: "D:/left", RemoteRoot: "/right"}
			local := Entry{Path: "a.txt", ID: "local", Size: 3, SHA256: strings.Repeat("a", 64)}
			remote := Entry{Path: "a.txt", ID: "remote", Size: 3, SHA256: strings.Repeat("b", 64)}
			if err = m.event(run, "run_started", c); err != nil {
				t.Fatal(err)
			}
			if err = m.event(run, "snapshot_before", Snapshot{Local: []Entry{local}, Remote: []Entry{remote}}); err != nil {
				t.Fatal(err)
			}
			if err = m.saveStep(run, StepRecord{Ordinal: 1, Status: "conflict", Step: Step{Action: "conflict", Path: "a.txt", Local: &local, Remote: &remote}}); err != nil {
				t.Fatal(err)
			}
			before, _ := m.Events(run)
			saved, err := m.CreateResolution(context.Background(), run, 1, "keep_both")
			if err != nil {
				t.Fatal(err)
			}
			if saved.ConflictSequence == "" || saved.Choice != "keep_both" || saved.LocalCopy == "" || saved.State != "preview" {
				t.Fatalf("decision %+v", saved)
			}
			if mode == "pull" {
				for _, effect := range saved.Effects {
					if effect.Side == "remote" {
						t.Fatal("pull preview uploads")
					}
				}
			}
			kept, err := m.CreateResolution(context.Background(), run, 1, "keep_local")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "pull" && (len(kept.Effects) != 0 || kept.State != "unresolved") {
				t.Fatalf("pull local %+v", kept)
			}
			skipped, err := m.CreateResolution(context.Background(), run, 1, "skip")
			if err != nil || len(skipped.Effects) != 0 {
				t.Fatal("skip planned writes")
			}
			request := httptest.NewRequest("POST", "/api/sync/runs/"+run+"/resolutions", strings.NewReader(`{"ordinal":1,"choice":"keep_remote"}`))
			request.SetPathValue("run", run)
			response := httptest.NewRecorder()
			m.HandleResolution(response, request)
			var viaHTTP Resolution
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &viaHTTP) != nil || len(viaHTTP.Effects) != 1 || viaHTTP.Effects[0].Action != "download" || !viaHTTP.Effects[0].Backup {
				t.Fatalf("remote preview %d", response.Code)
			}
			if viaHTTP.SnapshotSequence == "" || len(viaHTTP.SnapshotSHA256) != 64 || viaHTTP.SnapshotRecorded.IsZero() {
				t.Fatal("snapshot evidence missing")
			}
			request = httptest.NewRequest("GET", "/api/sync/runs/"+run+"/resolutions?ordinal=1", nil)
			request.SetPathValue("run", run)
			response = httptest.NewRecorder()
			m.HandleResolution(response, request)
			var history struct {
				Decisions []Resolution `json:"decisions"`
				More      bool         `json:"has_more"`
			}
			if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &history) != nil || len(history.Decisions) != 4 || history.More {
				t.Fatalf("decision history %d %s", response.Code, response.Body.String())
			}
			after, _ := m.Events(run)
			if len(after) != len(before) {
				t.Fatal("decision rewrote sync history")
			}
			if _, err = m.db.Exec(`UPDATE sync_resolutions SET body='{}' WHERE id=?`, saved.ID); err == nil {
				t.Fatal("mutable decision")
			}
			if _, err = m.CreateResolution(context.Background(), run, 1, "newest"); err == nil {
				t.Fatal("accepted implicit newest")
			}
			m.Close()
			m, err = Open(dir, &fixtureRemote{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			reread, err := m.Resolution(context.Background(), saved.ID)
			a, _ := json.Marshal(saved)
			b, _ := json.Marshal(reread)
			if err != nil || string(a) != string(b) {
				t.Fatal("decision changed on reopen")
			}
		})
	}
}
