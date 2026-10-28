package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
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
	expected := recordFor(file(921, "result.bin", int64(len(content))), "result.bin")
	metadata := expected
	if err := writeRecord(filepath.Join(dir, "partial.json"), metadata); err != nil {
		t.Fatal(err)
	}
	expected.SHA256 = hex.EncodeToString(digest[:])
	originalLink := linkDownloadedFile
	linkDownloadedFile = func(string, string) error { return errors.New("hard links unsupported") }
	t.Cleanup(func() { linkDownloadedFile = originalLink })

	if err := publishDownloadedFile(context.Background(), partial, final, expected); err != nil {
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
	registered, err := readRecord(filepath.Join(dir, "partial.json"))
	if err != nil || registered.PublishTempName == "" || !registered.PublishTempOwned {
		t.Fatalf("copy temporary was not durably registered: %+v err=%v", registered, err)
	}
	if err = cleanupRegisteredPublishTemp(dir, filepath.Join(dir, "partial.json"), &expected); err != nil {
		t.Fatal(err)
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
	expected := recordFor(file(922, "result.bin", int64(len(content))), "result.bin")
	if err := writeRecord(filepath.Join(dir, "partial.json"), expected); err != nil {
		t.Fatal(err)
	}
	expected.SHA256 = hex.EncodeToString(digest[:])
	originalLink := linkDownloadedFile
	linkDownloadedFile = func(string, string) error { return errors.New("hard links unsupported") }
	t.Cleanup(func() { linkDownloadedFile = originalLink })

	if err := publishDownloadedFile(context.Background(), partial, final, expected); err == nil {
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
	expected := recordFor(file(923, "result.bin", 7), "result.bin")
	if err := writeRecord(filepath.Join(dir, "partial.json"), expected); err != nil {
		t.Fatal(err)
	}
	expected.SHA256 = "not-the-file-hash"
	originalLink := linkDownloadedFile
	linkDownloadedFile = func(string, string) error { return errors.New("hard links unsupported") }
	t.Cleanup(func() { linkDownloadedFile = originalLink })

	if err := publishDownloadedFile(context.Background(), partial, final, expected); err == nil {
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

func TestCleanupRegisteredPublishTemporaryAfterInterruptedCopy(t *testing.T) {
	dir := t.TempDir()
	metadataPath := filepath.Join(dir, "partial.json")
	temporaryName := ".nas-download-publish-ab12cd34.tmp"
	temporaryPath := filepath.Join(dir, temporaryName)
	if err := os.WriteFile(temporaryPath, []byte("incomplete copy"), 0600); err != nil {
		t.Fatal(err)
	}
	metadata := recordFor(file(912, "result.bin", 20), "result.bin")
	metadata.PublishTempName = temporaryName
	metadata.PublishTempOwned = true
	if err := writeRecord(metadataPath, metadata); err != nil {
		t.Fatal(err)
	}

	if err := cleanupRegisteredPublishTemp(dir, metadataPath, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(temporaryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("registered interrupted copy was not removed: %v", err)
	}
	cleaned, err := readRecord(metadataPath)
	if err != nil || cleaned.PublishTempName != "" {
		t.Fatalf("temporary name was not cleared durably: %+v err=%v", cleaned, err)
	}
}

func TestCleanupRegisteredPublishTemporaryRejectsPathEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "keep.tmp")
	if err := os.WriteFile(outside, []byte("user file"), 0600); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(dir, "partial.json")
	metadata := recordFor(file(913, "result.bin", 1), "result.bin")
	metadata.PublishTempName = filepath.Join("..", filepath.Base(filepath.Dir(outside)), filepath.Base(outside))
	metadata.PublishTempOwned = true
	if err := writeRecord(metadataPath, metadata); err != nil {
		t.Fatal(err)
	}
	if err := cleanupRegisteredPublishTemp(dir, metadataPath, nil); err == nil {
		t.Fatal("accepted a publish temporary path outside the target directory")
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "user file" {
		t.Fatalf("path escape changed user file: %q err=%v", got, err)
	}
}

func TestCleanupUnownedPublishNamePreservesExistingFile(t *testing.T) {
	dir := t.TempDir()
	metadataPath := filepath.Join(dir, "partial.json")
	name := ".nas-download-publish-0123456789abcdef0123456789abcdef.tmp"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("unowned file"), 0600); err != nil {
		t.Fatal(err)
	}
	metadata := recordFor(file(915, "result.bin", 12), "result.bin")
	metadata.PublishTempName = name
	if err := writeRecord(metadataPath, metadata); err != nil {
		t.Fatal(err)
	}
	if err := cleanupRegisteredPublishTemp(dir, metadataPath, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "unowned file" {
		t.Fatalf("unowned file was removed or changed: %q err=%v", got, err)
	}
	cleaned, err := readRecord(metadataPath)
	if err != nil || cleaned.PublishTempName != "" || cleaned.PublishTempOwned {
		t.Fatalf("unowned intent was not cleared: %+v err=%v", cleaned, err)
	}
}

func TestTransferFailsClosedOnCorruptPartialMetadata(t *testing.T) {
	d, _ := setup(t, func(http.ResponseWriter, *http.Request) {
		t.Error("download request sent with corrupt partial metadata")
	})
	info := file(914, "result.bin", 7)
	local := filepath.Join(d.cfg.LocalDownDir, "result.bin")
	if err := os.MkdirAll(filepath.Dir(local), 0700); err != nil {
		t.Fatal(err)
	}
	stateHash := sha256.Sum256([]byte("result.bin"))
	state := filepath.Join(d.cfg.LocalDownDir, stateDirectory, hex.EncodeToString(stateHash[:]))
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(state, "content.part")
	metadata := filepath.Join(state, "partial.json")
	if err := os.WriteFile(partial, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metadata, []byte("{corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := d.transfer(context.Background(), info, "result.bin", local); err == nil {
		t.Fatal("transfer overwrote corrupt partial metadata")
	}
	if got, err := os.ReadFile(partial); err != nil || string(got) != "partial" {
		t.Fatalf("partial content changed after metadata error: %q err=%v", got, err)
	}
	if got, err := os.ReadFile(metadata); err != nil || string(got) != "{corrupt" {
		t.Fatalf("partial metadata changed after corruption: %q err=%v", got, err)
	}
}
