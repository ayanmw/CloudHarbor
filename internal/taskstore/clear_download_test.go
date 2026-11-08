package taskstore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClearDownloadRecordAtomicAndReopen(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "rollback"}[fail], func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "queue")
			s, err := Open(dir, "/queue")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { s.Close() }()
			body := []byte(`{"status":"done"}`)
			if err = s.Save(42, body); err != nil {
				t.Fatal(err)
			}
			if err = s.Save(43, body); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			now := time.Now().UTC()
			id := strings.Repeat("a", 32)
			if err = s.BeginDownloadManifest(ctx, DownloadManifest{ID: id, RootID: 42, RootPath: "/queue/file", StartedAtUTC: now}); err != nil {
				t.Fatal(err)
			}
			entry := DownloadManifestEntry{FsID: 42, Path: "/queue/file", Name: "file", Size: 3, RelativePath: "file", ObservedAtUTC: now}
			if err = s.AppendDownloadManifestEntries(ctx, id, []DownloadManifestEntry{entry}); err != nil {
				t.Fatal(err)
			}
			if err = s.PublishDownloadManifest(ctx, id, now); err != nil {
				t.Fatal(err)
			}
			if err = s.RecordDownloadManifestResult(ctx, id, entry, DownloadManifestResult{Kind: "published", SHA256: strings.Repeat("b", 64), RecordedAtUTC: now}); err != nil {
				t.Fatal(err)
			}

			otherID := strings.Repeat("c", 32)
			if err = s.BeginDownloadManifest(ctx, DownloadManifest{ID: otherID, RootID: 43, RootPath: "/queue/other", StartedAtUTC: now}); err != nil {
				t.Fatal(err)
			}
			otherEntry := entry
			otherEntry.FsID = 43
			otherEntry.Path = "/queue/other"
			otherEntry.Name = "other"
			otherEntry.RelativePath = "other"
			if err = s.AppendDownloadManifestEntries(ctx, otherID, []DownloadManifestEntry{otherEntry}); err != nil {
				t.Fatal(err)
			}
			if err = s.PublishDownloadManifest(ctx, otherID, now); err != nil {
				t.Fatal(err)
			}
			if err = s.RecordDownloadManifestResult(ctx, otherID, otherEntry, DownloadManifestResult{Kind: "published", SHA256: strings.Repeat("d", 64), RecordedAtUTC: now}); err != nil {
				t.Fatal(err)
			}
			if fail {
				if _, err = s.db.Exec(`CREATE TRIGGER reject_cleanup BEFORE DELETE ON jobs BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			err = s.ClearDownloadRecord(ctx, 42, body)
			if (err != nil) != fail {
				t.Fatalf("cleanup err=%v fail=%v", err, fail)
			}
			for _, table := range []string{"jobs", "job_events", "download_manifests", "download_manifest_entries", "download_manifest_current", "download_manifest_results", "download_manifest_totals"} {
				var count int
				query := "SELECT count(*) FROM " + table
				if strings.HasPrefix(table, "download_manifest") {
					if table == "download_manifest_current" {
						query += " WHERE root_id=42"
					} else {
						query += " WHERE manifest_id=\"" + id + "\""
					}
				}
				if table == "jobs" {
					query += " WHERE id=42"
				}
				if table == "job_events" {
					query += " WHERE job_id=42"
				}
				if e := s.db.QueryRow(query).Scan(&count); e != nil {
					t.Fatal(e)
				}
				want := 0
				if fail {
					want = 1
				}
				if count != want {
					t.Fatalf("%s count=%d want=%d", table, count, want)
				}
			}
			if other, err := s.CurrentDownloadManifestSummary(ctx, 43); err != nil || other.Files != 1 {
				t.Fatalf("other manifest lost: %+v %v", other, err)
			}
			if _, err = s.db.Exec(`DELETE FROM download_manifest_results WHERE manifest_id=?`, otherID); err == nil {
				t.Fatal("result append-only guard lost")
			}
			if _, err = s.db.Exec(`DELETE FROM job_events WHERE job_id=43`); err == nil {
				t.Fatal("append-only protection lost")
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = Open(dir, "/queue")
			if err != nil {
				t.Fatal(err)
			}
			records, err := s.Load()
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := records[42]; ok != fail {
				t.Fatal("record returned after restart")
			}
			if _, ok := records[43]; !ok {
				t.Fatal("other task lost")
			}
		})
	}
}
