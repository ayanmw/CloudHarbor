package taskstore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDownloadManifestResultRequiresSealedMatchingFileAndPreservesFirstEvidence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	s, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	id := "55555555555555555555555555555555"
	if err = s.Save(42, []byte(`{"status":"pending"}`)); err != nil {
		t.Fatal(err)
	}
	if err = s.BeginDownloadManifest(ctx, DownloadManifest{ID: id, RootID: 42, RootPath: "/queue/file", StartedAtUTC: now}); err != nil {
		t.Fatal(err)
	}
	entry := DownloadManifestEntry{FsID: 42, Path: "/queue/file", Name: "file", Size: 3, Mtime: 123, CloudMD5: "provider-hash", RelativePath: "file", ObservedAtUTC: now}
	if err = s.AppendDownloadManifestEntries(ctx, id, []DownloadManifestEntry{entry}); err != nil {
		t.Fatal(err)
	}
	result := DownloadManifestResult{Kind: "published", SHA256: strings.Repeat("a", 64), RecordedAtUTC: now}
	if err = s.RecordDownloadManifestResult(ctx, id, entry, result); err == nil {
		t.Fatal("draft accepted file completion")
	}
	if err = s.PublishDownloadManifest(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*DownloadManifestEntry){
		"identity":      func(e *DownloadManifestEntry) { e.FsID++ },
		"path":          func(e *DownloadManifestEntry) { e.Path += "-changed" },
		"name":          func(e *DownloadManifestEntry) { e.Name += "-changed" },
		"size":          func(e *DownloadManifestEntry) { e.Size++ },
		"mtime":         func(e *DownloadManifestEntry) { e.Mtime++ },
		"provider hash": func(e *DownloadManifestEntry) { e.CloudMD5 += "-changed" },
		"relative path": func(e *DownloadManifestEntry) { e.RelativePath += "-changed" },
		"directory":     func(e *DownloadManifestEntry) { e.IsDir = 1 },
	} {
		changed := entry
		change(&changed)
		if err = s.RecordDownloadManifestResult(ctx, id, changed, result); err == nil {
			t.Fatalf("changed %s accepted", name)
		}
	}
	if got, err := s.DownloadManifestFileResult(ctx, id, 42); err != nil || got != nil {
		t.Fatalf("invented result=%+v err=%v", got, err)
	}
	if err = s.RecordDownloadManifestResult(ctx, id, entry, result); err != nil {
		t.Fatal(err)
	}
	later := result
	later.Kind = "local_verified"
	later.RecordedAtUTC = now.Add(time.Hour)
	if err = s.RecordDownloadManifestResult(ctx, id, entry, later); err != nil {
		t.Fatal(err)
	}
	if summary, err := s.CurrentDownloadManifestSummary(ctx, 42); err != nil || summary.Completed != 1 || summary.CompletedBytes != 3 {
		t.Fatalf("duplicate result double counted: %+v err=%v", summary, err)
	}
	later.SHA256 = strings.Repeat("b", 64)
	if err = s.RecordDownloadManifestResult(ctx, id, entry, later); err == nil {
		t.Fatal("conflicting digest overwritten")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.DownloadManifestFileResult(ctx, id, 42)
	if err != nil || got == nil || *got != result {
		t.Fatalf("first result changed=%+v err=%v", got, err)
	}
	if _, err = s.db.Exec(`UPDATE download_manifest_results SET kind='local_verified'`); err == nil {
		t.Fatal("result evidence is mutable")
	}
	if _, err = s.db.Exec(`DELETE FROM download_manifest_results`); err == nil {
		t.Fatal("result evidence is deletable")
	}
}
