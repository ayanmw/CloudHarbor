package downloader

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
	"web_browser_baidu_onekey_upload/internal/foldersync"
)

// VerifyArchive performs no writes and never trusts cached content hashes.
// A confirmed root move is not evidence that all descendants survived intact.
func (a FolderSyncRemote) VerifyArchive(ctx context.Context, c foldersync.Config, s foldersync.Step, run string, expected []foldersync.Entry) (proof foldersync.RemoteArchiveVerification, err error) {
	proof = foldersync.RemoteArchiveVerification{Version: "remote-archive-verification-v2", Root: s.Path, Entries: []foldersync.Entry{}}
	if s.Action != "remote_archive" || s.Remote == nil || s.Remote.Path != s.Path {
		return proof, fmt.Errorf("归档核对缺少固定来源")
	}
	archive, err := syncRemoteArchivePath(c, run, s.Path)
	if err != nil {
		return proof, err
	}
	proof.ArchivePath = archive
	proof.ExpectedSHA256, proof.ExpectedItems = foldersync.SubtreeFingerprint(expected, s.Path)
	var found bool
	seenExpected := map[string]bool{}
	for _, e := range expected {
		if e.Path != s.Path && !strings.HasPrefix(e.Path, s.Path+"/") {
			continue
		}
		if seenExpected[e.Path] || e.ID == "" || !e.Directory && len(e.SHA256) != 64 {
			return proof, fmt.Errorf("归档来源清单不完整")
		}
		seenExpected[e.Path] = true
		if e.Path == s.Path {
			found = e.ID == s.Remote.ID && e.Directory == s.Remote.Directory
		}
	}
	if !found {
		return proof, fmt.Errorf("归档来源根证据缺失")
	}
	client, _, err := a.guard(ctx, c, nil, false)
	if err != nil {
		return proof, err
	}
	info, err := syncInfo(c.RemoteRoot, *s.Remote)
	if err != nil {
		return proof, err
	}
	actual, err := client.GetFileInfo(info.FsID)
	if err != nil {
		return proof, err
	}
	if actual.FsID != info.FsID || actual.Path != archive || actual.IsDir != info.IsDir {
		return proof, fmt.Errorf("归档根身份或位置变化")
	}
	pending := []string{}
	seen := map[int64]bool{}
	add := func(id int64, p string, directory bool, size, mtime int64, md5 string) error {
		if id <= 0 || seen[id] || p != archive && !strings.HasPrefix(p, archive+"/") {
			return fmt.Errorf("归档遍历身份重复或路径越界")
		}
		seen[id] = true
		relative := s.Path + strings.TrimPrefix(p, archive)
		e := foldersync.Entry{ID: strconv.FormatInt(id, 10), Path: relative, Directory: directory, Size: size, Mtime: mtime, MtimeUnit: "unix_seconds", MD5: md5, Observed: time.Now().UTC()}
		if directory {
			e.Size = 0
			pending = append(pending, p)
		} else {
			file := actual
			file.FsID = id
			file.Path = p
			file.ServerFilename = path.Base(p)
			file.IsDir = 0
			file.Size = size
			file.Mtime = mtime
			file.MD5 = md5
			digest, readErr := syncHash(ctx, client, file)
			if readErr != nil {
				return readErr
			}
			e.SHA256 = digest
		}
		proof.Entries = append(proof.Entries, e)
		return nil
	}
	if err = add(actual.FsID, actual.Path, actual.IsDir == 1, actual.Size, actual.Mtime, actual.MD5); err != nil {
		return proof, err
	}
	for len(pending) > 0 {
		if err = ctx.Err(); err != nil {
			return proof, err
		}
		directory := pending[0]
		pending = pending[1:]
		items, e := client.ListDir(directory)
		if e != nil {
			return proof, e
		}
		for _, item := range items {
			if item.IsDir != 0 && item.IsDir != 1 {
				return proof, fmt.Errorf("归档项目类型无效")
			}
			if path.Dir(item.Path) != directory || item.Path != path.Join(directory, item.ServerFilename) || item.ServerFilename == "." || item.ServerFilename == ".." || strings.Contains(item.ServerFilename, "/") {
				return proof, fmt.Errorf("归档列表不是直接子项")
			}
			if err = add(item.FsID, item.Path, item.IsDir == 1, item.Size, item.Mtime, item.MD5); err != nil {
				return proof, err
			}
		}
	}
	proof.ObservedSHA256, proof.ObservedItems = foldersync.SubtreeFingerprint(proof.Entries, s.Path)
	proof.ExpectedContentIdentitySHA256, _ = foldersync.ArchiveContentIdentity(expected, s.Path)
	proof.ObservedContentIdentitySHA256, _ = foldersync.ArchiveContentIdentity(proof.Entries, s.Path)
	proof.MtimeChanges = foldersync.ArchiveMtimeChanges(expected, proof.Entries, s.Path)
	if proof.ExpectedContentIdentitySHA256 != proof.ObservedContentIdentitySHA256 || proof.ExpectedItems != proof.ObservedItems {
		return proof, fmt.Errorf("归档完整子树与原证据不符，保留归档等待核对")
	}
	// Bind completion to a fresh root observation and the same account/root.
	if _, _, err = a.guard(ctx, c, nil, false); err != nil {
		return proof, err
	}
	finalRoot, readErr := client.GetFileInfo(info.FsID)
	if readErr != nil {
		return proof, readErr
	}
	if finalRoot.Path != actual.Path || finalRoot.FsID != actual.FsID || finalRoot.IsDir != actual.IsDir || finalRoot.Mtime != actual.Mtime {
		return proof, fmt.Errorf("归档根在遍历期间变化")
	}
	proof.VerifiedAt = time.Now().UTC()
	return proof, nil
}
