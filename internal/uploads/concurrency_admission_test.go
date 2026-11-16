package uploads

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func TestParallelBatchIsolationStopsActualSDKMutationAfterPhaseCallback(t *testing.T) {
	for _, phase := range []string{"submitting", "uploading", "finalizing"} {
		t.Run(phase, func(t *testing.T) {
			original := http.DefaultTransport
			defer func() { http.DefaultTransport = original }()
			var precreates, uploads, creates atomic.Int32
			http.DefaultTransport = sessionTestTransport(func(r *http.Request) (*http.Response, error) {
				body := ""
				switch r.URL.Query().Get("method") {
				case "precreate":
					precreates.Add(1)
					body = `{"errno":0,"return_type":1,"uploadid":"fixture-session","block_list":[0]}`
				case "upload":
					uploads.Add(1)
					body = `{"errno":0,"md5":"900150983cd24fb0d6963f7d28e17f72"}`
				case "create":
					creates.Add(1)
					body = `{"errno":0,"fs_id":1,"path":"/target/a","size":3}`
				default:
					t.Error("unexpected request")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			var m *Manager
			var peer string
			var err error
			m, err = Open(t.TempDir(), func(ctx context.Context, local, remote string, progress func(string, int64) error, _ func(baiduapi.UploadTransferSample) error) (baiduapi.UploadReceipt, error) {
				return baiduapi.NewClient("token", "app").UploadLocalFile(ctx, local, remote, func(state string, sent int64) error {
					if e := progress(state, sent); e != nil {
						return e
					}
					if state == phase {
						return m.update(peer, "unknown", 0, "injected peer unknown after phase callback")
					}
					return nil
				})
			})
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}, {Path: "peer", Size: "3"}})
			if err != nil {
				t.Fatal(err)
			}
			peer = b.Items[1].ID
			if err = m.Receive(context.Background(), b.ID, b.Items[0].ID, strings.NewReader("abc")); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			var state string
			for time.Now().Before(deadline) {
				got, e := m.Get(b.ID)
				if e != nil {
					t.Fatal(e)
				}
				for _, item := range got.Items {
					if item.ID == b.Items[0].ID {
						state = item.State
					}
				}
				if state == "paused" || state == "confirmed" || state == "failed" || state == "unknown" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if phase == "submitting" && precreates.Load() != 0 {
				t.Fatalf("precreate sent after unknown: %d", precreates.Load())
			}
			if phase != "submitting" && precreates.Load() != 1 {
				t.Fatalf("expected admitted precreate, got %d", precreates.Load())
			}
			if phase == "finalizing" && uploads.Load() != 1 {
				t.Fatalf("expected confirmed part before finalizing, got %d", uploads.Load())
			}
			if phase == "uploading" && uploads.Load() != 0 {
				t.Fatalf("part sent after unknown: %d", uploads.Load())
			}
			if creates.Load() != 0 || state != "paused" {
				t.Fatalf("create=%d state=%s", creates.Load(), state)
			}
		})
	}
}

func TestAdmittedUploadReceiptsSurviveIsolationAndShutdownButNotReturnedOwner(t *testing.T) {
	for _, kind := range []string{"rapid", "create"} {
		for _, mode := range []string{"unknown", "shutdown", "returned"} {
			t.Run(kind+"_"+mode, func(t *testing.T) {
				m, err := Open(t.TempDir(), nil)
				if err != nil {
					t.Fatal(err)
				}
				defer m.Close()
				b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}, {Path: "peer", Size: "3"}})
				if err != nil {
					t.Fatal(err)
				}
				item, attempt := b.Items[0].ID, strings.Repeat("a", 32)
				m.beginUploadAttempt(item, attempt)
				plan := fixtureSessionPlan("/target/a")
				plan.CreateAdmissionVersion = 1
				var observe func() error
				if kind == "rapid" {
					intent := baiduapi.UploadPrecreateIntent{RemotePath: plan.RemotePath, Size: plan.Size, LocalSHA256: plan.LocalSHA256, BlockMD5: plan.BlockMD5, CredentialFingerprint: plan.CredentialFingerprint, AppKeyFingerprint: plan.AppKeyFingerprint, ObservedAtUTC: time.Now().UTC()}
					record := m.precreateEvidenceRecorder(item, attempt)
					if err = record(baiduapi.UploadPrecreateCheckpoint{Intent: &intent}); err != nil {
						t.Fatal(err)
					}
					receipt := baiduapi.UploadCreateReceipt{PlanDigest: intent.Digest(), Receipt: baiduapi.UploadReceipt{FsID: "42", Path: "/target/a", Size: "3"}, ObservedAtUTC: time.Now().UTC()}
					observe = func() error { return record(baiduapi.UploadPrecreateCheckpoint{RapidReceipt: &receipt}) }
				} else {
					record := m.sessionEvidenceRecorder(item, attempt)
					part := baiduapi.UploadPartReceipt{PlanDigest: plan.Digest(), Index: 0, MD5: plan.BlockMD5[0], ObservedAtUTC: time.Now().UTC()}
					admit := baiduapi.UploadPartAdmission(part)
					create := baiduapi.UploadCreateAdmission{PlanDigest: plan.Digest(), ObservedAtUTC: time.Now().UTC()}
					for _, checkpoint := range []baiduapi.UploadSessionCheckpoint{{Plan: &plan}, {Admission: &admit}, {Part: &part}, {CreateAdmission: &create}} {
						if err = record(checkpoint); err != nil {
							t.Fatal(err)
						}
					}
					receipt := baiduapi.UploadCreateReceipt{PlanDigest: plan.Digest(), Receipt: baiduapi.UploadReceipt{FsID: "42", Path: "/target/a", Size: "3"}, ObservedAtUTC: time.Now().UTC()}
					observe = func() error { return record(baiduapi.UploadSessionCheckpoint{CreateReceipt: &receipt}) }
				}
				switch mode {
				case "unknown":
					if err = m.update(b.Items[1].ID, "unknown", 0, "peer unknown"); err != nil {
						t.Fatal(err)
					}
				case "shutdown":
					m.cancel()
				case "returned":
					if err = m.recordUploadAttemptReturned(item, attempt); err != nil {
						t.Fatal(err)
					}
				}
				err = observe()
				if mode == "returned" {
					if err == nil {
						t.Fatal("late receipt accepted after runner ownership ended")
					}
				} else if err != nil {
					t.Fatalf("admitted receipt lost: %v", err)
				}
			})
		}
	}
}
