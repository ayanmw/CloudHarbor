package taskstore

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestDownloadManifestPublishIsAtomicAndSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	if err = s.Save(42, []byte(`{"status":"paused"}`)); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	root := DownloadManifestEntry{FsID: 42, Path: "/queue/root", Name: "root", IsDir: 1, RelativePath: "root", ObservedAtUTC: now}
	child := DownloadManifestEntry{FsID: 43, Path: "/queue/root/文件.bin", Name: "文件.bin", Size: 123, RelativePath: "root/文件.bin", ObservedAtUTC: now}
	first := "11111111111111111111111111111111"
	begin := func(id string) {
		t.Helper()
		if err := s.BeginDownloadManifest(ctx, DownloadManifest{ID: id, RootID: 42, RootPath: root.Path, StartedAtUTC: now}); err != nil {
			t.Fatal(err)
		}
	}
	begin(first)
	if err = s.AppendDownloadManifestEntries(ctx, first, []DownloadManifestEntry{root, child}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.CurrentDownloadManifest(ctx, 42); err != nil || got != nil {
		t.Fatalf("draft visible=%+v err=%v", got, err)
	}
	if err = s.PublishDownloadManifest(ctx, first, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	second := "22222222222222222222222222222222"
	begin(second)
	if err = s.AppendDownloadManifestEntries(ctx, second, []DownloadManifestEntry{root}); err != nil {
		t.Fatal(err)
	}
	if err = s.AppendDownloadManifestEntries(ctx, second, []DownloadManifestEntry{child, child}); err == nil {
		t.Fatal("duplicate identity accepted")
	}
	entries, err := s.DownloadManifestEntries(ctx, second, 0, 20)
	if err != nil || len(entries) != 1 {
		t.Fatalf("failed batch partially committed: %+v err=%v", entries, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.CurrentDownloadManifest(ctx, 42)
	if err != nil || current == nil || current.ID != first || current.PublishedAtUTC == nil {
		t.Fatalf("lost complete pointer=%+v err=%v", current, err)
	}
	entries, err = s.DownloadManifestEntries(ctx, first, 1, 1)
	if err != nil || len(entries) != 1 || entries[0].FsID != 43 || !entries[0].ObservedAtUTC.Equal(now) {
		t.Fatalf("page=%+v err=%v", entries, err)
	}
	if err = s.AppendDownloadManifestEntries(ctx, first, []DownloadManifestEntry{child}); err == nil {
		t.Fatal("published manifest was mutable")
	}
	if err = s.PublishDownloadManifest(ctx, second, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	current, err = s.CurrentDownloadManifest(ctx, 42)
	if err != nil || current.ID != second {
		t.Fatalf("replacement=%+v err=%v", current, err)
	}
}

func TestDownloadManifestRejectsIncompleteTreeAndUnboundedBatches(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	if err = s.Save(42, []byte(`{"status":"pending"}`)); err != nil {
		t.Fatal(err)
	}
	id := "33333333333333333333333333333333"
	if err = s.BeginDownloadManifest(ctx, DownloadManifest{ID: id, RootID: 42, RootPath: "/queue/root", StartedAtUTC: now}); err != nil {
		t.Fatal(err)
	}
	root := DownloadManifestEntry{FsID: 42, Path: "/queue/root", Name: "root", IsDir: 1, RelativePath: "root", ObservedAtUTC: now}
	orphan := DownloadManifestEntry{FsID: 44, Path: "/queue/root/missing/file", Name: "file", RelativePath: "root/missing/file", ObservedAtUTC: now}
	if err = s.AppendDownloadManifestEntries(ctx, id, []DownloadManifestEntry{root, orphan}); err != nil {
		t.Fatal(err)
	}
	if err = s.PublishDownloadManifest(ctx, id, now); err == nil {
		t.Fatal("orphan tree published")
	}
	batch := make([]DownloadManifestEntry, 513)
	for i := range batch {
		batch[i] = DownloadManifestEntry{FsID: int64(100 + i), Path: fmt.Sprintf("/queue/root/%d", i), Name: fmt.Sprint(i), RelativePath: fmt.Sprintf("root/%d", i), ObservedAtUTC: now}
	}
	if err = s.AppendDownloadManifestEntries(ctx, id, batch); err == nil {
		t.Fatal("unbounded batch accepted")
	}
	if _, err = s.DownloadManifestEntries(ctx, id, 0, 201); err == nil {
		t.Fatal("unbounded page accepted")
	}
}

func TestDownloadManifestPublicationRollbackAndObservationSources(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	id := "44444444444444444444444444444444"
	if err = s.Save(42, []byte(`{"status":"pending"}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.BeginDownloadManifest(ctx, DownloadManifest{ID: id, RootID: 42, RootPath: "/queue/root", StartedAtUTC: now}); err != nil {
		t.Fatal(err)
	}
	entries := []DownloadManifestEntry{{FsID: 42, Path: "/queue/root", Name: "root", IsDir: 1, RelativePath: "root", ObservedAtUTC: now}, {FsID: 43, Path: "/queue/root/file", Name: "file", RelativePath: "root/file", ObservedAtUTC: now}}
	if err = s.AppendDownloadManifestEntries(ctx, id, entries); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER inject_download_pointer_failure BEFORE INSERT ON download_manifest_current BEGIN SELECT RAISE(ABORT,'injected pointer failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err = s.PublishDownloadManifest(ctx, id, now); err == nil {
		t.Fatal("injected publication error ignored")
	}
	var published sql.NullInt64
	if err = s.db.QueryRow(`SELECT published_at_utc FROM download_manifests WHERE manifest_id=?`, id).Scan(&published); err != nil || published.Valid {
		t.Fatalf("partially sealed manifest=%+v err=%v", published, err)
	}
	var totals int
	if err = s.db.QueryRow(`SELECT count(*) FROM download_manifest_totals WHERE manifest_id=?`, id).Scan(&totals); err != nil || totals != 0 {
		t.Fatalf("partially published totals=%d err=%v", totals, err)
	}
	if _, err = s.db.Exec(`DROP TRIGGER inject_download_pointer_failure`); err != nil {
		t.Fatal(err)
	}
	if err = s.PublishDownloadManifest(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	actual, err := s.DownloadManifestEntries(ctx, id, 0, 10)
	if err != nil || len(actual) != 2 || actual[0].ObservationSource != "root_task_snapshot" || actual[1].ObservationSource != "directory_listing" {
		t.Fatalf("observation provenance=%+v err=%v", actual, err)
	}
}
