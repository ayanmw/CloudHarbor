package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestCleanupDeleteRequiresRetainedCopyContentAndIdentity(t *testing.T) {
	for _, scenario := range []string{"matching", "changed_content", "missing", "same_identity", "changed_identity_after_hash"} {
		t.Run(scenario, func(t *testing.T) {
			var reads atomic.Int32
			d, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
				n := reads.Add(1)
				data := "payload"
				if scenario == "changed_content" && n == 2 {
					data = "changed"
				}
				_, _ = w.Write([]byte(data))
			})
			target := baiduapi.FileInfo{FsID: 901, Path: "/source/copy", ServerFilename: "copy", Size: 7, Mtime: 123}
			keep := baiduapi.FileInfo{FsID: 902, Path: "/source/keep", ServerFilename: "keep", Size: 7, Mtime: 124}
			p.lists["/source"] = []baiduapi.FileInfo{target, keep}
			if scenario == "changed_identity_after_hash" {
				p.metadataOverride = func(id int64, call int) *baiduapi.FileInfo {
					if id == keep.FsID && call >= 2 {
						changed := keep
						changed.Mtime++
						return &changed
					}
					return nil
				}
			}
			if scenario == "missing" {
				p.lists["/source"] = []baiduapi.FileInfo{target}
			}
			store, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
			if err != nil {
				t.Fatal(err)
			}
			d.queue.store = store
			defer shutdown(t, d)
			d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
			digest := sha256.Sum256([]byte("payload"))
			if scenario == "same_identity" {
				keep = target
			}
			result, err := d.MutateCloud(context.Background(), CloudMutationRequest{Operation: "delete", Expected: target, ExpectedContentSHA256: hex.EncodeToString(digest[:]), ExpectedRetained: &keep})
			if scenario == "matching" {
				if err != nil || result.Status != "confirmed" || len(p.actions) != 1 || reads.Load() != 2 {
					t.Fatalf("matching result=%+v err=%v reads=%d actions=%v", result, err, reads.Load(), p.actions)
				}
				events, _, _, e := store.RemoteOperationEventPage(result.OperationID, taskstore.RemoteOperationEventFilter{}, 0, 20)
				if e != nil {
					t.Fatal(e)
				}
				found := false
				for _, event := range events {
					if event.Status != "prepared" {
						continue
					}
					var evidence struct {
						Verification struct {
							Completed time.Time          `json:"checks_completed_at_utc"`
							Retained  *baiduapi.FileInfo `json:"expected_retained"`
							SHA       string             `json:"expected_content_sha256"`
						} `json:"cleanup_verification"`
					}
					if e = json.Unmarshal(event.Evidence, &evidence); e != nil {
						t.Fatal(e)
					}
					if evidence.Verification.Completed.IsZero() || evidence.Verification.Retained == nil || evidence.Verification.Retained.FsID != keep.FsID || evidence.Verification.SHA != hex.EncodeToString(digest[:]) {
						t.Fatalf("missing retained verification evidence: %s", event.Evidence)
					}
					found = true
				}
				if !found {
					t.Fatal("prepared verification event missing")
				}
			} else {
				if err == nil || len(p.actions) != 0 {
					t.Fatalf("unsafe cleanup accepted: %v %v", err, p.actions)
				}
				if scenario == "changed_content" && !errors.Is(err, ErrIntakeStale) {
					t.Fatalf("content mismatch: %v", err)
				}
			}
		})
	}
}
