package taskstore

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenMigratesV41DownloadManifestsAndRecoversCommittedManifest(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "upgrade", true: "manifest_recovery"}[committed], func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "queue")
			s, err := Open(dir, "/queue")
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Save(42, []byte(`{"status":"paused"}`)); err != nil {
				t.Fatal(err)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", sqliteDataSourceName(filepath.Join(dir, "queue.sqlite")))
			if err != nil {
				t.Fatal(err)
			}
			if !committed {
				_, err = db.Exec(`DROP TABLE download_manifest_states; DROP TABLE download_manifest_results; DROP TABLE download_manifest_totals; DROP TABLE download_manifest_current; DROP TABLE download_manifest_entries; DROP TABLE download_manifests; UPDATE store_meta SET value='41' WHERE key='schema_version'`)
			}
			db.Close()
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(manifest{Version: 41, Scope: "/queue"})
			if err = os.WriteFile(filepath.Join(dir, "manifest.json"), body, 0600); err != nil {
				t.Fatal(err)
			}
			s, err = Open(dir, "/queue")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			want := "migrated_v41"
			if committed {
				want = "recovered_manifest"
			}
			if s.migrationAction != want {
				t.Fatalf("action=%s want=%s", s.migrationAction, want)
			}
			var count int
			if err = s.db.QueryRow(`SELECT count(*) FROM jobs WHERE id=42`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("old job lost count=%d err=%v", count, err)
			}
			if err = s.db.QueryRow(`SELECT count(*) FROM download_manifests`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("invented history count=%d err=%v", count, err)
			}
		})
	}
}
