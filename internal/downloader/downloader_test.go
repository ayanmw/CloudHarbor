package downloader

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Fake only the remote provider; exercise the real API client, downloader and disk.
type provider struct {
	lists      map[string][]baiduapi.FileInfo
	actions    []string
	failAction bool
	mu         sync.Mutex
}

func setup(t *testing.T, content http.HandlerFunc) (*Downloader, *provider) {
	t.Helper()
	srv := httptest.NewServer(content)
	t.Cleanup(srv.Close)
	p := &provider{lists: map[string][]baiduapi.FileInfo{}}
	original := http.DefaultTransport
	http.DefaultTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "pan.baidu.com" {
			return original.RoundTrip(r)
		}
		var result any
		switch r.URL.Query().Get("method") {
		case "filemetas":
			if r.URL.Query().Get("dlink") == "1" {
				result = map[string]any{"errno": 0, "list": []map[string]string{{"dlink": srv.URL}}}
			} else {
				var id int64
				fmt.Sscanf(r.URL.Query().Get("fsids"), "[%d]", &id)
				var found []baiduapi.FileInfo
				p.mu.Lock()
				for _, items := range p.lists {
					for _, item := range items {
						if item.FsID == id {
							found = append(found, item)
						}
					}
				}
				p.mu.Unlock()
				result = map[string]any{"errno": 0, "list": found}
			}
		case "list":
			items := p.lists[r.URL.Query().Get("dir")]
			if items == nil {
				items = []baiduapi.FileInfo{}
			}
			result = map[string]any{"errno": 0, "list": items}
		case "filemanager":
			r.ParseForm()
			p.mu.Lock()
			p.actions = append(p.actions, r.URL.Query().Get("opera")+":"+r.Form.Get("filelist"))
			fail := p.failAction
			p.mu.Unlock()
			errno := 0
			if fail {
				errno = 1
			}
			result = map[string]any{"errno": errno, "info": []map[string]int{{"errno": 0}}}
		default:
			return nil, fmt.Errorf("unexpected API request: %s", r.URL.Path)
		}
		b, _ := json.Marshal(result)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(b))), Request: r}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	d := NewDownloader(&config.Config{LocalDownDir: t.TempDir(), QueueDir: "/queue"}, baiduapi.NewClient("test", "test"))
	t.Cleanup(func() { shutdown(t, d) })
	return d, p
}
func file(id int64, name string, size int64) baiduapi.FileInfo {
	return baiduapi.FileInfo{FsID: id, Path: "/queue/" + name, ServerFilename: name, Size: size, Mtime: 123}
}
func assertStatus(t *testing.T, d *Downloader, id int64, want string) {
	t.Helper()
	v := d.GetTaskByFsID(id)
	if v == nil || v.Status != want {
		t.Fatalf("task=%+v, want status %s", v, want)
	}
}

func TestShortFileIsFailureAndDoesNotTouchSource(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	d.downloadFile(file(1, "a.txt", 6))
	assertStatus(t, d, 1, "failed")
	if len(p.actions) != 0 {
		t.Fatalf("modified source: %v", p.actions)
	}
	if _, err := os.Stat(filepath.Join(d.cfg.LocalDownDir, "a.txt")); !os.IsNotExist(err) {
		t.Fatalf("published incomplete file: %v", err)
	}
}

func TestExistingUnverifiedFileIsPreserved(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abcdef") })
	target := filepath.Join(d.cfg.LocalDownDir, "a.txt")
	os.WriteFile(target, []byte("OTHER!"), 0644)
	d.downloadFile(file(1, "a.txt", 6))
	assertStatus(t, d, 1, "failed")
	b, _ := os.ReadFile(target)
	if string(b) != "OTHER!" || len(p.actions) != 0 {
		t.Fatalf("existing file/source changed: %q %v", b, p.actions)
	}
}

func TestFolderFailureDoesNotMarkParent(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	folder := file(10, "folder", 0)
	folder.IsDir = 1
	child := file(11, "bad.txt", 6)
	child.Path = "/queue/folder/bad.txt"
	p.lists[folder.Path] = []baiduapi.FileInfo{child}
	d.downloadFolder(folder)
	assertStatus(t, d, 10, "failed")
	if len(p.actions) != 0 {
		t.Fatalf("marked incomplete directory: %v", p.actions)
	}
}

func TestInterruptedDownloadResumesAndHandlesIgnoredRange(t *testing.T) {
	for _, ignore := range []bool{false, true} {
		t.Run(fmt.Sprint(ignore), func(t *testing.T) {
			requests := 0
			seenRange := false
			d, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests == 1 {
					w.Header().Set("Content-Length", "6")
					io.WriteString(w, "abc")
					return
				}
				if r.Header.Get("Range") == "bytes=3-" {
					seenRange = true
					if !ignore {
						w.Header().Set("Content-Range", "bytes 3-5/6")
						w.WriteHeader(206)
						io.WriteString(w, "def")
						return
					}
				}
				io.WriteString(w, "abcdef")
			})
			f := file(1, "a.txt", 6)
			d.downloadFile(f)
			assertStatus(t, d, 1, "failed")
			d.downloadFile(f)
			assertStatus(t, d, 1, "done")
			b, err := os.ReadFile(filepath.Join(d.cfg.LocalDownDir, "a.txt"))
			if err != nil || string(b) != "abcdef" {
				t.Fatalf("content=%q, err=%v", b, err)
			}
			if !seenRange || len(p.actions) != 1 {
				t.Fatalf("resume=%v, source actions=%v", seenRange, p.actions)
			}
		})
	}
}

func TestPostprocessingFailureCanRetryWithoutDownloading(t *testing.T) {
	requests := 0
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { requests++; io.WriteString(w, "abcdef") })
	p.failAction = true
	f := file(1, "a.txt", 6)
	d.downloadFile(f)
	assertStatus(t, d, 1, "failed")
	p.failAction = false
	d.downloadFile(f)
	assertStatus(t, d, 1, "done")
	if requests != 1 || len(p.actions) != 2 {
		t.Fatalf("download requests=%d, actions=%v", requests, p.actions)
	}
}

func TestNestedFolderOnlyMarksRootAfterAllChildren(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	root := file(1, "root", 0)
	root.IsDir = 1
	sub := file(2, "sub", 0)
	sub.IsDir = 1
	sub.Path = root.Path + "/sub"
	child := file(3, "a.txt", 3)
	child.Path = sub.Path + "/a.txt"
	p.lists[root.Path] = []baiduapi.FileInfo{sub}
	p.lists[sub.Path] = []baiduapi.FileInfo{child}
	d.downloadFolder(root)
	assertStatus(t, d, 1, "done")
	b, err := os.ReadFile(filepath.Join(d.cfg.LocalDownDir, "root", "sub", "a.txt"))
	if err != nil || string(b) != "abc" {
		t.Fatalf("content=%q, err=%v", b, err)
	}
	if len(p.actions) != 1 || !strings.Contains(p.actions[0], `"newname":"root.可删除"`) {
		t.Fatalf("unexpected source changes: %v", p.actions)
	}
}

func TestTaskSnapshotsDoNotExposeMutableState(t *testing.T) {
	d, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	d.downloadFile(file(1, "a.txt", 3))
	tasks := d.GetTasks()
	tasks[0].Status = "corrupted"
	assertStatus(t, d, 1, "done")
	task := d.GetTaskByFsID(1)
	task.Status = "corrupted"
	assertStatus(t, d, 1, "done")
}

func TestLegacyMarkedChildBlocksParentCleanup(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected download") })
	root := file(1, "root", 0)
	root.IsDir = 1
	child := file(2, "old.可删除", 3)
	child.Path = root.Path + "/old.可删除"
	p.lists[root.Path] = []baiduapi.FileInfo{child}
	d.SetDownloadMode(DownloadModeDelete)
	d.downloadFolder(root)
	assertStatus(t, d, 1, "failed")
	if len(p.actions) != 0 {
		t.Fatalf("deleted unverified child: %v", p.actions)
	}
}

func TestInvalidTopLevelNameIsRejected(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	d.downloadFile(file(1, "unexpected/a.txt", 3))
	assertStatus(t, d, 1, "failed")
	if len(p.actions) != 0 {
		t.Fatalf("processed invalid name: %v", p.actions)
	}
}

func TestFullSizePartialWithoutSuccessReceiptIsRedownloaded(t *testing.T) {
	requests := 0
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Range") != "" {
			t.Error("unverified full-size partial was resumed")
		}
		io.WriteString(w, "abcdef")
	})
	f := file(1, "a.txt", 6)
	sum := sha256.Sum256([]byte("a.txt"))
	state := filepath.Join(d.cfg.LocalDownDir, stateDirectory, hex.EncodeToString(sum[:]))
	os.MkdirAll(state, 0700)
	os.WriteFile(filepath.Join(state, "content.part"), []byte("WRONG!"), 0600)
	if err := writeRecord(filepath.Join(state, "partial.json"), recordFor(f, "a.txt")); err != nil {
		t.Fatal(err)
	}
	d.downloadFile(f)
	assertStatus(t, d, 1, "done")
	b, _ := os.ReadFile(filepath.Join(d.cfg.LocalDownDir, "a.txt"))
	if requests != 1 || string(b) != "abcdef" || len(p.actions) != 1 {
		t.Fatalf("requests=%d content=%q actions=%v", requests, b, p.actions)
	}
}

func TestChangedCloudVersionDoesNotResumeOldPartial(t *testing.T) {
	requests := 0
	d, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Content-Length", "6")
			io.WriteString(w, "old")
			return
		}
		if r.Header.Get("Range") != "" {
			t.Error("old version was resumed")
		}
		io.WriteString(w, "NEWNEW")
	})
	f := file(1, "a.txt", 6)
	d.downloadFile(f)
	assertStatus(t, d, 1, "failed")
	f.Mtime++
	// New process, same directory: identity binding must survive restart.
	d = NewDownloader(d.cfg, d.getClient())
	d.downloadFile(f)
	assertStatus(t, d, 1, "done")
	b, _ := os.ReadFile(filepath.Join(d.cfg.LocalDownDir, "a.txt"))
	if string(b) != "NEWNEW" {
		t.Fatalf("mixed versions: %q", b)
	}
}

func TestChangedCompletedCloudVersionPreservesLocalFile(t *testing.T) {
	var downloads int
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) {
		downloads++
		io.WriteString(w, "NEWNEW")
	})
	old := file(81, "stable.txt", 6)
	old.MD5 = "old-cloud-version"
	local := filepath.Join(d.cfg.LocalDownDir, "stable.txt")
	oldContent := []byte("OLDOLD")
	if err := os.WriteFile(local, oldContent, 0600); err != nil {
		t.Fatal(err)
	}
	key := sha256.Sum256([]byte("stable.txt"))
	state := filepath.Join(d.cfg.LocalDownDir, stateDirectory, hex.EncodeToString(key[:]))
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	record := recordFor(old, "stable.txt")
	oldDigest := sha256.Sum256(oldContent)
	record.SHA256 = hex.EncodeToString(oldDigest[:])
	if err := writeRecord(filepath.Join(state, "complete.json"), record); err != nil {
		t.Fatal(err)
	}
	newVersion := old
	newVersion.MD5 = "new-cloud-version"

	d.downloadFile(newVersion)

	assertStatus(t, d, old.FsID, "failed")
	actual, err := os.ReadFile(local)
	if err != nil || string(actual) != string(oldContent) {
		t.Fatalf("old completed file was overwritten: content=%q err=%v", actual, err)
	}
	if downloads != 0 || len(p.actions) != 0 {
		t.Fatalf("unverified new version triggered download/source cleanup: downloads=%d actions=%v", downloads, p.actions)
	}
}

func TestModifiedCompletedFileBlocksSourceCleanup(t *testing.T) {
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abcdef") })
	f := file(1, "a.txt", 6)
	p.failAction = true
	d.downloadFile(f)
	assertStatus(t, d, 1, "failed")
	if err := os.WriteFile(filepath.Join(d.cfg.LocalDownDir, "a.txt"), []byte("BADBAD"), 0644); err != nil {
		t.Fatal(err)
	}
	p.failAction = false
	d.downloadFile(f)
	assertStatus(t, d, 1, "failed")
	if len(p.actions) != 1 {
		t.Fatalf("source cleanup ran on modified file: %v", p.actions)
	}
}

func TestZeroLengthFilePublishesSuccessfully(t *testing.T) {
	d, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {})
	d.downloadFile(file(1, "empty", 0))
	assertStatus(t, d, 1, "done")
	stat, err := os.Stat(filepath.Join(d.cfg.LocalDownDir, "empty"))
	if err != nil || stat.Size() != 0 {
		t.Fatalf("stat=%v err=%v", stat, err)
	}
}

func TestOverlappingScansAndConcurrentSnapshots(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	d, p := setup(t, func(w http.ResponseWriter, r *http.Request) { close(started); <-release; io.WriteString(w, "abc") })
	p.lists["/queue"] = []baiduapi.FileInfo{file(1, "a.txt", 3)}
	if err := d.ScanAndDownload(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("download did not start")
	}
	if err := d.ScanAndDownload(); err != nil {
		close(release)
		t.Fatal(err)
	}
	go func() {
		defer close(finished)
		for i := 0; i < 500; i++ {
			d.GetTasks()
			d.GetTaskByFsID(1)
			d.UpdateClient(baiduapi.NewClient("test", "test"))
			d.SetDownloadMode(DownloadModeKeep)
		}
	}()
	close(release)
	<-finished
	deadline := time.Now().Add(5 * time.Second)
	for {
		d.taskMutex.RLock()
		active := len(d.inFlight)
		d.taskMutex.RUnlock()
		if active == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("download did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	assertStatus(t, d, 1, "done")
	if len(p.actions) != 1 {
		t.Fatalf("duplicate source processing: %v", p.actions)
	}
}
