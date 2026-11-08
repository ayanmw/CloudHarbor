package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/foldersync"
)

func TestFolderSyncRemoteArchiveUsesJournalAndNeverReplaysLostMove(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed", true: "unknown"}[lost], func(t *testing.T) {
			d, p, calls, _, lose := intakeFixture(t)
			d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
			c := foldersync.Config{ID: strings.Repeat("a", 32), Mode: "both", RemoteRoot: "/sync", RemoteID: "70", Account: syncAccountScope(d.getClient())}
			run := strings.Repeat("b", 32)
			p.lists["/"] = []baiduapi.FileInfo{{FsID: 70, Path: "/sync", ServerFilename: "sync", IsDir: 1}}
			source := baiduapi.FileInfo{FsID: 71, Path: "/sync/folder", ServerFilename: "folder", IsDir: 1, Mtime: 123}
			p.lists["/sync"] = []baiduapi.FileInfo{source}
			parent := "/sync"
			for i, part := range []string{".nas-sync-history", c.ID, run} {
				child := path.Join(parent, part)
				p.lists[parent] = append(p.lists[parent], baiduapi.FileInfo{FsID: int64(80 + i), Path: child, ServerFilename: part, IsDir: 1})
				parent = child
			}
			lose.Store(lost)
			events := []string{}
			log := func(kind string, value any) error { events = append(events, kind); return nil }
			step := foldersync.Step{Action: "remote_archive", Path: "folder", Remote: &foldersync.Entry{ID: "71", Path: "folder", Directory: true, Mtime: 123, MtimeUnit: "unix_seconds"}}
			result, err := (FolderSyncRemote{d}).Apply(context.Background(), c, step, run, log)
			if lost {
				var notStarted *foldersync.NotStartedError
				if err == nil || errors.As(err, &notStarted) {
					t.Fatalf("lost move must remain unknown: %v", err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(result)
				if !strings.Contains(string(raw), path.Join(parent, "folder")) {
					t.Fatalf("missing archive receipt %s", raw)
				}
			}
			if calls.Load() != 1 {
				t.Fatalf("move calls %d", calls.Load())
			}
			_, _ = (FolderSyncRemote{d}).Apply(context.Background(), c, step, run, log)
			if calls.Load() != 1 {
				t.Fatal("replayed move")
			}
			if len(events) < 2 {
				t.Fatal("missing event evidence", events)
			}
			operations, err := d.queue.store.LoadRemoteOperations()
			if err != nil || len(operations) != 1 {
				t.Fatal("journal", operations, err)
			}
			want := "confirmed"
			if lost {
				want = "unknown"
			}
			if operations[0].Status != want {
				t.Fatalf("status %s", operations[0].Status)
			}
		})
	}
}

func TestSyncRemoteArchivePathRejectsInvalidScope(t *testing.T) {
	c := foldersync.Config{ID: strings.Repeat("a", 32), Mode: "both", RemoteRoot: "/sync"}
	run := strings.Repeat("b", 32)
	expected := "/sync/.nas-sync-history/" + c.ID + "/" + run + "/folder/child"
	if got, err := syncRemoteArchivePath(c, run, "folder/child"); err != nil || got != expected {
		t.Fatal(got, err)
	}
	for _, p := range []string{"", "../other", "folder/../other", "/folder", "folder//child", ".nas-sync-history/old", "a\\b", "a:b"} {
		if got, err := syncRemoteArchivePath(c, run, p); err == nil {
			t.Fatalf("accepted %q as %s", p, got)
		}
	}
	for _, invalid := range []string{"", "invalid", strings.Repeat("z", 32)} {
		if _, err := syncRemoteArchivePath(c, invalid, "folder"); err == nil {
			t.Fatal("accepted run", invalid)
		}
	}
	c.Mode = "pull"
	if _, err := syncRemoteArchivePath(c, run, "folder"); err == nil {
		t.Fatal("pull remote write allowed")
	}
}
