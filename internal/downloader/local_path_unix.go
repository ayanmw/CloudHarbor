//go:build !windows

package downloader

import (
	"os"
	"path/filepath"
)

func localPathReparse(os.FileInfo) bool { return false }

func resolveExistingLocalPath(name string) (string, error) {
	return filepath.EvalSymlinks(name)
}
