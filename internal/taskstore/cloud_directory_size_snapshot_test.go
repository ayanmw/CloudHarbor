package taskstore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCloudDirectorySizesSnapshotUsesMatchedIdentitiesAndOneGeneration(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	now := time.Date(2026, 11, 6, 2, 0, 0, 0, time.UTC)
	if err = s.UpsertCloudInventoryPage("acct", "run", "/", []CloudInventoryEntry{{FsID: 1, Path: "/a", Name: "a", Directory: true}, {FsID: 2, Path: "/zero", Name: "zero", Directory: true}}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.UpsertCloudInventoryPage("acct", "run", "/a", []CloudInventoryEntry{{FsID: 3, Path: "/a/file", Name: "file", Size: 9007199254740993}}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.UpsertCloudInventoryPage("acct", "run", "/zero", nil, now, nil); err != nil {
		t.Fatal(err)
	}
	ids := []CloudDirectoryIdentity{{FsID: 1, Path: "/a"}, {FsID: 2, Path: "/zero"}, {FsID: 9, Path: "/missing"}}
	got, err := s.CloudDirectorySizesSnapshot(context.Background(), "acct", ids)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Sizes[1].Complete || got.Sizes[1].Bytes != 9007199254740993 || !got.Sizes[2].Complete || got.Sizes[2].Bytes != 0 || got.Sizes[9].Complete || got.Generation <= 0 || got.ReadAtUTC.IsZero() {
		t.Fatalf("snapshot %+v", got)
	}
	replaced, err := s.CloudDirectorySizesSnapshot(context.Background(), "acct", []CloudDirectoryIdentity{{FsID: 99, Path: "/a"}})
	if err != nil || replaced.Sizes[99].Complete || replaced.Sizes[99].Bytes != 0 {
		t.Fatalf("old identity leaked %+v %v", replaced, err)
	}
	other, err := s.CloudDirectorySizesSnapshot(context.Background(), "other", ids)
	if err != nil || other.Sizes[1].Complete || other.Generation != 0 {
		t.Fatalf("scope leak %+v %v", other, err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := s.CloudDirectorySizesSnapshot(context.Background(), "acct", ids)
	if err != nil || reopened.Generation != got.Generation || reopened.Sizes[1].Bytes != got.Sizes[1].Bytes {
		t.Fatalf("restart %+v %v", reopened, err)
	}
	if err = s.InvalidateCloudInventory("acct", []string{"/a"}); err != nil {
		t.Fatal(err)
	}
	fresh, err := s.CloudDirectorySizesSnapshot(context.Background(), "acct", ids)
	if err != nil || fresh.Generation <= got.Generation || fresh.Sizes[1].Complete {
		t.Fatalf("stale generation %+v %v", fresh, err)
	}
}

func TestCloudDirectorySizesSnapshotRejectsInvalidRequestsAndRollsBackDerivedWrites(t *testing.T) {
	s, err := Open(t.TempDir(), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	ids := []CloudDirectoryIdentity{{FsID: 1, Path: "/a"}, {FsID: 2, Path: "/b"}}
	if err = s.UpsertCloudInventoryPage("acct", "run", "/", []CloudInventoryEntry{{FsID: 1, Path: "/a", Name: "a", Directory: true}, {FsID: 2, Path: "/b", Name: "b", Directory: true}}, now, nil); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/a", "/b"} {
		if err = s.UpsertCloudInventoryPage("acct", "run", path, nil, now, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, bad := range [][]CloudDirectoryIdentity{{{FsID: 0, Path: "/a"}}, {{FsID: 1, Path: "/"}}, {{FsID: 1, Path: "relative"}}, {{FsID: 1, Path: "/a"}, {FsID: 1, Path: "/b"}}, {{FsID: 1, Path: "/a"}, {FsID: 2, Path: "/a"}}} {
		if _, err = s.CloudDirectorySizesSnapshot(context.Background(), "acct", bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := s.CloudDirectorySizesSnapshot(ctx, "acct", ids)
	if !errors.Is(err, context.Canceled) || got.Sizes != nil {
		t.Fatalf("cancel exposed snapshot %+v %v", got, err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER fail_second_size BEFORE INSERT ON cloud_directory_size_cache WHEN NEW.path='/b' BEGIN SELECT RAISE(ABORT,'injected snapshot failure'); END`); err != nil {
		t.Fatal(err)
	}
	got, err = s.CloudDirectorySizesSnapshot(context.Background(), "acct", ids)
	if err == nil || got.Sizes != nil {
		t.Fatalf("partial snapshot returned %+v %v", got, err)
	}
	var cached int
	if err = s.db.QueryRow(`SELECT count(*) FROM cloud_directory_size_cache WHERE account_fingerprint='acct'`).Scan(&cached); err != nil || cached != 0 {
		t.Fatalf("partial derived writes committed %d %v", cached, err)
	}
	if _, err = s.db.Exec(`DROP TRIGGER fail_second_size`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CloudDirectorySizesSnapshot(context.Background(), "acct", ids); err != nil {
		t.Fatal(err)
	}

	// Both directory/directory and directory/file collisions are ambiguous.
	for _, kind := range []int{0, 1} {
		if _, err = s.db.Exec(`INSERT INTO cloud_inventory(account_fingerprint,fs_id,parent_path,path,name,is_dir,size_bytes,provider_mtime,content_md5,observed_at_utc,last_seen_run_id) SELECT account_fingerprint,1001,parent_path,path,name,?,size_bytes,provider_mtime,content_md5,observed_at_utc,last_seen_run_id FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=1`, kind); err != nil {
			t.Fatal(err)
		}
		got, err = s.CloudDirectorySizesSnapshot(context.Background(), "acct", ids)
		if err != nil || got.Sizes[1].Complete || !got.Sizes[2].Complete {
			t.Fatalf("ambiguous path kind=%d accepted %+v %v", kind, got, err)
		}
		if _, err = s.db.Exec(`DELETE FROM cloud_inventory WHERE account_fingerprint='acct' AND fs_id=1001`); err != nil {
			t.Fatal(err)
		}
	}
}
