package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"path"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/foldersync"
)

func TestRemoteArchiveVerificationReadsEveryDescendant(t *testing.T) {
	for _, scenario := range []string{"complete", "retimestamped", "missing", "extra", "replaced", "changed", "empty_directory_missing", "root_changed_during_scan", "duplicate_id", "escaped_child"} {
		t.Run(scenario, func(t *testing.T) {
			d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("old")) })
			d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
			c := foldersync.Config{ID: strings.Repeat("a", 32), Mode: "both", RemoteRoot: "/sync", RemoteID: "70", Account: syncAccountScope(d.getClient())}
			run := strings.Repeat("b", 32)
			archive := path.Join(c.RemoteRoot, ".nas-sync-history", c.ID, run, "folder")
			p.lists["/"] = []baiduapi.FileInfo{{FsID: 70, Path: "/sync", ServerFilename: "sync", IsDir: 1}}
			p.lists[path.Dir(archive)] = []baiduapi.FileInfo{{FsID: 71, Path: archive, ServerFilename: "folder", IsDir: 1, Mtime: 123}}
			child := baiduapi.FileInfo{FsID: 72, Path: archive + "/child", ServerFilename: "child", Size: 3, Mtime: 124}
			empty := baiduapi.FileInfo{FsID: 73, Path: archive + "/empty", ServerFilename: "empty", IsDir: 1, Mtime: 125}
			p.lists[archive] = []baiduapi.FileInfo{child, empty}
			p.lists[empty.Path] = []baiduapi.FileInfo{}
			hash := sha256.Sum256([]byte("old"))
			digest := hex.EncodeToString(hash[:])
			expected := []foldersync.Entry{{ID: "71", Path: "folder", Directory: true, Mtime: 123, MtimeUnit: "unix_seconds"}, {ID: "72", Path: "folder/child", Size: 3, Mtime: 124, MtimeUnit: "unix_seconds", SHA256: digest}, {ID: "73", Path: "folder/empty", Directory: true, Mtime: 125, MtimeUnit: "unix_seconds"}}
			if scenario == "retimestamped" {
				p.lists[path.Dir(archive)][0].Mtime = 999
				for i := range p.lists[archive] {
					p.lists[archive][i].Mtime = 999
				}
			}
			switch scenario {
			case "missing":
				p.lists[archive] = []baiduapi.FileInfo{empty}
			case "extra":
				p.lists[archive] = append(p.lists[archive], baiduapi.FileInfo{FsID: 74, Path: archive + "/extra", ServerFilename: "extra", Size: 3, Mtime: 124})
			case "replaced":
				p.lists[archive][0].FsID = 99
			case "changed":
				expected[1].SHA256 = strings.Repeat("c", 64)
			case "empty_directory_missing":
				p.lists[archive] = []baiduapi.FileInfo{child}
			case "root_changed_during_scan":
				p.metadataOverride = func(id int64, call int) *baiduapi.FileInfo {
					if id == 71 && call >= 2 {
						v := p.lists[path.Dir(archive)][0]
						v.Mtime++
						return &v
					}
					return nil
				}
			case "duplicate_id":
				p.lists[archive][1].FsID = child.FsID
			case "escaped_child":
				p.lists[archive][1].Path = "/outside/empty"
			}
			step := foldersync.Step{Action: "remote_archive", Path: "folder", Remote: &expected[0]}
			proof, err := (FolderSyncRemote{d}).VerifyArchive(foldersync.WithRunID(context.Background(), run), c, step, run, expected)
			if scenario == "complete" || scenario == "retimestamped" {
				if err != nil {
					t.Fatal(err)
				}
				if proof.ExpectedContentIdentitySHA256 != proof.ObservedContentIdentitySHA256 {
					t.Fatal("content identity mismatch")
				}
				if scenario == "retimestamped" && (proof.ExpectedSHA256 == proof.ObservedSHA256 || len(proof.MtimeChanges) != 3) {
					t.Fatal("time differences not preserved", proof.MtimeChanges)
				}
				if proof.ExpectedItems != "3" || proof.ObservedItems != "3" || proof.ArchivePath != archive || proof.VerifiedAt.IsZero() {
					t.Fatalf("proof %+v", proof)
				}
			} else if err == nil {
				t.Fatal("changed archive accepted", scenario)
			}
			if len(p.actions) != 0 {
				t.Fatal("verification wrote to cloud", p.actions)
			}
		})
	}
}
