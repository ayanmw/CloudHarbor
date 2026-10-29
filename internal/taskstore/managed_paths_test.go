package taskstore

import (
	"path/filepath"
	"testing"
)

func TestOpenUsingExistingScopePreservesStoreIdentityAfterPathChange(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(dir, "/old-monitor")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if store, err = Open(dir, "/new-monitor"); err == nil {
		store.Close()
		t.Fatal("ordinary open must continue rejecting an implicit scope change")
	}
	store, err = OpenUsingExistingScope(dir, "/new-monitor")
	if err != nil {
		t.Fatalf("managed path change must preserve the existing store scope: %v", err)
	}
	defer store.Close()
	if store.scope != "/old-monitor" {
		t.Fatalf("store identity changed unexpectedly: %q", store.scope)
	}
}

func TestReadSettingsFileReturnsAbsentOrValidSettings(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "queue")
	body, err := ReadSettingsFile(dir)
	if err != nil || body != nil {
		t.Fatalf("absent settings: %q, %v", body, err)
	}
	store, err := Open(dir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SaveSettings([]byte(`{"download_mode":"keep","managed_queue_dir":"/next"}`)); err != nil {
		t.Fatal(err)
	}
	store.Close()
	body, err = ReadSettingsFile(dir)
	if err != nil || string(body) != `{"download_mode":"keep","managed_queue_dir":"/next"}` {
		t.Fatalf("settings: %q, %v", body, err)
	}
}
