package downloader

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestTaskEventPageReadsBoundedEventsFromQueueStore(t *testing.T) {
	root := t.TempDir()
	queueDir := filepath.Join(root, stateDirectory, "queue")
	store, err := taskstore.Open(queueDir, "/preview")
	if err != nil {
		t.Fatal(err)
	}
	job := []byte(`{"version":1,"file":{"fs_id":42,"path":"/preview/a.txt","server_filename":"a.txt","size":3},"mode":"keep","status":"pending"}`)
	for i := 1; i <= 3; i++ {
		if err := store.SaveWithEvent(42, job, taskstore.Event{Kind: "event"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	d, err := OpenDownloader(&config.Config{LocalDownDir: root, QueueDir: "/preview", MaxParallel: 1}, baiduapi.NewClient("", ""))
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

	events, before, more, err := d.TaskEventPage(42, 0, 2)
	if err != nil || len(events) != 2 || !more || before != events[0].Sequence {
		t.Fatalf("events=%#v cursor=%d more=%v err=%v", events, before, more, err)
	}
}
