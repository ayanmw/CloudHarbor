package downloader

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
)

func TestManagedPathsPersistAcrossRestartAndKeepStoreScope(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	initialLocal := filepath.Join(root, "downloads")
	cfg := &config.Config{QueueDir: "/old-monitor", LocalDownDir: initialLocal, StateDir: state, MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	newLocal := filepath.Join(root, "new-downloads")
	want := ManagedPaths{QueueDir: "/new-monitor", LocalDownDir: newLocal}
	if _, err = d.SetManagedPaths(context.Background(), want, false); err != nil {
		t.Fatal(err)
	}
	if got := d.GetManagedPaths(); got.QueueDir != want.QueueDir || !sameLocalPath(got.LocalDownDir, want.LocalDownDir) {
		t.Fatalf("runtime path not updated: %#v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = d.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenDownloader(&config.Config{QueueDir: "/old-monitor", LocalDownDir: initialLocal, StateDir: state, MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Shutdown(context.Background())
	got := reopened.GetManagedPaths()
	if got.QueueDir != want.QueueDir || !sameLocalPath(got.LocalDownDir, want.LocalDownDir) {
		t.Fatalf("managed paths not restored: %#v", got)
	}
	if _, err = os.Stat(newLocal); err != nil {
		t.Fatalf("new local directory was not created: %v", err)
	}
	if err = reopened.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	explicitLocal := filepath.Join(root, "environment-downloads")
	explicit, err := OpenDownloader(&config.Config{QueueDir: "/environment-monitor", QueueDirExplicit: true, LocalDownDir: explicitLocal, LocalDownDirExplicit: true, StateDir: state, MaxParallel: 1}, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer explicit.Shutdown(context.Background())
	got = explicit.GetManagedPaths()
	if got.QueueDir != "/environment-monitor" || !sameLocalPath(got.LocalDownDir, explicitLocal) {
		t.Fatalf("explicit startup paths must override web settings: %#v", got)
	}
}

func TestManagedPathsRejectRunningTasksAndExplicitEnvironment(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{QueueDir: "/queue", LocalDownDir: filepath.Join(root, "downloads"), StateDir: filepath.Join(root, "state"), MaxParallel: 1}
	d, err := OpenDownloader(cfg, baiduapi.NewClient("token", "app"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Shutdown(context.Background())
	d.queue.mu.Lock()
	d.queue.active[42] = func() {}
	d.queue.mu.Unlock()
	if _, err = d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/new", LocalDownDir: filepath.Join(root, "new")}, false); err == nil {
		t.Fatal("running task should prevent path changes")
	}
	d.queue.mu.Lock()
	delete(d.queue.active, 42)
	d.queue.mu.Unlock()
	cfg.QueueDirExplicit = true
	if _, err = d.SetManagedPaths(context.Background(), ManagedPaths{QueueDir: "/new", LocalDownDir: filepath.Join(root, "next")}, false); err == nil {
		t.Fatal("explicit environment queue path should be immutable in the web settings path")
	}
	if got := d.GetManagedPaths().QueueDir; got != "/queue" {
		t.Fatalf("rejected change modified active path: %s", got)
	}
}
