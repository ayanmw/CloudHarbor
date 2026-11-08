package baiduapi

import (
	"bytes"
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

func scopedSessionFixture(payload []byte) UploadSessionPlan {
	c := NewClient("old-token", "app")
	sha := sha256.Sum256(payload)
	scope := UploadAccountScope{AccountFingerprint: strings.Repeat("a", 64), CredentialFingerprint: c.Fingerprint(), AppKeyFingerprint: c.ApplicationFingerprint(), CheckedAtUTC: time.Now().UTC()}
	hashes := []string{}
	needed := []int{}
	for offset := 0; offset < len(payload) || len(hashes) == 0; offset += UploadBlockSize {
		end := min(offset+UploadBlockSize, len(payload))
		md := md5.Sum(payload[offset:end])
		hashes = append(hashes, fmt.Sprintf("%x", md))
		needed = append(needed, len(needed))
		if end == len(payload) {
			break
		}
	}
	return UploadSessionPlan{PartAdmissionVersion: 1, UploadID: "private-fixture", RemotePath: "/file", Size: int64(len(payload)), BlockSize: UploadBlockSize, LocalSHA256: fmt.Sprintf("%x", sha), BlockMD5: hashes, NeededParts: needed, CredentialFingerprint: c.Fingerprint(), AppKeyFingerprint: c.ApplicationFingerprint(), ObservedAtUTC: time.Now().UTC(), AccountScope: &scope}
}

func TestUploadScopeMismatchStopsBeforeRequests(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	calls := 0
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
	})
	plan := scopedSessionFixture([]byte("abc"))
	local := t.TempDir() + "/payload"
	if err := os.WriteFile(local, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"credential", "app", "unbound", "empty-app"} {
		client := NewClient("old-token", "app")
		scope := *plan.AccountScope
		switch kind {
		case "credential":
			scope.CredentialFingerprint = NewClient("other", "app").Fingerprint()
		case "app":
			scope.AppKeyFingerprint = NewClient("old-token", "other").ApplicationFingerprint()
		case "unbound":
			scope.AccountFingerprint = ""
		case "empty-app":
			client = NewClient("old-token", "")
			scope.AppKeyFingerprint = client.ApplicationFingerprint()
		}
		ctx := WithUploadAccountScope(context.Background(), scope)
		_, err := client.UploadLocalFile(ctx, local, "/file", func(string, int64) error { return nil })
		if err == nil || calls != 0 {
			t.Fatalf("kind=%s calls=%d err=%v", kind, calls, err)
		}
	}
}
func TestUploadSessionPayloadVerificationChecksAllBlocksAndEmptyPayload(t *testing.T) {
	for _, payload := range [][]byte{{}, append(bytes.Repeat([]byte("a"), UploadBlockSize), byte('b'))} {
		plan := scopedSessionFixture(payload)
		name := t.TempDir() + "/payload"
		if err := os.WriteFile(name, payload, 0600); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		if err = verifyUploadSessionPayload(context.Background(), f, plan, nil); err != nil {
			f.Close()
			t.Fatal(err)
		}
		f.Close()
		if len(payload) > 0 {
			payload[len(payload)-1] = 'c'
			if err = os.WriteFile(name, payload, 0600); err != nil {
				t.Fatal(err)
			}
			f, err = os.Open(name)
			if err != nil {
				t.Fatal(err)
			}
			err = verifyUploadSessionPayload(context.Background(), f, plan, nil)
			f.Close()
			if err == nil {
				t.Fatal("changed last block accepted")
			}
		}
	}
}

func TestUploadSessionPayloadCheckpointStopsBetweenBlocks(t *testing.T) {
	payload := append(bytes.Repeat([]byte("a"), UploadBlockSize), byte('b'))
	plan := scopedSessionFixture(payload)
	name := t.TempDir() + "/payload"
	if err := os.WriteFile(name, payload, 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	calls := 0
	stop := errors.New("fixture pause")
	err = verifyUploadSessionPayload(context.Background(), f, plan, func() error {
		calls++
		if calls == 2 {
			return stop
		}
		return nil
	})
	offset, _ := f.Seek(0, io.SeekCurrent)
	if !errors.Is(err, stop) || calls != 2 || offset != UploadBlockSize {
		t.Fatalf("calls=%d offset=%d err=%v", calls, offset, err)
	}
}
func TestUploadSessionStableScopeAllowsTokenRotationAndRejectsAccountOrAppChanges(t *testing.T) {
	p := scopedSessionFixture([]byte("abc"))
	current := *p.AccountScope
	current.CredentialFingerprint = NewClient("new-token", "app").Fingerprint()
	if err := ValidateUploadSessionScope(p, current); err != nil {
		t.Fatal(err)
	}
	changed := current
	changed.AccountFingerprint = strings.Repeat("b", 64)
	if ValidateUploadSessionScope(p, changed) == nil {
		t.Fatal("different account accepted")
	}
	changed = current
	changed.AppKeyFingerprint = NewClient("new-token", "other-app").ApplicationFingerprint()
	if ValidateUploadSessionScope(p, changed) == nil {
		t.Fatal("different app accepted")
	}
	p.AccountScope = nil
	if ValidateUploadSessionScope(p, current) == nil {
		t.Fatal("historical unscoped session accepted")
	}
}
func TestUploadSessionPayloadVerificationReadsContentAndRejectsChangedData(t *testing.T) {
	for _, tc := range []struct {
		name, payload          string
		badSHA, badMD5, cancel bool
		wantError              bool
	}{{"same", "abc", false, false, false, false}, {"same-size-change", "abd", false, false, false, true}, {"size-change", "abcd", false, false, false, true}, {"sha", "abc", true, false, false, true}, {"md5", "abc", false, true, false, true}, {"cancel", "abc", false, false, true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			p := scopedSessionFixture([]byte("abc"))
			if tc.badSHA {
				p.LocalSHA256 = strings.Repeat("0", 64)
			}
			if tc.badMD5 {
				p.BlockMD5[0] = strings.Repeat("0", 32)
			}
			name := t.TempDir() + "/payload"
			if err := os.WriteFile(name, []byte(tc.payload), 0600); err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(name)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			ctx := context.Background()
			if tc.cancel {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			err = verifyUploadSessionPayload(ctx, f, p, nil)
			if (err != nil) != tc.wantError {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
