package taskstore

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDownloadManifestCountsUseTotalsAndUnfinishedStates(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	id := strings.Repeat("c", 32)
	if err = s.Save(42, []byte(`{"status":"paused"}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.BeginDownloadManifest(ctx, DownloadManifest{ID: id, RootID: 42, RootPath: "/queue/root", StartedAtUTC: now}); err != nil {
		t.Fatal(err)
	}
	entries := []DownloadManifestEntry{{FsID: 42, Path: "/queue/root", Name: "root", IsDir: 1, RelativePath: "root", ObservedAtUTC: now}}
	for i, name := range []string{"active", "done", "wait", "failed"} {
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
	if err = s.RecordDownloadManifestState(ctx, id, entries[4], "failed", "文件处理失败"); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordDownloadManifestResult(ctx, id, entries[2], DownloadManifestResult{Kind: "published", SHA256: strings.Repeat("a", 64), RecordedAtUTC: now}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		run    string
		active bool
		want   map[string]int
	}{{s.runID, true, map[string]int{"downloading": 1, "done": 1, "failed": 1, "pending": 1}}, {s.runID, false, map[string]int{"done": 1, "failed": 1, "pending": 2}}, {"old-run", true, map[string]int{"done": 1, "failed": 1, "pending": 2}}} {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		counts, total, err := downloadManifestStatusCounts(ctx, tx, id, tc.run, tc.active)
		tx.Rollback()
		if err != nil || total != 4 || !reflect.DeepEqual(counts, tc.want) {
			t.Fatalf("counts=%v total=%d want=%v err=%v", counts, total, tc.want, err)
		}
	}
	if err = s.RecordDownloadManifestState(ctx, id, entries[3], "auth_wait", "等待授权"); err != nil {
		t.Fatal(err)
	}
	authPage, err := s.DownloadManifestFilePage(ctx, id, "auth_wait", "", 0, 200, true)
	if err != nil || authPage.Total != 1 || authPage.Counts["auth_wait"] != 1 {
		t.Fatalf("auth state=%+v err=%v", authPage, err)
	}
	if err = s.RecordDownloadManifestState(ctx, id, entries[3], "cancelled", "下载已中断"); err != nil {
		t.Fatal(err)
	}
	cancelledPage, err := s.DownloadManifestFilePage(ctx, id, "cancelled", "", 0, 200, true)
	if err != nil || cancelledPage.Total != 1 || cancelledPage.Counts["auth_wait"] != 0 || cancelledPage.Counts["cancelled"] != 1 || cancelledPage.Items[0].Entry.FsID != 45 {
		t.Fatalf("cancelled state=%+v err=%v", cancelledPage, err)
	}
	if _, err = s.db.Exec(`UPDATE download_manifest_totals SET files=completed WHERE manifest_id=?`, id); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, _, err = downloadManifestStatusCounts(ctx, tx, id, s.runID, true); err == nil {
		t.Fatal("contradictory projection returned fake counts")
	}
	tx.Rollback()
	if _, err = s.db.Exec(`UPDATE download_manifest_totals SET files=4 WHERE manifest_id=?`, id); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries[1:] {
		if err = s.RecordDownloadManifestResult(ctx, id, entry, DownloadManifestResult{Kind: "published", SHA256: strings.Repeat("a", 64), RecordedAtUTC: now}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.DownloadManifestFilePage(ctx, id, "pending", "", 0, 200, true)
	if err != nil || page.Total != 0 || len(page.Items) != 0 || !reflect.DeepEqual(page.Counts, map[string]int{"done": 4}) {
		t.Fatalf("completed results left stale pending: %+v err=%v", page, err)
	}
}

func TestDownloadManifestCountsEmptyPublishedDirectory(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	id := strings.Repeat("e", 32)
	if err = s.Save(42, []byte(`{"status":"paused"}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.BeginDownloadManifest(ctx, DownloadManifest{ID: id, RootID: 42, RootPath: "/queue/empty", StartedAtUTC: now}); err != nil {
		t.Fatal(err)
	}
	if err = s.AppendDownloadManifestEntries(ctx, id, []DownloadManifestEntry{{FsID: 42, Path: "/queue/empty", Name: "empty", IsDir: 1, RelativePath: "empty", ObservedAtUTC: now}}); err != nil {
		t.Fatal(err)
	}
	if err = s.PublishDownloadManifest(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	page, err := s.DownloadManifestFilePage(ctx, id, "", "", 0, 200, true)
	if err != nil || page.Total != 0 || len(page.Items) != 0 || len(page.Counts) != 0 {
		t.Fatalf("empty directory became file rows: %+v err=%v", page, err)
	}
}

func TestDownloadStateCountQueryDoesNotReadFullEntriesOrResults(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rows, err := s.db.Query(`EXPLAIN QUERY PLAN `+downloadFileStateCountsSQL, s.runID, true, strings.Repeat("d", 32))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	plans := []string{}
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plans = append(plans, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(plans, "\n")
	if strings.Contains(plan, "download_manifest_entries") || strings.Contains(plan, "download_manifest_results") || !strings.Contains(plan, "download_manifest_states") {
		t.Fatalf("count query reads full inventory: %s", plan)
	}
}
