package downloader

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestUploadStagedFilePreservesAttemptSessionObserver(t *testing.T) {
	for _, mismatch := range []string{"none", "path", "identity"} {
		t.Run(fmt.Sprint("metadata_mismatch_", mismatch), func(t *testing.T) {
			d, p, _, _, _ := intakeFixture(t)
			if err := d.BindAccount("123"); err != nil {
				t.Fatal(err)
			}
			d.SetAuthentication(baiduapi.NewClient("verified-token", "app"), true, time.Now().Add(time.Hour))
			p.metadataOverride = func(id int64, _ int) *baiduapi.FileInfo {
				if id == 999 {
					if mismatch != "none" {
						if mismatch == "identity" {
							return &baiduapi.FileInfo{FsID: 1000, Path: "/uploaded", Size: 3}
						}
						return &baiduapi.FileInfo{FsID: 999, Path: "/changed", Size: 3}
					}
					return &baiduapi.FileInfo{FsID: 999, Path: "/uploaded", ServerFilename: "uploaded", Size: 3, Mtime: 42}
				}
				return nil
			}
			creates := 0
			base := http.DefaultTransport
			http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
				body := ""
				switch r.URL.Query().Get("method") {
				case "precreate":
					body = `{"errno":0,"return_type":1,"uploadid":"fixture-private-session","block_list":[0]}`
				case "upload":
					body = `{"errno":0,"md5":"900150983cd24fb0d6963f7d28e17f72"}`
				case "create":
					creates++
					body = `{"errno":0,"fs_id":999,"path":"/uploaded","size":3}`
				default:
					return base.RoundTrip(r)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			local := t.TempDir() + "/payload"
			if err := os.WriteFile(local, []byte("abc"), 0600); err != nil {
				t.Fatal(err)
			}
			checkpoints := []baiduapi.UploadSessionCheckpoint{}
			ctx := baiduapi.WithUploadSessionObserver(context.Background(), func(checkpoint baiduapi.UploadSessionCheckpoint) error {
				checkpoints = append(checkpoints, checkpoint)
				return nil
			})
			receipt, err := d.UploadStagedFile(ctx, local, "/uploaded", func(string, int64) error { return nil }, nil)
			if mismatch != "none" {
				if !errors.Is(err, baiduapi.ErrUploadUnconfirmed) {
					t.Fatalf("metadata mismatch: %v", err)
				}
			} else if err != nil || receipt.FsID != "999" {
				t.Fatalf("receipt=%+v err=%v", receipt, err)
			}
			if creates != 1 || len(checkpoints) != 5 || checkpoints[4].CreateReceipt == nil || checkpoints[4].CreateReceipt.Receipt.FsID != "999" {
				t.Fatal("lost receipt identity or repeated create")
			}
			if len(checkpoints) != 5 || checkpoints[0].Plan == nil || !checkpoints[0].Plan.Valid() || checkpoints[2].Part == nil || checkpoints[2].Part.PlanDigest != checkpoints[0].Plan.Digest() {
				t.Fatalf("receipt=%+v count=%d err=%v", receipt, len(checkpoints), err)
			}
			account := sha256.Sum256([]byte("123"))
			scope := checkpoints[0].Plan.AccountScope
			if scope == nil || scope.AccountFingerprint != fmt.Sprintf("%x", account) || scope.CheckedAtUTC.IsZero() {
				t.Fatalf("missing stable account scope: %+v", scope)
			}
		})
	}
}

func TestUploadApplicationChangeInvalidatesOldAuthenticationEpoch(t *testing.T) {
	d, _, _, _, _ := intakeFixture(t)
	expiry := time.Now().Add(time.Hour)
	d.SetAuthentication(baiduapi.NewClient("token", "first-app"), true, expiry)
	_, epoch, err := d.authorizedClient(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(baiduapi.NewClient("token", "second-app"), true, expiry)
	if _, err = d.intakeClient(context.Background(), epoch); !errors.Is(err, ErrIntakeStale) {
		t.Fatal("application change retained previous admission")
	}
}

func TestUploadScopeRequiresBindingAndRejectsCredentialChangeDuringHashing(t *testing.T) {
	d, _, _, _, _ := intakeFixture(t)
	expiry := time.Now().Add(time.Hour)
	d.SetAuthentication(baiduapi.NewClient("token", "app"), true, expiry)
	base := http.DefaultTransport
	calls := 0
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) { calls++; return base.RoundTrip(r) })
	local := t.TempDir() + "/payload"
	if err := os.WriteFile(local, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := d.UploadStagedFile(context.Background(), local, "/file", func(string, int64) error { return nil }, nil)
	if !errors.Is(err, taskstore.ErrAccountNotBound) || calls != 0 {
		t.Fatalf("unbound calls=%d err=%v", calls, err)
	}
	if err = d.BindAccount("123"); err != nil {
		t.Fatal(err)
	}
	rotated := false
	_, err = d.UploadStagedFile(context.Background(), local, "/file", func(phase string, _ int64) error {
		if phase == "hashing" && !rotated {
			rotated = true
			d.SetAuthentication(baiduapi.NewClient("rotated-token", "app"), true, expiry)
		}
		return nil
	}, nil)
	if !errors.Is(err, ErrIntakeStale) || calls != 0 || !rotated {
		t.Fatalf("stale scope calls=%d rotated=%v err=%v", calls, rotated, err)
	}
}

func TestDownloaderUsesResumeContextAndCurrentAuthorization(t *testing.T) {
	d, p, _, _, _ := intakeFixture(t)
	if err := d.BindAccount("123"); err != nil {
		t.Fatal(err)
	}
	client := baiduapi.NewClient("current-token", "app")
	d.SetAuthentication(client, true, time.Now().Add(time.Hour))
	p.metadataOverride = func(id int64, _ int) *baiduapi.FileInfo {
		if id == 999 {
			return &baiduapi.FileInfo{FsID: 999, Path: "/uploaded", Size: 3}
		}
		return nil
	}
	account := sha256.Sum256([]byte("123"))
	sha := sha256.Sum256([]byte("abc"))
	oldToken := sha256.Sum256([]byte("old-token"))
	scope := baiduapi.UploadAccountScope{AccountFingerprint: fmt.Sprintf("%x", account), CredentialFingerprint: fmt.Sprintf("%x", oldToken), AppKeyFingerprint: client.ApplicationFingerprint(), CheckedAtUTC: time.Now().UTC()}
	plan := baiduapi.UploadSessionPlan{PartAdmissionVersion: 1, CreateAdmissionVersion: 1, UploadID: "original", RemotePath: "/uploaded", Size: 3, BlockSize: baiduapi.UploadBlockSize, LocalSHA256: fmt.Sprintf("%x", sha), BlockMD5: []string{"900150983cd24fb0d6963f7d28e17f72"}, NeededParts: []int{0}, CredentialFingerprint: scope.CredentialFingerprint, AppKeyFingerprint: scope.AppKeyFingerprint, AccountScope: &scope, ObservedAtUTC: time.Now().UTC()}
	base := http.DefaultTransport
	calls := []string{}
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		method := r.URL.Query().Get("method")
		if method != "upload" && method != "create" {
			if method == "precreate" {
				t.Fatal("resume sent precreate")
			}
			return base.RoundTrip(r)
		}
		calls = append(calls, method)
		if r.URL.Query().Get("access_token") != "current-token" {
			t.Fatal("used historical credential")
		}
		body := `{"errno":0,"md5":"900150983cd24fb0d6963f7d28e17f72"}`
		if method == "create" {
			body = `{"errno":0,"fs_id":999,"path":"/uploaded","size":3}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	local := t.TempDir() + "/payload"
	if err := os.WriteFile(local, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := baiduapi.WithUploadSessionResume(context.Background(), baiduapi.UploadSessionResume{SourceAttemptID: strings.Repeat("a", 32), SourceSequence: 1, Plan: plan})
	admitted := false
	ctx = baiduapi.WithUploadResumeAdmission(ctx, func(a baiduapi.UploadResumeAdmission) error {
		if a.SourcePlanDigest != plan.Digest() || a.CurrentScope.CredentialFingerprint != client.Fingerprint() {
			t.Fatal("lost scope bridge")
		}
		admitted = true
		return nil
	})
	observations := 0
	ctx = baiduapi.WithUploadSessionObserver(ctx, func(c baiduapi.UploadSessionCheckpoint) error {
		if !admitted || c.Plan != nil {
			t.Fatal("unexpected resume checkpoint")
		}
		observations++
		return nil
	})
	receipt, err := d.UploadStagedFile(ctx, local, "/uploaded", func(string, int64) error { return nil }, nil)
	if err != nil || receipt.FsID != "999" || len(calls) != 2 || observations != 4 {
		t.Fatalf("receipt=%+v calls=%v observations=%d err=%v", receipt, calls, observations, err)
	}
}

func TestResumeDoesNotRecreateMissingParent(t *testing.T) {
	d, p, _, _, _ := intakeFixture(t)
	if err := d.BindAccount("123"); err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(baiduapi.NewClient("token", "app"), true, time.Now().Add(time.Hour))
	p.lists["/"] = []baiduapi.FileInfo{}
	base := http.DefaultTransport
	writes := 0
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			writes++
			return nil, io.ErrUnexpectedEOF
		}
		return base.RoundTrip(r)
	})
	ctx := baiduapi.WithUploadSessionResume(context.Background(), baiduapi.UploadSessionResume{})
	_, err := d.UploadStagedFile(ctx, t.TempDir()+"/missing-local", "/missing/file", func(string, int64) error { return nil }, nil)
	if writes != 0 || !errors.Is(err, baiduapi.ErrUploadUnconfirmed) {
		t.Fatalf("resume recreated namespace: writes=%d err=%v", writes, err)
	}
}

func TestReceiptVerificationUsesAccountGateAndRejectsLateEpoch(t *testing.T) {
	for _, mode := range []string{"success", "different-account", "late-credential-change"} {
		t.Run(mode, func(t *testing.T) {
			d, p, _, _, _ := intakeFixture(t)
			if err := d.BindAccount("123"); err != nil {
				t.Fatal(err)
			}
			client := baiduapi.NewClient("current-token", "app")
			expiry := time.Now().Add(time.Hour)
			d.SetAuthentication(client, true, expiry)
			account := sha256.Sum256([]byte("123"))
			old := sha256.Sum256([]byte("old-token"))
			scope := baiduapi.UploadAccountScope{AccountFingerprint: fmt.Sprintf("%x", account), CredentialFingerprint: fmt.Sprintf("%x", old), AppKeyFingerprint: client.ApplicationFingerprint(), CheckedAtUTC: time.Now().UTC()}
			if mode == "different-account" {
				scope.AccountFingerprint = strings.Repeat("e", 64)
			}
			calls := 0
			p.metadataOverride = func(id int64, _ int) *baiduapi.FileInfo {
				calls++
				if mode == "late-credential-change" {
					d.SetAuthentication(baiduapi.NewClient("rotated-token", "app"), true, expiry)
				}
				return &baiduapi.FileInfo{FsID: id, Path: "/uploaded", Size: 3}
			}
			result, err := d.VerifyUploadReceipt(context.Background(), scope, baiduapi.UploadReceipt{Path: "/uploaded", FsID: "999", Size: "3"})
			switch mode {
			case "success":
				if err != nil || result.CurrentScope.CredentialFingerprint != client.Fingerprint() || calls != 1 {
					t.Fatalf("verification=%+v calls=%d err=%v", result, calls, err)
				}
			case "different-account":
				if err == nil || calls != 0 {
					t.Fatalf("read different account: %d %v", calls, err)
				}
			case "late-credential-change":
				if !errors.Is(err, ErrIntakeStale) {
					t.Fatalf("accepted stale epoch: %v", err)
				}
			}
		})
	}
}
