package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
)

// Route every provider request to the local fixture, including in the child.
// A process kill must exercise the durable sending record, not graceful Shutdown.
func routeCrashProvider(t *testing.T, address string) {
	t.Helper()
	endpoint, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		copy.URL.Scheme, copy.URL.Host = endpoint.Scheme, endpoint.Host
		copy.Host = endpoint.Host
		return original.RoundTrip(copy)
	})
	t.Cleanup(func() { http.DefaultTransport = original })
}

func TestSourceOperationProcessCrashRemainsIsolated(t *testing.T) {
	for _, mode := range []DownloadMode{DownloadModeKeep, DownloadModeDelete} {
		t.Run(string(mode), func(t *testing.T) {
			started := make(chan struct{}, 1)
			var sourceRequests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Query().Get("method") {
				case "list":
					json.NewEncoder(w).Encode(map[string]any{"errno": 0, "list": []baiduapi.FileInfo{file(1, "a.txt", 3)}})
				case "filemetas":
					if r.URL.Query().Get("dlink") == "1" {
						json.NewEncoder(w).Encode(map[string]any{"errno": 0, "list": []map[string]string{{"dlink": "http://" + r.Host + "/content"}}})
					} else {
						json.NewEncoder(w).Encode(map[string]any{"errno": 0, "list": []baiduapi.FileInfo{file(1, "a.txt", 3)}})
					}
				case "filemanager":
					wantOperation := "rename"
					if mode == DownloadModeDelete {
						wantOperation = "delete"
					}
					if r.URL.Query().Get("opera") != wantOperation {
						t.Errorf("source operation = %q, want %s", r.URL.Query().Get("opera"), wantOperation)
					}
					sourceRequests.Add(1)
					select {
					case started <- struct{}{}:
					default:
					}
					// Read the posted request before waiting for disconnect: HTTP/1
					// otherwise may not notice the killed client's closed connection.
					io.Copy(io.Discard, r.Body)
					<-r.Context().Done()
				default:
					io.WriteString(w, "abc")
				}
			}))
			defer srv.Close()
			address := srv.URL
			root := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSourceCrashProcessHelper$")
			cmd.Env = append(os.Environ(), "NAS_SOURCE_CRASH_ROOT="+root, "NAS_SOURCE_CRASH_URL="+address, "NAS_SOURCE_CRASH_MODE="+string(mode))
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			defer func() {
				if !waited {
					cmd.Process.Kill()
					cmd.Wait()
				}
			}()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("child did not reach the source request")
			}
			body, err := os.ReadFile(filepath.Join(root, stateDirectory, "queue", "jobs", "1.json"))
			var saved queueJob
			if err != nil || json.Unmarshal(body, &saved) != nil || saved.SourceState != "sending" || saved.Status != "source_unknown" {
				t.Fatalf("request preceded durable sending record: %s, %v", body, err)
			}
			if body, err = os.ReadFile(filepath.Join(root, "a.txt")); err != nil || string(body) != "abc" {
				t.Fatalf("source request preceded complete publication: %q, %v", body, err)
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err == nil {
				t.Fatal("helper exited successfully instead of being killed")
			}
			waited = true
			routeCrashProvider(t, address)
			d, err := OpenDownloader(&config.Config{LocalDownDir: root, QueueDir: "/queue", MaxParallel: 1}, baiduapi.NewClient("fixture-token", "fixture-app"))
			if err != nil {
				t.Fatalf("crash did not release writer lock: %v", err)
			}
			defer shutdown(t, d)
			if err = d.ScanAndDownload(); err != nil {
				t.Fatal(err)
			}
			// A fresh scan and ready credential must not release this quarantine.
			time.Sleep(200 * time.Millisecond)
			tasks := d.GetTasks()
			if len(tasks) != 1 || tasks[0].Status != "source_unknown" || tasks[0].CanCancel || tasks[0].CanRetry || tasks[0].DownloadMode != mode {
				t.Fatalf("crash lost isolation or captured mode: %+v", tasks)
			}
			for _, action := range []func(int64) error{d.Cancel, d.Retry} {
				if !errors.Is(action(1), ErrTaskConflict) {
					t.Fatal("manual action released an uncertain source operation")
				}
			}
			shutdown(t, d)
			if sourceRequests.Load() != 1 {
				t.Fatalf("replayed uncertain source request: %d", sourceRequests.Load())
			}
		})
	}
}

func TestSourceCrashProcessHelper(t *testing.T) {
	root := os.Getenv("NAS_SOURCE_CRASH_ROOT")
	if root == "" {
		return
	}
	routeCrashProvider(t, os.Getenv("NAS_SOURCE_CRASH_URL"))
	d, err := OpenDownloader(&config.Config{LocalDownDir: root, QueueDir: "/queue", MaxParallel: 1}, baiduapi.NewClient("fixture-token", "fixture-app"))
	if err != nil {
		t.Fatal(err)
	}
	d.SetDownloadMode(DownloadMode(os.Getenv("NAS_SOURCE_CRASH_MODE")))
	if err = d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	<-d.ctx.Done()
}
