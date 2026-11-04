package taskstore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownloadManifestSummaryCountsFilesAndEvidenceAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	now := time.Now().UTC()
	id := "66666666666666666666666666666666"
	if err = s.Save(42, []byte(`{"status":"paused"}`)); err != nil {
		t.Fatal(err)
	}
	if m, err := s.CurrentDownloadManifestSummary(ctx, 42); err != nil || m != nil {
		t.Fatalf("invented old summary=%+v err=%v", m, err)
	}
	if err = s.BeginDownloadManifest(ctx, DownloadManifest{ID: id, RootID: 42, RootPath: "/queue/root", AttemptID: id, StartedAtUTC: now}); err != nil {
		t.Fatal(err)
	}
	entries := []DownloadManifestEntry{{FsID: 42, Path: "/queue/root", Name: "root", IsDir: 1, RelativePath: "root", ObservedAtUTC: now}, {FsID: 43, Path: "/queue/root/empty", Name: "empty", IsDir: 1, RelativePath: "root/empty", ObservedAtUTC: now}, {FsID: 44, Path: "/queue/root/a", Name: "a", Size: 123, RelativePath: "root/a", ObservedAtUTC: now}, {FsID: 45, Path: "/queue/root/b", Name: "b", Size: 456, RelativePath: "root/b", ObservedAtUTC: now}}
	if err = s.AppendDownloadManifestEntries(ctx, id, entries); err != nil {
		t.Fatal(err)
	}
	if m, err := s.CurrentDownloadManifestSummary(ctx, 42); err != nil || m != nil {
		t.Fatalf("draft appeared=%+v err=%v", m, err)
	}
	if err = s.PublishDownloadManifest(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordDownloadManifestResult(ctx, id, entries[2], DownloadManifestResult{Kind: "published", SHA256: strings.Repeat("a", 64), RecordedAtUTC: now}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER inject_summary_result_failure AFTER INSERT ON download_manifest_results BEGIN SELECT RAISE(ABORT,'injected result rollback'); END`); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordDownloadManifestResult(ctx, id, entries[3], DownloadManifestResult{Kind: "published", SHA256: strings.Repeat("b", 64), RecordedAtUTC: now}); err == nil {
		t.Fatal("result failure ignored")
	}
	if _, err = s.db.Exec(`DROP TRIGGER inject_summary_result_failure`); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.CurrentDownloadManifestSummary(ctx, 42)
	if err != nil || m == nil || m.Files != 2 || m.Folders != 2 || m.Size != 579 || m.Completed != 1 || m.CompletedBytes != 123 || m.Root.Path != "/queue/root" || m.Manifest.AttemptID != id {
		t.Fatalf("summary=%+v err=%v", m, err)
	}
}
