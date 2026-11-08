package baiduapi

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
)

func TestUploadSessionEvidencePrecedesPartsAndStopsOnPersistenceFailure(t *testing.T) {
	for _, failAt := range []string{"", "plan", "admission", "part"} {
		t.Run(failAt, func(t *testing.T) {
			original := http.DefaultTransport
			defer func() { http.DefaultTransport = original }()
			local := t.TempDir() + "/payload"
			if err := os.WriteFile(local, []byte("hello"), 0600); err != nil {
				t.Fatal(err)
			}
			calls := []string{}
			events := []string{}
			sentinel := errors.New("evidence unavailable")
			http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
				method := r.URL.Query().Get("method")
				calls = append(calls, method)
				body := `{"errno":0,"return_type":1,"uploadid":"private-session","block_list":[0]}`
				if method == "upload" {
					if len(events) != 2 || events[0] != "plan" || events[1] != "admission" {
						t.Fatal("part admitted before durable plan")
					}
					body = `{"errno":0,"md5":"5d41402abc4b2a76b9719d911017c592"}`
				} else if method == "create" {
					if len(events) != 3 || events[2] != "part" {
						t.Fatal("create admitted before part evidence")
					}
					body = `{"errno":0,"fs_id":1,"path":"/hello","size":5}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			ctx := WithUploadSessionObserver(context.Background(), func(e UploadSessionCheckpoint) error {
				if e.Plan != nil {
					events = append(events, "plan")
					sum := sha256.Sum256([]byte("hello"))
					if e.Plan.LocalSHA256 != fmt.Sprintf("%x", sum) || e.Plan.UploadID != "private-session" || e.Plan.Size != 5 || len(e.Plan.BlockMD5) != 1 || len(e.Plan.NeededParts) != 1 || e.Plan.ObservedAtUTC.IsZero() {
						t.Fatalf("bad plan=%+v", e.Plan)
					}
					if failAt == "plan" {
						return sentinel
					}
				} else if e.Admission != nil {
					events = append(events, "admission")
					if failAt == "admission" {
						return sentinel
					}
				} else if e.Part != nil {
					events = append(events, "part")
					if e.Part == nil || e.Part.Index != 0 || e.Part.MD5 != "5d41402abc4b2a76b9719d911017c592" || e.Part.ObservedAtUTC.IsZero() {
						t.Fatalf("bad part=%+v", e.Part)
					}
					if failAt == "part" {
						return sentinel
					}
				}
				return nil
			})
			_, err := NewClient("token", "app").UploadLocalFile(ctx, local, "/hello", func(string, int64) error { return nil })
			expected := 3
			if failAt == "plan" || failAt == "admission" {
				expected = 1
			} else if failAt == "part" {
				expected = 2
			}
			if len(calls) != expected || (failAt != "" && !errors.Is(err, sentinel)) || (failAt == "" && err != nil) {
				t.Fatalf("calls=%v events=%v err=%v", calls, events, err)
			}
		})
	}
}

func TestUploadSessionRejectsMissingPlanAndNeverAcknowledgesBadParts(t *testing.T) {
	for _, tc := range []struct {
		name, pre          string
		wantError          bool
		calls, checkpoints int
	}{
		{"missing", `{"errno":0,"return_type":1,"uploadid":"fixture"}`, true, 1, 0},
		{"null", `{"errno":0,"return_type":1,"uploadid":"fixture","block_list":null}`, true, 1, 0},
		{"duplicate", `{"errno":0,"return_type":1,"uploadid":"fixture","block_list":[0,0]}`, true, 1, 0},
		{"bad-md5", `{"errno":0,"return_type":1,"uploadid":"fixture","block_list":[0]}`, true, 2, 2},
		{"existing-blocks", `{"errno":0,"return_type":1,"uploadid":"fixture","block_list":[]}`, false, 2, 3},
		{"rapid", `{"errno":0,"return_type":2,"fs_id":1,"path":"/hello","size":5}`, false, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := http.DefaultTransport
			defer func() { http.DefaultTransport = original }()
			local := t.TempDir() + "/payload"
			if err := os.WriteFile(local, []byte("hello"), 0600); err != nil {
				t.Fatal(err)
			}
			calls, checkpoints := 0, 0
			http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				body := tc.pre
				switch r.URL.Query().Get("method") {
				case "upload":
					body = `{"errno":0,"md5":"00000000000000000000000000000000"}`
				case "create":
					body = `{"errno":0,"fs_id":1,"path":"/hello","size":5}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			ctx := WithUploadSessionObserver(context.Background(), func(UploadSessionCheckpoint) error { checkpoints++; return nil })
			_, err := NewClient("token", "app").UploadLocalFile(ctx, local, "/hello", func(string, int64) error { return nil })
			if (err != nil) != tc.wantError || calls != tc.calls || checkpoints != tc.checkpoints {
				t.Fatalf("calls=%d checkpoints=%d err=%v", calls, checkpoints, err)
			}
		})
	}
}

func TestUploadPartAdmissionFailureStopsBeforePartRequest(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	local := t.TempDir() + "/payload"
	if err := os.WriteFile(local, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errno":0,"return_type":1,"uploadid":"fixture","block_list":[0]}`)), Request: r}, nil
	})
	failure := errors.New("admission unavailable")
	ctx := WithUploadSessionObserver(context.Background(), func(e UploadSessionCheckpoint) error {
		if e.Admission != nil {
			return failure
		}
		return nil
	})
	_, err := NewClient("token", "app").UploadLocalFile(ctx, local, "/hello", func(string, int64) error { return nil })
	if !errors.Is(err, failure) || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestUploadCreateEvidenceControlsAdmissionAndKeepsUnknownResultIsolated(t *testing.T) {
	for _, mode := range []string{"success", "admission-failure", "receipt-failure", "lost-reply", "invalid-receipt"} {
		t.Run(mode, func(t *testing.T) {
			original := http.DefaultTransport
			defer func() { http.DefaultTransport = original }()
			name := t.TempDir() + "/payload"
			if err := os.WriteFile(name, []byte("hello"), 0600); err != nil {
				t.Fatal(err)
			}
			calls := []string{}
			admitted, received := false, false
			failure := errors.New("fixture create evidence failure")
			http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
				method := r.URL.Query().Get("method")
				calls = append(calls, method)
				body := `{"errno":0,"return_type":1,"uploadid":"fixture","block_list":[0]}`
				if method == "upload" {
					body = `{"errno":0,"md5":"5d41402abc4b2a76b9719d911017c592"}`
				} else if method == "create" {
					if !admitted {
						t.Fatal("create before durable admission")
					}
					if mode == "lost-reply" {
						return nil, io.ErrUnexpectedEOF
					}
					body = `{"errno":0,"fs_id":1,"path":"/hello(1)","size":5}`
					if mode == "invalid-receipt" {
						body = `{"errno":0,"fs_id":1,"path":"/other/hello","size":5}`
					}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			ctx := WithUploadSessionObserver(context.Background(), func(e UploadSessionCheckpoint) error {
				if e.CreateAdmission != nil {
					if mode == "admission-failure" {
						return failure
					}
					admitted = true
				}
				if e.CreateReceipt != nil {
					received = true
					if e.CreateReceipt.Receipt.Path != "/hello(1)" || e.CreateReceipt.Receipt.FsID != "1" {
						t.Fatal("wrong create identity")
					}
					if mode == "receipt-failure" {
						return failure
					}
				}
				return nil
			})
			_, err := NewClient("token", "app").UploadLocalFile(ctx, name, "/hello", func(string, int64) error { return nil })
			if mode == "success" {
				if err != nil || !received {
					t.Fatalf("success err=%v", err)
				}
			} else if err == nil {
				t.Fatal("failure accepted")
			}
			if mode == "admission-failure" {
				if len(calls) != 2 || admitted {
					t.Fatalf("calls=%v admitted=%v", calls, admitted)
				}
			} else if len(calls) != 3 {
				t.Fatalf("calls=%v", calls)
			}
			if mode == "lost-reply" || mode == "invalid-receipt" || mode == "receipt-failure" {
				if !errors.Is(err, ErrUploadUnconfirmed) {
					t.Fatalf("result not isolated: %v", err)
				}
			}
		})
	}
}
