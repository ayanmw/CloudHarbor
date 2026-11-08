package baiduapi

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestResumeUsesOriginalSessionWithoutPrecreate(t *testing.T) {
	for _, mode := range []string{"success", "confirmed", "admission-failure", "changed-payload", "changed-account", "no-admission", "no-session-observer", "rejected-session"} {
		t.Run(mode, func(t *testing.T) {
			local := t.TempDir() + "/payload"
			if err := os.WriteFile(local, []byte("hello"), 0600); err != nil {
				t.Fatal(err)
			}
			client := NewClient("new-token", "app")
			md := md5.Sum([]byte("hello"))
			sha := sha256.Sum256([]byte("hello"))
			old := UploadAccountScope{AccountFingerprint: strings.Repeat("a", 64), CredentialFingerprint: strings.Repeat("b", 64), AppKeyFingerprint: client.ApplicationFingerprint(), CheckedAtUTC: time.Now().UTC()}
			plan := UploadSessionPlan{CreateAdmissionVersion: 1, PartAdmissionVersion: 1, UploadID: "original-session", RemotePath: "/hello", Size: 5, BlockSize: UploadBlockSize, LocalSHA256: fmt.Sprintf("%x", sha), BlockMD5: []string{fmt.Sprintf("%x", md)}, NeededParts: []int{0}, CredentialFingerprint: old.CredentialFingerprint, AppKeyFingerprint: old.AppKeyFingerprint, AccountScope: &old, ObservedAtUTC: time.Now().UTC()}
			scope := old
			scope.CredentialFingerprint = client.Fingerprint()
			if mode == "changed-account" {
				scope.AccountFingerprint = strings.Repeat("d", 64)
			}
			if mode == "changed-payload" {
				if err := os.WriteFile(local, []byte("other"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			original := http.DefaultTransport
			defer func() { http.DefaultTransport = original }()
			calls := []string{}
			admitted := false
			http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
				method := r.URL.Query().Get("method")
				calls = append(calls, method)
				if !admitted || method == "precreate" {
					t.Fatal("resume bypassed admission or repeated precreate")
				}
				body := `{"errno":0,"md5":"5d41402abc4b2a76b9719d911017c592"}`
				if method == "create" {
					body = `{"errno":0,"fs_id":42,"path":"/hello","size":5}`
				}
				if mode == "rejected-session" {
					body = `{"errno":-8}`
				}
				if method == "upload" && r.URL.Query().Get("uploadid") != "original-session" {
					t.Fatal("changed session")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			ctx := WithUploadAccountScope(context.Background(), scope)
			if mode != "no-admission" {
				ctx = WithUploadResumeAdmission(ctx, func(a UploadResumeAdmission) error {
					if a.SourcePlanDigest != plan.Digest() || a.CurrentScope.CredentialFingerprint != client.Fingerprint() {
						t.Fatal("scope history overwritten")
					}
					if mode == "admission-failure" {
						return errors.New("fixture persistence failure")
					}
					admitted = true
					return nil
				})
			}
			checkpoints := 0
			if mode != "no-session-observer" {
				ctx = WithUploadSessionObserver(ctx, func(c UploadSessionCheckpoint) error {
					checkpoints++
					if c.Plan != nil {
						t.Fatal("resume invented a new precreate plan")
					}
					return nil
				})
			}
			source := UploadSessionResume{SourceAttemptID: strings.Repeat("c", 32), SourceSequence: 1, Plan: plan}
			if mode == "confirmed" {
				source.ConfirmedParts = []int{0}
			}
			receipt, err := client.ResumeUploadLocalFileMeasured(ctx, local, "/hello", source, func(string, int64) error { return nil }, nil)
			switch mode {
			case "success":
				if err != nil || receipt.FsID != "42" || len(calls) != 2 || checkpoints != 4 {
					t.Fatalf("receipt=%+v calls=%v checkpoints=%d err=%v", receipt, calls, checkpoints, err)
				}
			case "confirmed":
				if err != nil || len(calls) != 1 || calls[0] != "create" || checkpoints != 2 {
					t.Fatalf("confirmed parts repeated: %v %v", calls, err)
				}
			case "rejected-session":
				if err == nil || len(calls) != 1 {
					t.Fatalf("session rejection fell back: %v %v", calls, err)
				}
			default:
				if err == nil || len(calls) != 0 {
					t.Fatalf("unsafe resume: %v %v", calls, err)
				}
			}
		})
	}
}
