package downloader

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func TestDisplaySizeListingBuildsWholeDirectoryThenPinsPages(t *testing.T) {
	d, p := setup(t, nil)
	s, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = s
	defer shutdown(t, d)
	if err = s.BindAccount("123"); err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	ctx := context.Background()
	client, _, err := d.authorizedCloudClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ns := client.CloudCacheNamespace()
	now := time.Now().UTC()
	files := []baiduapi.FileInfo{{FsID: 2001, Path: "/docs/known", ServerFilename: "known", IsDir: 1}, {FsID: 2002, Path: "/docs/zero", ServerFilename: "zero", IsDir: 1}, {FsID: 2003, Path: "/docs/unknown", ServerFilename: "unknown", IsDir: 1}}
	for n := 1104; n >= 0; n-- {
		name := fmt.Sprintf("f%04d", n)
		files = append(files, baiduapi.FileInfo{FsID: int64(n + 1), Path: "/docs/" + name, ServerFilename: name, Size: 50})
	}
	p.lists["/docs"] = files
	if err = s.UpsertCloudInventoryPage(ns, "test", "/", []taskstore.CloudInventoryEntry{{FsID: 2000, Path: "/docs", Name: "docs", Directory: true}}, now, nil); err != nil {
		t.Fatal(err)
	}
	var cached []taskstore.CloudInventoryEntry
	for _, f := range files {
		cached = append(cached, taskstore.CloudInventoryEntry{FsID: f.FsID, Path: f.Path, Name: f.ServerFilename, Directory: f.IsDir == 1, Size: f.Size})
	}
	if err = s.UpsertCloudInventoryPage(ns, "test", "/docs", cached, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.UpsertCloudInventoryPage(ns, "test", "/docs/known", []taskstore.CloudInventoryEntry{{FsID: 3000, Path: "/docs/known/file", Name: "file", Size: 5000}}, now, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.UpsertCloudInventoryPage(ns, "test", "/docs/zero", nil, now, nil); err != nil {
		t.Fatal(err)
	}
	first, err := d.ListCloudDisplaySize(ctx, "/docs", 0, 100, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.DisplaySize == nil || first.Total == nil || *first.Total != len(files) || first.Entries[0].Name != "zero" || first.DisplaySize.UnknownDirectories != 1 {
		t.Fatalf("first page %+v", first)
	}
	token := first.DisplaySize.SnapshotID
	// A token cannot be reused under another grouping mode or sort field.
	if _, e := d.ListCloudDisplayOrdered(ctx, "/docs", 0, 100, baiduapi.DirectoryOrder{Field: "name"}, true, token); !errors.Is(e, ErrCloudDisplaySizeSnapshotChanged) {
		t.Fatalf("cross-order token accepted: %v", e)
	}
	if _, e := d.ListCloudDisplayOrdered(ctx, "/docs", 0, 100, baiduapi.DirectoryOrder{Field: "size"}, false, token); !errors.Is(e, ErrCloudDisplaySizeSnapshotChanged) {
		t.Fatalf("cross-mode token accepted: %v", e)
	}

	if len(token) != 32 {
		t.Fatal("invalid token")
	}
	first.Entries[0].Name = "tampered"
	first.DisplaySize.CacheGeneration = -99
	seen := map[int64]bool{}
	var ordered []CloudEntry
	for offset := 0; offset < len(files); offset += 100 {
		page, e := d.ListCloudDisplaySize(ctx, "/docs", offset, 100, false, token)
		if e != nil {
			t.Fatal(e)
		}
		for _, entry := range page.Entries {
			if seen[entry.FsID] {
				t.Fatal("duplicate across pages")
			}
			seen[entry.FsID] = true
			ordered = append(ordered, entry)
		}
	}
	if len(seen) != len(files) || ordered[0].Name != "zero" || ordered[len(ordered)-1].Name != "unknown" || ordered[len(ordered)-2].Name != "known" || ordered[len(ordered)-2].Size != 5000 || ordered[len(ordered)-2].ProviderSize != 0 {
		t.Fatal("whole-list order or identity corrupted")
	}
	for n := 0; n < 1105; n++ {
		if ordered[n+1].Name != fmt.Sprintf("f%04d", n) {
			t.Fatalf("name tie lost %d", n)
		}
	}
	p.mu.Lock()
	requests := append([]string(nil), p.listRequests...)
	p.mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("paging reread provider: %v", requests)
	}
	for _, r := range requests {
		if r != "/docs" {
			t.Fatalf("recursive read: %s", r)
		}
	}
	if _, err = d.ListCloudDisplaySize(ctx, "/other", 0, 100, false, token); !errors.Is(err, ErrCloudDisplaySizeSnapshotChanged) {
		t.Fatalf("scope accepted %v", err)
	}
	if _, err = d.ListCloudDisplaySize(ctx, "/docs", 0, 100, true, token); !errors.Is(err, ErrCloudDisplaySizeSnapshotChanged) {
		t.Fatalf("direction accepted %v", err)
	}
	desc, err := d.ListCloudDisplaySize(ctx, "/docs", 0, 100, true, "")
	if err != nil || desc.Entries[0].Name != "known" || desc.Entries[1].Name != "f0000" {
		t.Fatalf("descending display order %+v %v", desc, err)
	}

	// Name/time grouping must also be established before pagination, not per page.
	for _, field := range []string{"name", "time"} {
		for _, mixed := range []bool{false, true} {
			order := baiduapi.DirectoryOrder{Field: field, Descending: true}
			first, e := d.ListCloudDisplayOrdered(ctx, "/docs", 0, 100, order, mixed, "")
			if e != nil {
				t.Fatal(e)
			}
			seenIDs := map[int64]bool{}
			passedDirectoryGroup := false
			p.mu.Lock()
			before := len(p.listRequests)
			p.mu.Unlock()
			for off := 0; off < len(files); off += 100 {
				page, e := d.ListCloudDisplayOrdered(ctx, "/docs", off, 100, order, mixed, first.DisplaySize.SnapshotID)
				if e != nil {
					t.Fatal(e)
				}
				if page.DisplaySize.Mixed != mixed || *page.Order != order {
					t.Fatal("mode/order response lost")
				}
				for _, entry := range page.Entries {
					if seenIDs[entry.FsID] {
						t.Fatal("duplicate page identity")
					}
					seenIDs[entry.FsID] = true
					if !mixed {
						if !entry.Directory {
							passedDirectoryGroup = true
						} else if passedDirectoryGroup {
							t.Fatal("directory appeared after file across pages")
						}
					}
				}
			}
			if len(seenIDs) != len(files) {
				t.Fatal("missing cross-page identity")
			}
			p.mu.Lock()
			after := len(p.listRequests)
			p.mu.Unlock()
			if after != before {
				t.Fatal("snapshot paging reread provider")
			}
		}
	}
	if err = s.InvalidateCloudInventory(ns, []string{"/docs/known"}); err != nil {
		t.Fatal(err)
	}
	if _, err = d.ListCloudDisplaySize(ctx, "/docs", 100, 100, false, token); !errors.Is(err, ErrCloudDisplaySizeSnapshotChanged) {
		t.Fatalf("generation accepted %v", err)
	}
}

func TestDisplaySizeListingRejectsCancelledAndAmbiguousBuildsWithoutPublishing(t *testing.T) {
	for _, mode := range []string{"cancelled", "ambiguous"} {
		t.Run(mode, func(t *testing.T) {
			d, p := setup(t, nil)
			s, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
			if err != nil {
				t.Fatal(err)
			}
			d.queue.store = s
			defer shutdown(t, d)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p.lists["/docs"] = []baiduapi.FileInfo{{FsID: 1, Path: "/docs/a", ServerFilename: "a", Size: 1}}
			want := ErrIntakeConflict
			if mode == "ambiguous" {
				p.lists["/docs"] = append(p.lists["/docs"], baiduapi.FileInfo{FsID: 2, Path: "/docs/a", ServerFilename: "a", Size: 2})
			} else {
				want = context.Canceled
				p.beforeList = func(string) { cancel() }
			}
			if _, err = d.ListCloudDisplaySize(ctx, "/docs", 0, 100, false, ""); !errors.Is(err, want) {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			d.displaySizeMu.Lock()
			cached := len(d.displaySizeSnapshots)
			d.displaySizeMu.Unlock()
			if cached != 0 {
				t.Fatal("failed build published snapshot")
			}
		})
	}
}

func TestDisplaySizeSnapshotRetentionIsBoundedAndExpiredTokensAreNotReused(t *testing.T) {
	d, p := setup(t, nil)
	s, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = s
	defer shutdown(t, d)
	now := time.Now().UTC()
	for index := 0; index <= displaySizeSnapshotLimit; index++ {
		id := fmt.Sprintf("%032x", index)
		d.rememberDisplaySizeSnapshot(&cloudDisplaySizeSnapshot{info: CloudDisplaySizeInfo{SnapshotID: id, CreatedAtUTC: now.Add(time.Duration(index) * time.Second), ExpiresAtUTC: now.Add(time.Hour)}})
	}
	if len(d.displaySizeSnapshots) != displaySizeSnapshotLimit || d.displaySizeSnapshots[fmt.Sprintf("%032x", 0)] != nil {
		t.Fatal("retention not bounded")
	}
	page, err := d.ListCloudDisplaySize(context.Background(), "/docs", 0, 100, false, "")
	if err != nil {
		t.Fatal(err)
	}
	token := page.DisplaySize.SnapshotID
	d.displaySizeMu.Lock()
	d.displaySizeSnapshots[token].info.ExpiresAtUTC = time.Now().Add(-time.Second)
	d.displaySizeMu.Unlock()
	p.mu.Lock()
	before := len(p.listRequests)
	p.mu.Unlock()
	if _, err = d.ListCloudDisplaySize(context.Background(), "/docs", 0, 100, false, token); !errors.Is(err, ErrCloudDisplaySizeSnapshotChanged) {
		t.Fatalf("expired accepted %v", err)
	}
	p.mu.Lock()
	after := len(p.listRequests)
	p.mu.Unlock()
	if after != before {
		t.Fatal("expired token started new read automatically")
	}
	// Simulate loss of ephemeral views at restart; this is not a process-crash test.
	d.displaySizeMu.Lock()
	d.displaySizeSnapshots = nil
	d.displaySizeMu.Unlock()
	if _, err = d.ListCloudDisplaySize(context.Background(), "/docs", 0, 100, false, token); !errors.Is(err, ErrCloudDisplaySizeSnapshotChanged) {
		t.Fatalf("missing view accepted %v", err)
	}
}

func TestDisplaySizeSnapshotDoesNotCrossApplicationOrAuthenticationBoundary(t *testing.T) {
	d, p := setup(t, nil)
	s, err := taskstore.Open(filepath.Join(d.cfg.LocalDownDir, stateDirectory, "queue"), d.cfg.QueueDir)
	if err != nil {
		t.Fatal(err)
	}
	d.queue.store = s
	defer shutdown(t, d)
	if err = s.BindAccount("123"); err != nil {
		t.Fatal(err)
	}
	d.SetAuthentication(d.getClient(), true, time.Now().Add(time.Hour))
	ctx := context.Background()
	first, err := d.ListCloudDisplaySize(ctx, "/docs", 0, 100, false, "")
	if err != nil {
		t.Fatal(err)
	}
	token := first.DisplaySize.SnapshotID
	// A token cannot be reused under another grouping mode or sort field.
	if _, e := d.ListCloudDisplayOrdered(ctx, "/docs", 0, 100, baiduapi.DirectoryOrder{Field: "name"}, true, token); !errors.Is(e, ErrCloudDisplaySizeSnapshotChanged) {
		t.Fatalf("cross-order token accepted: %v", e)
	}
	if _, e := d.ListCloudDisplayOrdered(ctx, "/docs", 0, 100, baiduapi.DirectoryOrder{Field: "size"}, false, token); !errors.Is(e, ErrCloudDisplaySizeSnapshotChanged) {
		t.Fatalf("cross-mode token accepted: %v", e)
	}

	d.SetAuthentication(baiduapi.NewClient("synthetic-replacement", "other-app"), true, time.Now().Add(time.Hour))
	p.mu.Lock()
	before := len(p.listRequests)
	p.mu.Unlock()
	if _, err = d.ListCloudDisplaySize(ctx, "/docs", 0, 100, false, token); !errors.Is(err, ErrCloudDisplaySizeSnapshotChanged) {
		t.Fatalf("application boundary crossed %v", err)
	}
	d.SetAuthentication(d.getClient(), false, time.Now().Add(time.Hour))
	if _, err = d.ListCloudDisplaySize(ctx, "/docs", 0, 100, false, token); !errors.Is(err, ErrAuthenticationWaiting) {
		t.Fatalf("authentication boundary crossed %v", err)
	}
	p.mu.Lock()
	after := len(p.listRequests)
	p.mu.Unlock()
	if after != before {
		t.Fatal("invalid scope automatically read provider")
	}
}
