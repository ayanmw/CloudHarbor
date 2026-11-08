package uploads

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestWorkerResumesConfirmedPartsAcrossRestart(t *testing.T) {
	for _, mode := range []string{"direct", "retry-before-merge", "hash-interruption", "pending-isolated"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			m, err := Open(dir, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if m != nil {
					m.Close()
				}
			}()
			payload := strings.Repeat("a", baiduapi.UploadBlockSize) + "abc"
			b, err := m.Create("/target", []Input{{Path: "a", Size: fmt.Sprint(len(payload))}})
			if err != nil {
				t.Fatal(err)
			}
			item := b.Items[0].ID
			if err = m.Receive(context.Background(), b.ID, item, strings.NewReader(payload)); err != nil {
				t.Fatal(err)
			}
			oldID := strings.Repeat("a", 32)
			if claimed, e := m.claimUploadAttempt(item, oldID); e != nil || !claimed {
				t.Fatalf("claim=%v %v", claimed, e)
			}
			oldClient := baiduapi.NewClient("old-token", "app")
			scope := baiduapi.UploadAccountScope{AccountFingerprint: strings.Repeat("d", 64), CredentialFingerprint: oldClient.Fingerprint(), AppKeyFingerprint: oldClient.ApplicationFingerprint(), CheckedAtUTC: time.Now().UTC()}
			first, last := md5.Sum([]byte(payload[:baiduapi.UploadBlockSize])), md5.Sum([]byte("abc"))
			sha := sha256.Sum256([]byte(payload))
			plan := baiduapi.UploadSessionPlan{PartAdmissionVersion: 1, CreateAdmissionVersion: 1, UploadID: "old-session", RemotePath: "/target/a", Size: int64(len(payload)), BlockSize: baiduapi.UploadBlockSize, LocalSHA256: fmt.Sprintf("%x", sha), BlockMD5: []string{fmt.Sprintf("%x", first), fmt.Sprintf("%x", last)}, NeededParts: []int{0, 1}, CredentialFingerprint: scope.CredentialFingerprint, AppKeyFingerprint: scope.AppKeyFingerprint, AccountScope: &scope, ObservedAtUTC: time.Now().UTC()}
			record := m.sessionEvidenceRecorder(item, oldID)
			part := baiduapi.UploadPartReceipt{PlanDigest: plan.Digest(), Index: 0, MD5: plan.BlockMD5[0], ObservedAtUTC: time.Now().UTC()}
			admit := baiduapi.UploadPartAdmission(part)
			checkpoints := []baiduapi.UploadSessionCheckpoint{{Plan: &plan}, {Admission: &admit}}
			if mode != "pending-isolated" {
				checkpoints = append(checkpoints, baiduapi.UploadSessionCheckpoint{Part: &part})
			}
			for _, c := range checkpoints {
				if err = record(c); err != nil {
					t.Fatal(err)
				}
			}
			if err = m.update(item, "uploading", baiduapi.UploadBlockSize, ""); err != nil {
				t.Fatal(err)
			}
			if err = m.Close(); err != nil {
				t.Fatal(err)
			}
			m = nil
			original := http.DefaultTransport
			defer func() { http.DefaultTransport = original }()
			calls := []string{}
			http.DefaultTransport = sessionTestTransport(func(r *http.Request) (*http.Response, error) {
				method := r.URL.Query().Get("method")
				calls = append(calls, method)
				body := `{"errno":0,"md5":"900150983cd24fb0d6963f7d28e17f72"}`
				if method == "upload" {
					if r.URL.Query().Get("partseq") != "1" || r.URL.Query().Get("uploadid") != "old-session" {
						t.Error("confirmed part repeated or session changed")
					}
				} else if method == "create" {
					body = fmt.Sprintf(`{"errno":0,"fs_id":42,"path":"/target/a","size":%d}`, len(payload))
				} else {
					t.Error("unexpected precreate")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			client := baiduapi.NewClient("new-token", "app")
			current := scope
			current.CredentialFingerprint = client.Fingerprint()
			interrupted, retried := false, false
			m, err = Open(dir, func(ctx context.Context, local, remote string, progress func(string, int64) error, observe func(baiduapi.UploadTransferSample) error) (baiduapi.UploadReceipt, error) {
				source, ok := baiduapi.UploadSessionResumeFromContext(ctx)
				if !ok {
					t.Error("worker lost resume context")
					return baiduapi.UploadReceipt{}, fmt.Errorf("missing source")
				}
				wrapped := func(phase string, sent int64) error {
					if !interrupted && ((mode == "retry-before-merge" && phase == "finalizing") || (mode == "hash-interruption" && phase == "hashing")) {
						interrupted = true
						return fmt.Errorf("fixture interruption")
					}
					return progress(phase, sent)
				}
				return client.ResumeUploadLocalFileMeasured(baiduapi.WithUploadAccountScope(ctx, current), local, remote, source, wrapped, observe)
			})
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(6 * time.Second)
			for {
				got, e := m.Get(b.ID)
				if e != nil {
					t.Fatal(e)
				}
				if mode == "pending-isolated" && got.Items[0].State == "unknown" {
					if len(calls) != 0 {
						t.Fatal("unknown part repeated")
					}
					return
				}
				if got.Items[0].State == "failed" && mode != "direct" && !retried {
					if err = m.Retry(b.ID); err != nil {
						t.Fatal(err)
					}
					retried = true
				}
				if got.Items[0].State == "confirmed" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("not resumed: %+v", got)
				}
				time.Sleep(20 * time.Millisecond)
			}
			latest, err := m.latestUploadAttempt(item)
			if err != nil {
				t.Fatal(err)
			}
			j, err := m.readSessionJournal(item, latest.AttemptID)
			if err != nil || len(j.ConfirmedParts) != 2 || j.CreateReceipt == nil || j.PlanDigest != plan.Digest() {
				t.Fatalf("broken chain=%+v %v", j, err)
			}
			if len(calls) != 2 || calls[0] != "upload" || calls[1] != "create" {
				t.Fatalf("unexpected calls %v", calls)
			}
		})
	}
}
