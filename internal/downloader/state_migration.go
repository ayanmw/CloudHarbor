package downloader

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"web_browser_baidu_onekey_upload/internal/durablefs"
)

// PrepareStateDirectory relocates the old queue store out of the download
// directory after verifying a byte-for-byte copy. A conflicting destination
// fails closed rather than choosing one history silently.
func PrepareStateDirectory(destination, legacy string) error {
	destination, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	legacy, err = filepath.Abs(legacy)
	if err != nil {
		return err
	}
	if destination == legacy {
		return nil
	}
	retired := legacy + ".migrated"
	if err = os.RemoveAll(retired); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	legacyInfo, err := os.Lstat(legacy)
	if errors.Is(err, os.ErrNotExist) {
		return retireLegacyState(legacy, retired)
	}
	if err != nil {
		return err
	}
	if legacyInfo.Mode()&os.ModeSymlink != 0 || !legacyInfo.IsDir() {
		return fmt.Errorf("legacy application state is not a directory")
	}
	if _, err = os.Stat(destination); err == nil {
		same, compareErr := sameStateTree(legacy, destination)
		if compareErr != nil {
			return compareErr
		}
		if !same {
			return fmt.Errorf("both legacy and stable application state exist and differ; refusing to choose one")
		}
		return retireLegacyState(legacy, retired)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(destination)
	if err = durablefs.MkdirAll(parent, 0700); err != nil {
		return err
	}
	temp, err := os.MkdirTemp(parent, ".queue-state-migrate-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	if err = copyStateTree(legacy, temp); err != nil {
		return err
	}
	same, err := sameStateTree(legacy, temp)
	if err != nil {
		return err
	}
	if !same {
		return fmt.Errorf("legacy application state copy verification failed")
	}
	if err = os.Rename(temp, destination); err != nil {
		return err
	}
	return retireLegacyState(legacy, retired)
}

func retireLegacyState(legacy, retired string) error {
	if err := os.Rename(legacy, retired); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return os.RemoveAll(retired)
}

func copyStateTree(source, destination string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		from, to := filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())
		info, err := os.Lstat(from)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link in legacy state: %s", from)
		}
		if info.IsDir() {
			if err = os.Mkdir(to, info.Mode().Perm()); err != nil {
				return err
			}
			if err = copyStateTree(from, to); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file in legacy state: %s", from)
		}
		in, openErr := os.Open(from)
		if openErr != nil {
			return openErr
		}
		out, createErr := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if createErr != nil {
			in.Close()
			return createErr
		}
		_, copyErr := io.Copy(out, in)
		closeInErr, closeOutErr := in.Close(), out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeInErr != nil {
			return closeInErr
		}
		if closeOutErr != nil {
			return closeOutErr
		}
	}
	return nil
}

func sameStateTree(a, b string) (bool, error) {
	left, err := stateTreeDigests(a)
	if err != nil {
		return false, err
	}
	right, err := stateTreeDigests(b)
	if err != nil {
		return false, err
	}
	if len(left) != len(right) {
		return false, nil
	}
	for name, digest := range left {
		if right[name] != digest {
			return false, nil
		}
	}
	return true, nil
}

func stateTreeDigests(root string) (map[string][32]byte, error) {
	result := make(map[string][32]byte)
	var files []string
	err := filepath.WalkDir(root, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if name == root {
			return nil
		}
		info, err := os.Lstat(name)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("unsupported entry in state tree: %s", name)
		}
		if info.Mode().IsRegular() {
			relative, err := filepath.Rel(root, name)
			if err != nil {
				return err
			}
			files = append(files, relative)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	for _, relative := range files {
		body, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			return nil, err
		}
		result[relative] = sha256.Sum256(body)
	}
	return result, nil
}
