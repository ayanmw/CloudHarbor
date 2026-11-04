package taskstore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownloadManifestFilePageFiltersSortsAndDowngradesRestartedTransfers(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	now := time.Now().UTC()
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err = s.Save(42, []byte(`{"status":"paused"}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.BeginDownloadManifest(ctx, DownloadManifest{ID: id, RootID: 42, RootPath: "/queue/root", StartedAtUTC: now}); err != nil {
		t.Fatal(err)
	}
	entries := []DownloadManifestEntry{{FsID: 42, Path: "/queue/root", Name: "root", IsDir: 1, RelativePath: "root", ObservedAtUTC: now}}
	for i, name := range []string{"z-live", "a-wait", "b-done", "Ä文件%_", "failed"} {
		entries = append(entries, DownloadManifestEntry{FsID: int64(43 + i), Path: "/queue/root/" + name, Name: name, RelativePath: "root/" + name, Size: 3, ObservedAtUTC: now})
	}
	if err = s.AppendDownloadManifestEntries(ctx, id, entries); err != nil {
		t.Fatal(err)
	}
	if err = s.PublishDownloadManifest(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordDownloadManifestState(ctx, id, entries[1], "downloading", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordDownloadManifestState(ctx, id, entries[5], "failed", "文件处理失败"); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordDownloadManifestResult(ctx, id, entries[3], DownloadManifestResult{Kind: "published", SHA256: strings.Repeat("a", 64), RecordedAtUTC: now}); err != nil {
		t.Fatal(err)
	}
	page, err := s.DownloadManifestFilePage(ctx, id, "", "", 0, 2, true)
	if err != nil || page.Total != 5 || len(page.Items) != 2 || page.Items[0].Entry.FsID != 43 || page.Items[1].Status != "failed" || page.Counts["done"] != 1 || page.Counts["pending"] != 2 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	page, err = s.DownloadManifestFilePage(ctx, id, "pending", "ä文件%_", 0, 200, true)
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Entry.FsID != 46 {
		t.Fatalf("unicode literal search=%+v err=%v", page, err)
	}
	page, err = s.DownloadManifestFilePage(ctx, id, "downloading", "", 0, 200, false)
	if err != nil || page.Total != 0 {
		t.Fatalf("inactive root showed active transfers=%+v err=%v", page, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	page, err = s.DownloadManifestFilePage(ctx, id, "pending", "z-live", 0, 200, true)
	if err != nil || page.Total != 1 || !page.Items[0].Interrupted || page.Items[0].Status != "pending" {
		t.Fatalf("restart kept downloading=%+v err=%v", page, err)
	}
	page, err = s.DownloadManifestFilePage(ctx, id, "done", "", 0, 200, true)
	if err != nil || page.Total != 1 || page.Items[0].Result == nil || page.Items[0].Result.Kind != "published" {
		t.Fatalf("completion lost=%+v err=%v", page, err)
	}
	if _, err = s.DownloadManifestFilePage(ctx, id, "", "", 0, 201, true); err == nil {
		t.Fatal("unbounded page accepted")
	}
}
