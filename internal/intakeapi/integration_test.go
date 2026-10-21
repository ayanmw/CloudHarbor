package intakeapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"strings"
	"sync"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/downloader"
	"web_browser_baidu_onekey_upload/internal/management"
)

type localTransport func(*http.Request) (*http.Response, error)

func (f localTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Exercise the management API, real downloader journal and Baidu HTTP encoding
// together. Every provider request is confined to this local test server.
func TestHTTPIntakePersistsOnceAndSurvivesRestart(t *testing.T) {
	var mu sync.Mutex
	moves := 0
	file := baiduapi.FileInfo{FsID: 3400000000000000123, Path: "/source/a.txt", ServerFilename: "a.txt", Size: 3, Mtime: 42}
	lists := map[string][]baiduapi.FileInfo{"/source": {file}, "/queue": {}}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Query().Get("method") == "list":
			json.NewEncoder(w).Encode(map[string]any{"errno": 0, "list": lists[r.URL.Query().Get("dir")]})
		case r.URL.Query().Get("opera") == "move":
			r.ParseForm()
			var items []struct{ Path, Dest, Newname string }
			if json.Unmarshal([]byte(r.Form.Get("filelist")), &items) != nil || len(items) != 1 || items[0].Path != file.Path {
				t.Error("unexpected move body")
				http.Error(w, "bad request", 400)
				return
			}
			moves++
			file.Path = path.Join(items[0].Dest, items[0].Newname)
			lists["/source"] = []baiduapi.FileInfo{}
			lists["/queue"] = []baiduapi.FileInfo{file}
			io.WriteString(w, `{"errno":0,"info":[{"errno":0}]}`)
		default:
			t.Errorf("unexpected provider operation: %s", r.URL.Path)
			http.Error(w, "unexpected operation", 400)
		}
	}))
	defer provider.Close()
	address, _ := url.Parse(provider.URL)
	base := http.DefaultTransport
	http.DefaultTransport = localTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() == "127.0.0.1" {
			return base.RoundTrip(r)
		}
		if r.URL.Host != "pan.baidu.com" {
			return nil, io.ErrUnexpectedEOF
		}
		copy := r.Clone(r.Context())
		u := *r.URL
		u.Scheme, u.Host = address.Scheme, address.Host
		copy.URL = &u
		return base.RoundTrip(copy)
	})
	defer func() { http.DefaultTransport = base }()
	cfg := &config.Config{LocalDownDir: t.TempDir(), QueueDir: "/queue", MaxParallel: 1, ScanInterval: 30}
	client := baiduapi.NewClient("local-fixture", "")
	d, err := downloader.OpenDownloader(cfg, client)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := d.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	}()
	d.SetAuthentication(client, true, time.Now().Add(time.Hour))
	s := New(d, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/cloud", s.HandleCloud)
	mux.HandleFunc("/api/intakes/preview", s.HandlePreview)
	mux.HandleFunc("/api/intakes/", s.HandleAction)
	gate, _ := management.New("local-test-administrator")
	server := httptest.NewServer(gate.Wrap(mux))
	defer server.Close()
	request := func(method, endpoint, body string, authorized bool) (int, []byte) {
		t.Helper()
		r, _ := http.NewRequest(method, server.URL+endpoint, strings.NewReader(body))
		if authorized {
			r.SetBasicAuth("admin", "local-test-administrator")
		}
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		return response.StatusCode, data
	}
	if status, _ := request("POST", "/api/intakes/preview", `{"paths":["/source/a.txt"]}`, false); status != 401 {
		t.Fatal("anonymous preview accepted")
	}
	status, body := request("GET", "/api/cloud?dir=/source", "", true)
	if status != 200 || !strings.Contains(string(body), `"id":"3400000000000000123"`) {
		t.Fatalf("listing failed: %d %s", status, body)
	}
	status, body = request("POST", "/api/intakes/preview", `{"paths":["/source/a.txt"]}`, true)
	var preview downloader.Intake
	if status != 200 || json.Unmarshal(body, &preview) != nil || preview.ID == "" {
		t.Fatalf("preview failed: %d %s", status, body)
	}
	for i := 0; i < 2; i++ {
		status, body = request("POST", "/api/intakes/"+preview.ID+"/commit", `{}`, true)
		var result downloader.Intake
		if status != 200 || json.Unmarshal(body, &result) != nil || len(result.Items) != 1 || result.Items[0].State != "moved" {
			t.Fatalf("commit failed: %d %s", status, body)
		}
	}
	mu.Lock()
	count := moves
	mu.Unlock()
	if count != 1 {
		t.Fatalf("move replayed %d times", count)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := d.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	d, err = downloader.OpenDownloader(cfg, client)
	if err != nil {
		t.Fatal(err)
	}
	records, tasks := d.GetIntakes(), d.GetTasks()
	if len(records) != 1 || records[0].Items[0].State != "moved" || len(tasks) != 1 || tasks[0].TaskID != "3400000000000000123" || tasks[0].Status != "pending" {
		t.Fatalf("restart lost admission: records=%+v tasks=%+v", records, tasks)
	}
}
