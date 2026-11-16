package downloader

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadRejectsJunctionPathComponent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "outside")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	createMigrationTestAlias(t, filepath.Join(root, "alias"), target)
	if err := rejectSymlinks(root, "alias"); err == nil {
		t.Fatal("download accepted a junction path component with current Go defaults")
	}
}
