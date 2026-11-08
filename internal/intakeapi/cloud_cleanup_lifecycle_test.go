package intakeapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/downloader"
	"web_browser_baidu_onekey_upload/internal/management"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type cleanupFixtureTransport func(*http.Request) (*http.Response, error)

func (f cleanupFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Real loopback management HTTP + real downloader/SQLite, with a fail-closed
// provider transport. No token, account or path is loaded from the user's state.
func TestCleanupHTTPLifecycleSurvivesPageDisconnectAndRestart(t *testing.T) {
	for _, scenario := range []string{"confirmed", "unknown", "stopped"} {
		unknown := scenario == "unknown"
		t.Run(scenario, func(t *testing.T) {
			original := http.DefaultTransport
			started, release := make(chan struct{}), make(chan struct{})
			var first, unblock sync.Once
			var writes atomic.Int32
			allow := func() { unblock.Do(func() { close(release) }) }
			entries := []baiduapi.FileInfo{{FsID: 9007199254740993, Path: "/source/one", ServerFilename: "one", IsDir: 1, Mtime: 123}, {FsID: 9007199254740995, Path: "/source/two", ServerFilename: "two", IsDir: 1, Mtime: 124}}
			http.DefaultTransport = cleanupFixtureTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Scheme != "https" || r.URL.Host != "pan.baidu.com" {
					return nil, fmt.Errorf("unexpected fixture network target")
				}
				var result any
				switch r.URL.Query().Get("method") {
				case "filemetas":
					var ids []int64
					if err := json.Unmarshal([]byte(r.URL.Query().Get("fsids")), &ids); err != nil || len(ids) != 1 {
						return nil, fmt.Errorf("invalid fixture metadata request")
					}
					found := []baiduapi.FileInfo{}
					for _, entry := range entries {
						if entry.FsID == ids[0] {
							found = append(found, entry)
						}
					}
					result = map[string]any{"errno": 0, "list": found}
				case "list":
					result = map[string]any{"errno": 0, "list": []baiduapi.FileInfo{}, "has_more": 0}
				case "filemanager":
					if r.Method != "POST" || r.URL.Query().Get("opera") != "delete" {
						return nil, fmt.Errorf("unexpected fixture mutation")
					}
					if err := r.ParseForm(); err != nil {
						return nil, err
					}
					var paths []map[string]string
					if err := json.Unmarshal([]byte(r.Form.Get("filelist")), &paths); err != nil || len(paths) != 1 || (paths[0]["path"] != entries[0].Path && paths[0]["path"] != entries[1].Path) {
						return nil, fmt.Errorf("mutation escaped fixed fixture paths")
					}
					writes.Add(1)
					first.Do(func() { close(started) })
					select {
					case <-release:
					case <-r.Context().Done():
						return nil, r.Context().Err()
					}
					if unknown {
						result = map[string]any{}
					} else {
						result = map[string]any{"errno": 0, "info": []map[string]int{{"errno": 0}}}
					}
				default:
					return nil, fmt.Errorf("unexpected fixture provider method")
				}
				body, _ := json.Marshal(result)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
			})
			t.Cleanup(func() { allow(); http.DefaultTransport = original })
			cfg := &config.Config{LocalDownDir: t.TempDir(), QueueDir: "/queue"}
			open := func(token string) *downloader.Downloader {
				d, err := downloader.OpenDownloader(cfg, baiduapi.NewClient(token, "fixture-app"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					allow()
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if err := d.Shutdown(ctx); err != nil {
						t.Error(err)
					}
				})
				if err = d.BindAccount("123"); err != nil {
					t.Fatal(err)
				}
				d.SetAuthentication(baiduapi.NewClient(token, "fixture-app"), true, time.Now().Add(time.Hour))
				return d
			}
			client := &http.Client{Transport: original, Timeout: 5 * time.Second}
			serve := func(d *downloader.Downloader) *httptest.Server {
				api := New(d, nil)
				mux := http.NewServeMux()
				mux.HandleFunc("/api/cloud/cleanup-batches", api.HandleCloudCleanupBatches)
				mux.HandleFunc("/api/cloud/cleanup-batches/{id}", api.HandleCloudCleanupBatch)
				mux.HandleFunc("/api/cloud/cleanup-batches/{id}/items", api.HandleCloudCleanupItems)
				mux.HandleFunc("/api/cloud/cleanup-batches/{id}/events", api.HandleCloudCleanupEvents)
				mux.HandleFunc("/api/cloud/cleanup-batches/{id}/stop", api.HandleCloudCleanupStop)
				gate, err := management.New("fixture-cleanup-http-key")
				if err != nil {
					t.Fatal(err)
				}
				s := httptest.NewServer(gate.Wrap(mux))
				t.Cleanup(s.Close)
				return s
			}
			request := func(s *httptest.Server, method, path string, body []byte, bearer, origin string) (int, []byte) {
				req, err := http.NewRequest(method, s.URL+path, bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/json")
				if bearer != "" {
					req.Header.Set("Authorization", "Bearer "+bearer)
				}
				if origin != "" {
					req.Header.Set("Origin", origin)
				}
				response, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				data, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				return response.StatusCode, data
			}
			id := strings.Repeat("a", 32)
			items := []taskstore.CleanupBatchItem{}
			for _, entry := range entries {
				items = append(items, taskstore.CleanupBatchItem{Kind: "empty", Expected: taskstore.CleanupIdentity{FsID: entry.FsID, Path: entry.Path, Name: entry.ServerFilename, Directory: true, Mtime: entry.Mtime}, PreviewObservedAtUTC: time.Now().UTC()})
			}
			wire, _ := json.Marshal(map[string]any{"batch_id": id, "items": items})
			d := open("fixture-old-token")
			server := serve(d)
			if code, _ := request(server, "POST", "/api/cloud/cleanup-batches", wire, "", server.URL); code != 401 {
				t.Fatal(code)
			}
			if code, _ := request(server, "POST", "/api/cloud/cleanup-batches", wire, "fixture-cleanup-http-key", "https://evil.invalid"); code != 403 {
				t.Fatal(code)
			}
			if writes.Load() != 0 {
				t.Fatal("unapproved request reached provider")
			}
			if _, lookupErr := d.CloudCleanupBatch(context.Background(), id); !errors.Is(lookupErr, sql.ErrNoRows) {
				t.Fatalf("rejected requests created a batch: %v", lookupErr)
			}
			code, body := request(server, "POST", "/api/cloud/cleanup-batches", wire, "fixture-cleanup-http-key", server.URL)
			var accepted taskstore.CleanupBatchRecord
			if code != 202 || json.Unmarshal(body, &accepted) != nil || accepted.ID != id || accepted.ItemCount != 2 || accepted.CreatedAtUTC.IsZero() {
				t.Fatalf("create status=%d body=%s", code, body)
			}
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("worker never reached fixture provider")
			}
			if scenario == "stopped" {
				if code, _ = request(server, "POST", "/api/cloud/cleanup-batches/"+id+"/stop", []byte(`{}`), "fixture-cleanup-http-key", server.URL); code != 200 {
					t.Fatalf("stop status %d", code)
				}
			}
			server.Close() // No browser/management HTTP connection remains to own the job.
			allow()
			want, count := "completed", int32(2)
			if unknown {
				want, count = "unknown", 1
			}
			if scenario == "stopped" {
				want, count = "stopped", 1
			}
			deadline := time.Now().Add(8 * time.Second)
			for {
				record, err := d.CloudCleanupBatch(context.Background(), id)
				if err != nil {
					t.Fatal(err)
				}
				if record.Status == want {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("disconnected batch did not settle: %+v", record)
				}
				time.Sleep(20 * time.Millisecond)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := d.Shutdown(ctx)
			cancel()
			if err != nil {
				t.Fatal(err)
			}
			restored := open("fixture-renewed-token")
			if err = restored.ResumeCloudCleanup(); err != nil {
				t.Fatal(err)
			}
			again := serve(restored)
			code, body = request(again, "POST", "/api/cloud/cleanup-batches", wire, "fixture-cleanup-http-key", again.URL)
			var repeated taskstore.CleanupBatchRecord
			if code != 202 || json.Unmarshal(body, &repeated) != nil || repeated.Status != want || !repeated.CreatedAtUTC.Equal(accepted.CreatedAtUTC) {
				t.Fatalf("idempotent restart status=%d body=%s", code, body)
			}
			code, body = request(again, "GET", "/api/cloud/cleanup-batches/"+id+"/items?offset=0&limit=25", nil, "fixture-cleanup-http-key", "")
			var detail struct {
				Items []taskstore.CleanupItemRecord `json:"items"`
				Total int                           `json:"total"`
			}
			if code != 200 || json.Unmarshal(body, &detail) != nil || detail.Total != 2 || len(detail.Items) != 2 || detail.Items[0].Expected.FsID != entries[0].FsID {
				t.Fatalf("exact item readback status=%d body=%s", code, body)
			}
			if scenario == "stopped" && (detail.Items[0].Status != "confirmed" || detail.Items[1].Status != "pending" || detail.Items[1].OperationID != "" || !repeated.StopRequested) {
				t.Fatalf("stop advanced future item: %+v", detail.Items)
			}
			if unknown && (detail.Items[0].Status != "unknown" || detail.Items[1].Status != "pending" || detail.Items[1].OperationID != "") {
				t.Fatalf("unknown advanced subsequent item: %+v", detail.Items)
			}
			code, body = request(again, "GET", "/api/cloud/cleanup-batches/"+id+"/events?after_sequence=0&limit=100", nil, "fixture-cleanup-http-key", "")
			var events struct {
				Events []taskstore.CleanupBatchEvent `json:"events"`
			}
			if code != 200 || json.Unmarshal(body, &events) != nil || len(events.Events) < 3 || events.Events[0].Kind != "created" || events.Events[0].ResponseObservedAtUTC != nil {
				t.Fatalf("event readback status=%d body=%s", code, body)
			}
			results := 0
			for _, event := range events.Events {
				if event.RecordedAtUTC.IsZero() {
					t.Fatal("event has no server record time")
				}
				if event.Kind == "item_result" {
					results++
					if event.Ordinal == nil || *event.Ordinal < 0 || *event.Ordinal >= len(detail.Items) || event.OperationID != detail.Items[*event.Ordinal].OperationID {
						t.Fatalf("event/item evidence mismatch: %+v", event)
					}
					if !unknown && event.ResponseObservedAtUTC == nil {
						t.Fatal("confirmed result lacks provider response observation")
					}
				}
			}
			if int32(results) != count {
				t.Fatalf("result evidence count %d expected %d", results, count)
			}
			items[0].Expected.Mtime++
			changed, _ := json.Marshal(map[string]any{"batch_id": id, "items": items})
			if code, _ = request(again, "POST", "/api/cloud/cleanup-batches", changed, "fixture-cleanup-http-key", again.URL); code != 409 {
				t.Fatalf("changed manifest accepted: %d", code)
			}
			// Observe beyond the worker's two-second periodic check as well as its
			// immediate wake; terminal records must never become new sends.
			time.Sleep(2200 * time.Millisecond)
			if writes.Load() != count {
				t.Fatalf("replayed provider writes %d expected %d", writes.Load(), count)
			}
		})
	}
}
