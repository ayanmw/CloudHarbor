// Package durablefs provides directory-entry durability for filesystems that
// support syncing directories. Other platforms retain their existing file
// flush behavior because Go has no portable directory-sync operation.
package durablefs

import (
	"fmt"
	"os"
	"path/filepath"
)

var syncDirectory = platformSyncDirectory

func SyncDirectory(path string) error { return syncDirectory(path) }

// MkdirAll makes each newly created directory entry durable in its parent
// before creating the next level. It preserves the permissions of existing
// directories.
func MkdirAll(path string, perm os.FileMode) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	var missing []string
	current := filepath.Clean(absolute)
	for {
		info, statErr := os.Stat(current)
		if statErr == nil {
			if !info.IsDir() {
				return fmt.Errorf("not a directory: %s", current)
			}
			// On retries, make an entry durable even if its earlier mkdir
			// succeeded but syncing the parent failed.
			if err = SyncDirectory(filepath.Dir(current)); err != nil {
				return fmt.Errorf("sync parent of directory %s: %w", current, err)
			}
			break
		}
		if !os.IsNotExist(statErr) {
			return statErr
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			return statErr
		}
		current = parent
	}
	for i := len(missing) - 1; i >= 0; i-- {
		name := missing[i]
		if err = os.Mkdir(name, perm); err != nil && !os.IsExist(err) {
			return err
		}
		if err = SyncDirectory(filepath.Dir(name)); err != nil {
			return fmt.Errorf("sync parent of created directory %s: %w", name, err)
		}
	}
	return nil
}
