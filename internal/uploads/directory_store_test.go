package uploads

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDirectoryManifestPersistsWithoutFileStaging(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.createManifest("/target", nil, []string{"root", "root/empty"})
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Items) != 0 || len(b.Directories) != 2 {
		t.Fatalf("batch=%+v", b)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	got, err := m.Get(b.ID)
	if err != nil || len(got.Directories) != 2 || len(got.Items) != 0 {
		t.Fatalf("got=%+v error=%v", got, err)
	}
	for _, d := range got.Directories {
		if d.State != "queued" || d.Updated == "" || d.ActualPath != "" {
			t.Fatalf("directory=%+v", d)
		}
		if _, err := time.Parse(time.RFC3339Nano, d.Updated); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := m.db.QueryRow(`SELECT COUNT(*) FROM upload_directory_events WHERE directory=? AND state='queued'`, d.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("events=%d err=%v", count, err)
		}
	}
}

func TestDirectoryManifestRejectsConflictsAtomically(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, tc := range []struct {
		files []Input
		dirs  []string
	}{
		{nil, []string{"../x"}}, {nil, []string{"/x"}}, {nil, []string{"a/../b"}},
		{nil, []string{"a\\b"}}, {nil, []string{"a\x00b"}}, {nil, []string{"."}},
		{[]Input{{Path: "a", Size: "0"}}, []string{"a"}},
		{[]Input{{Path: "a", Size: "0"}}, []string{"a/b"}},
		{nil, []string{strings.Repeat("x", 256)}},
	} {
		if _, err := m.createManifest("/target", tc.files, tc.dirs); err == nil {
			t.Fatalf("accepted=%+v", tc)
		}
	}
	var count int
	if err := m.db.QueryRow(`SELECT COUNT(*) FROM batches`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial batches=%d err=%v", count, err)
	}
	if _, err := m.createManifest("/target", []Input{{Path: "root/a", Size: "0"}}, []string{"root", "root", "root/empty"}); err != nil {
		t.Fatal(err)
	}
}

func TestDirectoryManifestCombinedLimit(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	dirs := make([]string, 10000)
	for i := range dirs {
		dirs[i] = "same"
	}
	if _, err = m.createManifest("/target", []Input{{Path: "file", Size: "0"}}, dirs); err == nil {
		t.Fatal("combined input limit bypassed by duplicate directories")
	}
}

func TestDirectoryManifestRetainsLegacyFilesAndBlocksUnfinishedDirectories(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	legacy, err := m.Create("/target", []Input{{Path: "old", Size: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(legacy.ID)
	if err != nil || len(got.Items) != 1 || len(got.Directories) != 0 {
		t.Fatalf("legacy=%+v err=%v", got, err)
	}
	b, err := m.createManifest("/target", []Input{{Path: "root/a", Size: "1"}}, []string{"root"})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Receive(context.Background(), b.ID, b.Items[0].ID, strings.NewReader("a")); err != nil {
		t.Fatal(err)
	}
	claimed, err := m.claimUploadAttempt(b.Items[0].ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil || claimed {
		t.Fatalf("file admitted before directory completion: claimed=%v err=%v", claimed, err)
	}
}

func TestDirectoryManifestRollsBackWhenInitialEvidenceFails(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	_, err = m.db.Exec(`CREATE TRIGGER fail_directory_evidence BEFORE INSERT ON upload_directory_events BEGIN SELECT RAISE(ABORT,'injected evidence failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.createManifest("/target", []Input{{Path: "root/a", Size: "1"}}, []string{"root"}); err == nil {
		t.Fatal("ignored evidence failure")
	}
	for _, table := range []string{"batches", "items", "upload_directories", "upload_directory_events"} {
		var n int
		if err = m.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s retained %d rows: %v", table, n, err)
		}
	}
}

func TestDirectoryManifestRestartIsolatesSendingWithHistoricalEvidence(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.createManifest("/target", nil, []string{"root"})
	if err != nil {
		t.Fatal(err)
	}
	prior := "2026-11-01T01:02:03Z"
	if _, err = m.db.Exec(`UPDATE upload_directories SET state='sending',updated=?,operation_id='operation-test' WHERE id=?`, prior, b.Directories[0].ID); err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	got, err := m.Get(b.ID)
	if err != nil || len(got.Directories) != 1 || got.Directories[0].State != "unknown" || got.Directories[0].OperationID != "operation-test" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	var evidence, recorded string
	if err = m.db.QueryRow(`SELECT evidence,recorded FROM upload_directory_events WHERE directory=? AND state='unknown'`, b.Directories[0].ID).Scan(&evidence, &recorded); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(evidence, prior) || !strings.Contains(evidence, "sending") || recorded == prior {
		t.Fatalf("recovery evidence=%q recorded=%q", evidence, recorded)
	}
}

func TestDirectoryManifestOpensLegacyDatabaseWithoutChangingFileHistory(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Create("/target", []Input{{Path: "legacy.txt", Size: "1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Receive(context.Background(), b.ID, b.Items[0].ID, strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	var before int
	if err = m.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	// Remove only the new empty tables to represent the prior schema.
	if _, err = m.db.Exec(`DROP TABLE upload_directory_events; DROP TABLE upload_directories`); err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	got, err := m.Get(b.ID)
	if err != nil || len(got.Items) != 1 || got.Items[0].State != "queued" || len(got.Directories) != 0 {
		t.Fatalf("legacy=%+v err=%v", got, err)
	}
	var after int
	if err = m.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&after); err != nil || after != before {
		t.Fatalf("file events changed %d -> %d: %v", before, after, err)
	}
}
