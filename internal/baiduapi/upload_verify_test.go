package baiduapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestVerifyUploadReceiptOnlyReadsBoundIdentity(t *testing.T) {
	for _, mode := range []string{"success", "path", "size", "directory", "identity", "account", "application", "cancelled", "missing-scope", "bad-receipt"} {
		t.Run(mode, func(t *testing.T) {
			c := NewClient("new-token", "app")
			old := UploadAccountScope{AccountFingerprint: strings.Repeat("a", 64), CredentialFingerprint: strings.Repeat("b", 64), AppKeyFingerprint: c.ApplicationFingerprint(), CheckedAtUTC: time.Now().UTC()}
			current := old
			current.CredentialFingerprint = c.Fingerprint()
			receipt := UploadReceipt{Path: "/actual(1)", FsID: "42", Size: "5"}
			if mode == "bad-receipt" {
				receipt.FsID = "042"
			}
			original := http.DefaultTransport
			defer func() { http.DefaultTransport = original }()
			calls := 0
			http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || r.URL.Query().Get("method") != "filemetas" || r.URL.Query().Get("fsids") != "[42]" {
					t.Fatal("verification sent a mutation or read another object")
				}
				id, size, dir := 42, 5, 0
				path := "/actual(1)"
				switch mode {
				case "path":
					path = "/changed"
				case "size":
					size = 6
				case "directory":
					dir = 1
				case "identity":
					id = 43
				}
				body := fmt.Sprintf(`{"errno":0,"list":[{"fs_id":%d,"path":%q,"size":%d,"isdir":%d,"server_mtime":123}]}`, id, path, size, dir)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			ctx := context.Background()
			if mode == "account" {
				current.AccountFingerprint = strings.Repeat("c", 64)
			}
			if mode == "application" {
				old.AppKeyFingerprint = strings.Repeat("d", 64)
			}
			if mode != "missing-scope" {
				ctx = WithUploadAccountScope(ctx, current)
			}
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			result, err := c.VerifyUploadReceipt(ctx, old, receipt)
			if mode == "success" {
				if err != nil || result.Receipt != receipt || result.CurrentScope.CredentialFingerprint != c.Fingerprint() || result.ObservedAtUTC.IsZero() || result.RemoteMtime != 123 {
					t.Fatalf("result=%+v err=%v", result, err)
				}
			} else if err == nil {
				t.Fatalf("invalid receipt accepted: %s", mode)
			}
			switch mode {
			case "account", "application", "cancelled", "missing-scope", "bad-receipt":
				if calls != 0 {
					t.Fatal("read before authorization validation")
				}
			default:
				if calls != 1 {
					t.Fatal("verification repeated request")
				}
			}
		})
	}
}
