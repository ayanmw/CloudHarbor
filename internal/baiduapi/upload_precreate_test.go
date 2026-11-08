package baiduapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestRapidUploadEvidenceBeforeRequestAndAfterReply(t *testing.T) {
	for _, mode := range []string{"success", "intent-failure", "receipt-failure", "lost-reply", "wrong-parent"} {
		t.Run(mode, func(t *testing.T) {
			original := http.DefaultTransport
			defer func() { http.DefaultTransport = original }()
			local := t.TempDir() + "/payload"
			if err := os.WriteFile(local, []byte("hello"), 0600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			intent := ""
			received := false
			http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if intent == "" {
					t.Fatal("precreate before durable intent")
				}
				if mode == "lost-reply" {
					return nil, io.ErrUnexpectedEOF
				}
				body := `{"errno":0,"return_type":2,"fs_id":42,"path":"/hello(1)","size":5}`
				if mode == "wrong-parent" {
					body = `{"errno":0,"return_type":2,"fs_id":42,"path":"/other/hello","size":5}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			ctx := WithUploadPrecreateObserver(context.Background(), func(c UploadPrecreateCheckpoint) error {
				if c.Intent != nil {
					if !c.Intent.Valid() {
						t.Fatal("invalid intent")
					}
					if mode == "intent-failure" {
						return errors.New("fixture")
					}
					intent = c.Intent.Digest()
				}
				if c.RapidReceipt != nil {
					received = true
					if c.RapidReceipt.PlanDigest != intent || c.RapidReceipt.Receipt.FsID != "42" {
						t.Fatal("wrong receipt binding")
					}
					if mode == "receipt-failure" {
						return errors.New("fixture")
					}
				}
				return nil
			})
			_, err := NewClient("token", "app").UploadLocalFile(ctx, local, "/hello", func(string, int64) error { return nil })
			if mode == "success" {
				if err != nil || !received {
					t.Fatalf("receipt=%v err=%v", received, err)
				}
			} else if err == nil {
				t.Fatal("failure lost")
			}
			if mode == "intent-failure" {
				if calls != 0 {
					t.Fatal("sent after failed intent")
				}
			} else if calls != 1 {
				t.Fatal("precreate repeated")
			}
			if mode == "receipt-failure" || mode == "lost-reply" || mode == "wrong-parent" {
				if !errors.Is(err, ErrUploadUnconfirmed) {
					t.Fatalf("not isolated: %v", err)
				}
			}
		})
	}
}
