package downloader

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/config"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

const cleanupCrashBatchID = "cccccccccccccccccccccccccccccccc"

// The parent kills only its own helper process after verifying committed
// sending/prepared evidence. There is deliberately no graceful Shutdown.
func TestCleanupProcessCrashDoesNotReplayPreparedOrInFlightItem(t *testing.T) {
	for _, phase := range []string{"before_send", "in_flight"} {
		t.Run(phase, func(t *testing.T) {
			root := t.TempDir()
			ready := filepath.Join(root, "cleanup-crash-ready")
			if err := os.WriteFile(filepath.Join(root, "cleanup-fixture-owner"), []byte("synthetic-cleanup-crash"), 0600); err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Query().Get("method") {
				case "filemetas":
					json.NewEncoder(w).Encode(map[string]any{"errno": 0, "list": []baiduapi.FileInfo{{FsID: 91, Path: "/source/one", ServerFilename: "one", IsDir: 1, Mtime: 123}}})
				case "list":
					json.NewEncoder(w).Encode(map[string]any{"errno": 0, "list": []baiduapi.FileInfo{}, "has_more": 0})
				case "filemanager":
					if r.Method != "POST" || r.URL.Query().Get("opera") != "delete" {
						t.Error("unexpected fixture mutation")
						w.WriteHeader(400)
						return
					}
					if err := r.ParseForm(); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					var files []map[string]string
					if err := json.Unmarshal([]byte(r.Form.Get("filelist")), &files); err != nil || len(files) != 1 || files[0]["path"] != "/source/one" {
						t.Error("mutation escaped the first fixture item")
						w.WriteHeader(400)
						return
					}
					requests.Add(1)
					if err := os.WriteFile(ready, []byte("received-without-response"), 0600); err != nil {
						t.Error(err)
					}
					// ParseForm drained the body so HTTP/1 detects the killed client's close.
					<-r.Context().Done()
				default:
					w.WriteHeader(400)
				}
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCleanupCrashProcessHelper$")
			cmd.Env = append(os.Environ(), "NAS_CLEANUP_CRASH_ROOT="+root, "NAS_CLEANUP_CRASH_URL="+srv.URL, "NAS_CLEANUP_CRASH_PHASE="+phase)
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			waited := false
			defer func() {
				if !waited {
					_ = cmd.Process.Kill()
					<-done
				}
			}()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
		waitReady:
			for {
				select {
				case err := <-done:
					waited = true
					t.Fatalf("helper exited before crash checkpoint: %v %s", err, output.String())
				case <-ctx.Done():
					_ = cmd.Process.Kill()
					<-done
					waited = true
					t.Fatalf("helper checkpoint timeout: %s", output.String())
				case <-ticker.C:
					if _, err := os.Stat(ready); err == nil {
						break waitReady
					}
				}
			}
			db, err := sql.Open("sqlite", filepath.Join(root, stateDirectory, "queue", "queue.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			var itemState, operationState, operationID string
			err = db.QueryRow(`SELECT i.status,r.status,i.operation_id FROM cleanup_batch_items i JOIN remote_operations r ON r.operation_id=i.operation_id WHERE i.batch_id=? AND i.ordinal=0`, cleanupCrashBatchID).Scan(&itemState, &operationState, &operationID)
			db.Close()
			if err != nil || itemState != "sending" || operationState != "prepared" || operationID == "" {
				t.Fatalf("request preceded durable association: %s/%s id=%s err=%v", itemState, operationState, operationID, err)
			}
			want := int32(0)
			if phase == "in_flight" {
				want = 1
			}
			if requests.Load() != want {
				t.Fatalf("unexpected write before crash: %d expected %d", requests.Load(), want)
			}
			if err = cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			crashErr := <-done
			waited = true
			if crashErr == nil {
				t.Fatal("helper unexpectedly exited cleanly")
			}
			routeCrashProvider(t, srv.URL)
			d, err := OpenDownloader(&config.Config{LocalDownDir: root, QueueDir: "/queue"}, baiduapi.NewClient("synthetic-renewed-token", "synthetic-app"))
			if err != nil {
				t.Fatal(err)
			}
			defer shutdown(t, d)
			d.SetAuthentication(baiduapi.NewClient("synthetic-renewed-token", "synthetic-app"), true, time.Now().Add(time.Hour))
			record, err := d.CloudCleanupBatch(context.Background(), cleanupCrashBatchID)
			if err != nil || record.Status != "unknown" || record.Unknown != 1 || record.Pending != 1 {
				t.Fatalf("recovery fabricated outcome: %+v %v", record, err)
			}
			items, total, err := d.CloudCleanupItems(context.Background(), cleanupCrashBatchID, 0, 25)
			if err != nil || total != 2 || len(items) != 2 || items[0].Status != "unknown" || items[0].OperationID != operationID || items[1].Status != "pending" || items[1].OperationID != "" {
				t.Fatalf("recovered items: %+v total=%d err=%v", items, total, err)
			}
			events, _, err := d.CloudCleanupEvents(context.Background(), cleanupCrashBatchID, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, event := range events {
				if event.Kind == "item_result" {
					found = true
					if event.Reason != "remote_unknown" || event.OperationID != operationID || event.ResponseObservedAtUTC != nil {
						t.Fatalf("crash invented provider response: %+v", event)
					}
				}
			}
			if !found {
				t.Fatal("missing recovery event")
			}
			d.queue.mu.Lock()
			blocked := d.queue.isCloudMutationBlocked(91, "/source/one")
			d.queue.mu.Unlock()
			if !blocked {
				t.Fatal("uncertain source not quarantined after credential renewal")
			}
			if err = d.ResumeCloudCleanup(); err != nil {
				t.Fatal(err)
			}
			time.Sleep(2200 * time.Millisecond)
			if requests.Load() != want {
				t.Fatalf("crash replayed delete: %d expected %d", requests.Load(), want)
			}
			after, err := d.CloudCleanupBatch(context.Background(), cleanupCrashBatchID)
			if err != nil || after.Status != "unknown" || after.Pending != 1 {
				t.Fatalf("worker advanced after crash: %+v %v", after, err)
			}
		})
	}
}

func TestCleanupCrashProcessHelper(t *testing.T) {
	root, address, phase := os.Getenv("NAS_CLEANUP_CRASH_ROOT"), os.Getenv("NAS_CLEANUP_CRASH_URL"), os.Getenv("NAS_CLEANUP_CRASH_PHASE")
	if root == "" {
		t.Skip("subprocess fixture only")
	}
	marker, err := os.ReadFile(filepath.Join(root, "cleanup-fixture-owner"))
	if err != nil || string(marker) != "synthetic-cleanup-crash" {
		t.Fatal("not an owned synthetic fixture")
	}
	endpoint, err := url.Parse(address)
	if err != nil || endpoint.Scheme != "http" || endpoint.User != nil || !net.ParseIP(endpoint.Hostname()).IsLoopback() || (phase != "before_send" && phase != "in_flight") {
		t.Fatal("invalid loopback fixture")
	}
	original := http.DefaultTransport
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "pan.baidu.com" {
			return nil, fmt.Errorf("unexpected fixture network target")
		}
		if phase == "before_send" && r.URL.Query().Get("method") == "filemanager" {
			if err := os.WriteFile(filepath.Join(root, "cleanup-crash-ready"), []byte("prepared-before-network"), 0600); err != nil {
				return nil, err
			}
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		copy := r.Clone(r.Context())
		copy.URL.Scheme, copy.URL.Host = endpoint.Scheme, endpoint.Host
		copy.Host = endpoint.Host
		return original.RoundTrip(copy)
	})
	d, err := OpenDownloader(&config.Config{LocalDownDir: root, QueueDir: "/queue"}, baiduapi.NewClient("synthetic-old-token", "synthetic-app"))
	if err != nil {
		t.Fatal(err)
	}
	if err = d.BindAccount("123"); err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(baiduapi.NewClient("synthetic-old-token", "synthetic-app"), true, time.Now().Add(time.Hour))
	var items []taskstore.CleanupBatchItem
	for i, name := range []string{"one", "two"} {
		items = append(items, taskstore.CleanupBatchItem{Kind: "empty", Expected: taskstore.CleanupIdentity{FsID: int64(91 + i), Path: "/source/" + name, Name: name, Directory: true, Mtime: 123}, PreviewObservedAtUTC: time.Now().UTC()})
	}
	if _, err = d.CreateCloudCleanupBatch(context.Background(), cleanupCrashBatchID, items); err != nil {
		t.Fatal(err)
	}
	// Stay alive until the parent performs Process.Kill. Never close the store.
	select {}
}
