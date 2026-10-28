// Package durablefs provides directory-entry durability for filesystems that
// support syncing directories. Other platforms retain their existing file
// flush behavior because Go has no portable directory-sync operation.
package durablefs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var syncDirectory = platformSyncDirectory
var replaceFile = platformReplaceFile

const maxAtomicWriteWorkers = 4

func SyncDirectory(path string) error { return syncDirectory(path) }

// ReplaceFile atomically replaces destination with source using the platform's
// rename semantics. Callers needing directory durability must also call
// SyncDirectory.
func ReplaceFile(source, destination string) error {
	return replaceFile(source, destination)
}

type File struct {
	Path string
	Data []byte
}

// WriteFilesAtomic prepares and syncs every file before publishing any name,
// renames each file atomically, and syncs their shared directory once.
func WriteFilesAtomic(files []File) error {
	if len(files) == 0 {
		return nil
	}
	directory := filepath.Clean(filepath.Dir(files[0].Path))
	destinations := make([]string, len(files))
	temps := make([]string, len(files))
	seen := make(map[string]struct{}, len(files))
	for i, file := range files {
		if file.Path == "" {
			return fmt.Errorf("empty atomic-write path")
		}
		destination := filepath.Clean(file.Path)
		if filepath.Dir(destination) != directory {
			return fmt.Errorf("atomic-write batch spans directories")
		}
		if _, ok := seen[destination]; ok {
			return fmt.Errorf("duplicate atomic-write destination")
		}
		seen[destination] = struct{}{}
		destinations[i] = destination
	}
	defer func() {
		for _, name := range temps {
			if name != "" {
				_ = os.Remove(name)
			}
		}
	}()
	writeErrors := make([]error, len(files))
	work := make(chan int)
	workers := min(maxAtomicWriteWorkers, len(files))
	var workerWG sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			for i := range work {
				temps[i], writeErrors[i] = writeSyncedTemp(directory, files[i].Data)
			}
		}()
	}
	for i := range files {
		work <- i
	}
	close(work)
	workerWG.Wait()
	for _, err := range writeErrors {
		if err != nil {
			return err
		}
	}
	for i := range destinations {
		if err := ReplaceFile(temps[i], destinations[i]); err != nil {
			// Earlier renames may already be visible. Sync them before returning;
			// callers keep source items intact and can reconcile missing records.
			return errors.Join(err, SyncDirectory(directory))
		}
		temps[i] = ""
	}
	return SyncDirectory(directory)
}

func writeSyncedTemp(directory string, data []byte) (string, error) {
	temp, err := os.CreateTemp(directory, "record-*.tmp")
	if err != nil {
		return "", err
	}
	name := temp.Name()
	writeErr := temp.Chmod(0600)
	if writeErr == nil {
		_, writeErr = temp.Write(data)
	}
	if writeErr == nil {
		writeErr = temp.Sync()
	}
	closeErr := temp.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(name)
		return "", errors.Join(writeErr, closeErr)
	}
	return name, nil
}

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
