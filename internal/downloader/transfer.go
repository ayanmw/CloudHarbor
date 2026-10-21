package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

const stateDirectory = ".nas-downloader"

type fileRecord struct {
	FsID         int64  `json:"fs_id"`
	Size         int64  `json:"size"`
	Mtime        int64  `json:"mtime"`
	CloudMD5     string `json:"cloud_md5"`
	RelativePath string `json:"relative_path"`
	SHA256       string `json:"sha256,omitempty"`
}

func recordFor(info baiduapi.FileInfo, relative string) fileRecord {
	return fileRecord{FsID: info.FsID, Size: info.Size, Mtime: info.Mtime, CloudMD5: info.MD5, RelativePath: relative}
}
func (r fileRecord) matches(other fileRecord) bool {
	r.SHA256 = ""
	other.SHA256 = ""
	return r == other
}

func validName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00:") {
		return false
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	return true
}
func (d *Downloader) localPath(relative string) (string, error) {
	if filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" {
		return "", fmt.Errorf("absolute cloud path rejected")
	}
	components := strings.FieldsFunc(relative, func(r rune) bool { return r == '/' || r == '\\' })
	if len(components) == 0 {
		return "", fmt.Errorf("empty cloud path")
	}
	for _, name := range components {
		if !validName(name) {
			return "", fmt.Errorf("invalid cloud path %q", relative)
		}
	}
	if strings.EqualFold(components[0], stateDirectory) {
		return "", fmt.Errorf("reserved local state directory")
	}
	root, err := filepath.Abs(d.cfg.LocalDownDir)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, relative)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("cloud path escapes download root")
	}
	if err = rejectSymlinks(root, rel); err != nil {
		return "", err
	}
	return target, nil
}
func rejectSymlinks(root, relative string) error {
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		stat, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if stat.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link rejected: %s", current)
		}
	}
	return nil
}
func ensureDirectory(dir string) error { return os.MkdirAll(dir, 0755) }

func readRecord(name string) (fileRecord, error) {
	var record fileRecord
	body, err := os.ReadFile(name)
	if err == nil {
		err = json.Unmarshal(body, &record)
	}
	return record, err
}
func writeRecord(name string, record fileRecord) error {
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(name), "record-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err = temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err = temp.Write(body); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, name)
}
func hashFile(name string) (string, error) {
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (d *Downloader) transfer(ctx context.Context, info baiduapi.FileInfo, relative, local string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if info.Size < 0 {
		return fmt.Errorf("negative cloud file size")
	}
	if err := ensureDirectory(filepath.Dir(local)); err != nil {
		return err
	}
	root, err := filepath.Abs(d.cfg.LocalDownDir)
	if err != nil {
		return err
	}
	key := sha256.Sum256([]byte(relative))
	stateRel := filepath.Join(stateDirectory, hex.EncodeToString(key[:]))
	if err = rejectSymlinks(root, stateRel); err != nil {
		return err
	}
	state := filepath.Join(root, stateRel)
	if err = os.MkdirAll(state, 0700); err != nil {
		return err
	}
	expected := recordFor(info, relative)
	receiptName := filepath.Join(state, "complete.json")
	partialName := filepath.Join(state, "content.part")
	metaName := filepath.Join(state, "partial.json")
	for _, name := range []string{"content.part", "partial.json", "complete.json"} {
		if err = rejectSymlinks(state, name); err != nil {
			return err
		}
	}
	if stat, statErr := os.Stat(local); statErr == nil {
		record, recordErr := readRecord(receiptName)
		if stat.IsDir() || recordErr != nil || !record.matches(expected) || record.SHA256 == "" || stat.Size() != info.Size {
			return fmt.Errorf("本地文件已存在且无法确认身份，请移走或更名后重试: %s", local)
		}
		digest, hashErr := hashFile(local)
		if hashErr != nil {
			return hashErr
		}
		if digest != record.SHA256 {
			return fmt.Errorf("本地文件 SHA256 与完成记录不符，保留原文件: %s", local)
		}
		d.updateProgress(info.FsID, info.Size)
		return nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}

	record, recordErr := readRecord(metaName)
	var offset int64
	if recordErr == nil && record.matches(expected) {
		if stat, statErr := os.Stat(partialName); statErr == nil {
			offset = stat.Size()
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
	}
	if offset > info.Size {
		offset = 0
	}
	if offset == info.Size && offset > 0 {
		// A failed oversized response can fill the partial. Size alone cannot
		// authorize publication: recovery requires a verified success receipt.
		receipt, receiptErr := readRecord(receiptName)
		digest, hashErr := hashFile(partialName)
		if receiptErr != nil || hashErr != nil || !receipt.matches(expected) || receipt.SHA256 == "" || receipt.SHA256 != digest {
			offset = 0
		}
	}
	if offset == 0 {
		// Reset stale/unbound data before associating it with a different version.
		f, openErr := os.OpenFile(partialName, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if openErr != nil {
			return openErr
		}
		if err = f.Close(); err != nil {
			return err
		}
		if err = writeRecord(metaName, expected); err != nil {
			return err
		}
	}
	d.updateProgress(info.FsID, offset)
	if offset < info.Size || info.Size == 0 {
		link, linkErr := d.getClient().WithContext(ctx).GetDownloadLink(info.FsID)
		if linkErr != nil {
			return linkErr
		}
		body, length, downloadErr := d.getClient().WithContext(ctx).DownloadFileWithRange(link, offset)
		if errors.Is(downloadErr, baiduapi.ErrRangeIgnored) {
			offset = 0
			body, length, downloadErr = d.getClient().WithContext(ctx).DownloadFileWithRange(link, 0)
		}
		if downloadErr != nil {
			return downloadErr
		}
		defer body.Close()
		if length >= 0 && length != info.Size-offset {
			return fmt.Errorf("下载响应长度不符: got %d, want %d", length, info.Size-offset)
		}
		flags := os.O_WRONLY
		if offset == 0 {
			flags |= os.O_TRUNC
		}
		out, openErr := os.OpenFile(partialName, flags, 0600)
		if openErr != nil {
			return openErr
		}
		if _, err = out.Seek(offset, io.SeekStart); err != nil {
			out.Close()
			return err
		}
		written := offset
		buf := make([]byte, 128*1024)
		for {
			if err = ctx.Err(); err != nil {
				break
			}
			n, readErr := body.Read(buf)
			if n > 0 {
				if int64(n) > info.Size-written {
					err = fmt.Errorf("download exceeds expected size")
					break
				}
				var count int
				count, err = out.Write(buf[:n])
				written += int64(count)
				d.updateProgress(info.FsID, written)
				if err != nil {
					break
				}
				if count != n {
					err = io.ErrShortWrite
					break
				}
			}
			if readErr != nil {
				if readErr != io.EOF {
					err = readErr
				}
				break
			}
		}
		if err == nil && written != info.Size {
			err = fmt.Errorf("下载长度不符: got %d, want %d", written, info.Size)
		}
		if err == nil {
			err = out.Sync()
		}
		closeErr := out.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	}
	digest, err := hashFile(partialName)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	expected.SHA256 = digest
	// Receipt first: after a crash, a missing final file is recovered from the
	// bound partial; an existing final file is verified against this receipt.
	if err = writeRecord(receiptName, expected); err != nil {
		return err
	}
	// No-replace publication: a concurrently created target must be preserved.
	// Both paths are inside the download volume; hard links are required.
	if err = os.Link(partialName, local); err != nil {
		return fmt.Errorf("发布文件失败（目标存在或文件系统不支持硬链接）: %w", err)
	}
	if err = os.Remove(partialName); err != nil {
		return fmt.Errorf("清理临时文件失败: %w", err)
	}
	d.updateProgress(info.FsID, info.Size)
	return nil
}
