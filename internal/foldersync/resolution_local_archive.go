package foldersync

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/durablefs"
)

type LocalSubtreeArchiveReceipt struct {
	SourcePath     string    `json:"source_path"`
	ArchivePath    string    `json:"archive_path"`
	ExpectedSHA256 string    `json:"expected_sha256"`
	ObservedSHA256 string    `json:"observed_sha256"`
	Items          string    `json:"items"`
	Finished       time.Time `json:"finished_at_utc"`
}

// Caller must persist an intent before invoking this primitive. No directory
// execution entry point is enabled by it. After attempting the rename, any
// error is unknown: preserve the archive and never automatically replay it.
func archiveLocalSubtree(ctx context.Context, c Config, run, p string, expected []Entry) (receipt LocalSubtreeArchiveReceipt, resultErr error) {
	attempted := false
	defer func() {
		if resultErr != nil && !attempted {
			resultErr = &NotStartedError{resultErr}
		}
	}()
	if len(run) != 32 {
		return receipt, fmt.Errorf("归档批次ID无效")
	}
	if _, err := hex.DecodeString(run); err != nil {
		return receipt, err
	}
	rootID, err := rootIdentity(c.LocalRoot)
	if err != nil || rootID != c.LocalID {
		return receipt, fmt.Errorf("本地同步根身份变化")
	}
	source, err := localPath(c.LocalRoot, p)
	if err != nil {
		return receipt, err
	}
	want := index(expected)[p]
	if want == nil {
		return receipt, fmt.Errorf("归档缺少完整来源根")
	}
	expectedHash, expectedCount := subtreeIdentity(expected, p)
	before, err := ScanLocal(ctx, c.LocalRoot)
	if err != nil {
		return receipt, err
	}
	beforeHash, beforeCount := subtreeIdentity(before, p)
	if beforeHash != expectedHash || beforeCount != expectedCount {
		return receipt, fmt.Errorf("归档前完整子树已变化")
	}
	history := filepath.Join(c.LocalRoot, privateDirectory)
	// Every existing component is checked before use. A user-created link must
	// never redirect the private history or its owner marker outside the root.
	ensure := func(path string) error {
		if err := os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || isReparse(info) {
			return fmt.Errorf("归档路径不是独立真实目录")
		}
		return nil
	}
	if err = ensure(history); err != nil {
		return receipt, err
	}
	owner := filepath.Join(history, "owner")
	if info, e := os.Lstat(owner); e == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || isReparse(info) {
			return receipt, fmt.Errorf("归档所有权文件无效")
		}
	} else if !os.IsNotExist(e) {
		return receipt, e
	}
	raw, err := os.ReadFile(owner)
	if os.IsNotExist(err) {
		f, e := os.OpenFile(owner, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return receipt, e
		}
		_, e = f.WriteString(c.ID)
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			return receipt, e
		}
	} else if err != nil {
		return receipt, err
	} else if string(raw) != c.ID {
		return receipt, fmt.Errorf("归档目录所属配置不符")
	}
	targetRoot := filepath.Join(history, run, "archive")
	current := history
	parts := append([]string{run, "archive"}, strings.Split(p, "/")...)
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		if err = ensure(current); err != nil {
			return receipt, err
		}
	}
	target := filepath.Join(targetRoot, filepath.FromSlash(p))
	if _, err = os.Lstat(target); !os.IsNotExist(err) {
		return receipt, fmt.Errorf("固定归档目标已占用")
	}
	// History creation may take time; recheck the entire selected tree immediately
	// before the single namespace mutation, rather than checking its root alone.
	before, err = ScanLocal(ctx, c.LocalRoot)
	if err != nil {
		return receipt, err
	}
	beforeHash, beforeCount = subtreeIdentity(before, p)
	if beforeHash != expectedHash || beforeCount != expectedCount {
		return receipt, fmt.Errorf("归档发布前子树已变化")
	}
	if err = ctx.Err(); err != nil {
		return receipt, err
	}
	receipt = LocalSubtreeArchiveReceipt{SourcePath: source, ArchivePath: target, ExpectedSHA256: expectedHash, Items: expectedCount}
	attempted = true
	if err = durablefs.PublishNoReplace(source, target); err != nil {
		return receipt, err
	}
	for dir := filepath.Dir(target); ; dir = filepath.Dir(dir) {
		if err = durablefs.SyncDirectory(dir); err != nil {
			return receipt, err
		}
		if dir == c.LocalRoot {
			break
		}
		if filepath.Dir(dir) == dir {
			return receipt, fmt.Errorf("归档持久化边界无效")
		}
	}
	if err = durablefs.SyncDirectory(filepath.Dir(source)); err != nil {
		return receipt, err
	}
	// Read from the archive namespace while retaining original relative paths.
	// ScanLocal(target) would treat a legitimate nested .nas-sync-history as
	// its own reserved history and omit it; walking this selected root avoids it.
	observed := []Entry{}
	err = filepath.WalkDir(target, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, e := filepath.Rel(targetRoot, path)
		if e != nil {
			return e
		}
		entry, e := localEntry(ctx, targetRoot, filepath.ToSlash(relative))
		if e != nil {
			return e
		}
		if entry == nil {
			return fmt.Errorf("归档项目读取期间消失")
		}
		observed = append(observed, *entry)
		return nil
	})
	if err != nil {
		return receipt, err
	}
	observedHash, observedCount := subtreeIdentity(observed, p)
	receipt.ObservedSHA256 = observedHash
	if observedHash != expectedHash || observedCount != expectedCount {
		return receipt, fmt.Errorf("归档后子树校验不符；保留归档等待核对")
	}
	if _, err = os.Lstat(source); !os.IsNotExist(err) {
		return receipt, fmt.Errorf("原路径重新出现，归档保留待核对")
	}
	receipt.Finished = time.Now().UTC()
	return receipt, nil
}
