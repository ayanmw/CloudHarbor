package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishDownloadedFileFallsBackToVerifiedCopy(t *testing.T) {
	dir := t.TempDir()
	partial := filepath.Join(dir, "content.part")
	final := filepath.Join(dir, "result.bin")
	content := []byte("verified fallback content")
	if err := os.WriteFile(partial, content, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	originalLink := linkDownloadedFile
	linkDownloadedFile = func(string, string) error { return errors.New("hard links unsupported") }
	t.Cleanup(func() { linkDownloadedFile = originalLink })

	if err := publishDownloadedFile(context.Background(), partial, final, hex.EncodeToString(digest[:]), int64(len(content))); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(final)
	if err != nil || string(got) != string(content) {
		t.Fatalf("published content = %q, err=%v", got, err)
	}
	finalInfo, err := os.Stat(final)
	if err != nil {
		t.Fatal(err)
	}
	partialInfo, err := os.Stat(partial)
	if err != nil || os.SameFile(finalInfo, partialInfo) {
		t.Fatalf("fallback did not create an independent file: final=%v partial=%v err=%v", finalInfo, partialInfo, err)
	}
}

func TestPublishDownloadedFileFallbackDoesNotReplaceExistingDestination(t *testing.T) {
	dir := t.TempDir()
	partial := filepath.Join(dir, "content.part")
	final := filepath.Join(dir, "result.bin")
	content := []byte("verified fallback content")
	if err := os.WriteFile(partial, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(final, []byte("keep existing file"), 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	originalLink := linkDownloadedFile
	linkDownloadedFile = func(string, string) error { return errors.New("hard links unsupported") }
	t.Cleanup(func() { linkDownloadedFile = originalLink })

	if err := publishDownloadedFile(context.Background(), partial, final, hex.EncodeToString(digest[:]), int64(len(content))); err == nil {
		t.Fatal("fallback replaced an existing destination")
	}
	got, err := os.ReadFile(final)
	if err != nil || string(got) != "keep existing file" {
		t.Fatalf("existing destination changed: %q err=%v", got, err)
	}
	if _, err = os.Stat(partial); err != nil {
		t.Fatalf("source partial was lost after collision: %v", err)
	}
}

func TestPublishDownloadedFileFallbackRejectsContentChangedAfterHash(t *testing.T) {
	dir := t.TempDir()
	partial := filepath.Join(dir, "content.part")
	final := filepath.Join(dir, "result.bin")
	if err := os.WriteFile(partial, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	originalLink := linkDownloadedFile
	linkDownloadedFile = func(string, string) error { return errors.New("hard links unsupported") }
	t.Cleanup(func() { linkDownloadedFile = originalLink })

	if err := publishDownloadedFile(context.Background(), partial, final, "not-the-file-hash", 7); err == nil {
		t.Fatal("fallback published content that no longer matched the verified digest")
	}
	if _, err := os.Stat(final); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unverified final file exists: %v", err)
	}
	if _, err := os.Stat(partial); err != nil {
		t.Fatalf("source partial was lost after digest mismatch: %v", err)
	}
}

func TestRecoveryRemovesOnlyVerifiedCopiedPartial(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "result.bin")
	partial := filepath.Join(dir, "content.part")
	metadata := filepath.Join(dir, "partial.json")
	content := []byte("verified fallback content")
	if err := os.WriteFile(final, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partial, content, 0600); err != nil {
		t.Fatal(err)
	}
	info := file(911, "result.bin", int64(len(content)))
	expected := recordFor(info, "result.bin")
	if err := writeRecord(metadata, expected); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	receipt := expected
	receipt.SHA256 = hex.EncodeToString(digest[:])
	finalInfo, err := os.Stat(final)
	if err != nil {
		t.Fatal(err)
	}
	removeVerifiedPublishedPartial(context.Background(), finalInfo, receipt, expected, partial, metadata)
	if _, err = os.Stat(partial); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("verified copied partial was not cleaned: %v", err)
	}

	if err = os.WriteFile(partial, []byte("different contents"), 0600); err != nil {
		t.Fatal(err)
	}
	removeVerifiedPublishedPartial(context.Background(), finalInfo, receipt, expected, partial, metadata)
	if got, readErr := os.ReadFile(partial); readErr != nil || string(got) != "different contents" {
		t.Fatalf("unverified partial was changed: %q err=%v", got, readErr)
	}
}
