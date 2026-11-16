package downloader

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/durablefs"
)

const stateDirectory = ".nas-downloader"

var syncPublishedDirectory = durablefs.SyncDirectory
var removePublishedPartial = os.Remove
var linkDownloadedFile = os.Link
var writePartialChunk = func(file *os.File, data []byte) (int, error) { return file.Write(data) }

func publishDownloadedFile(ctx context.Context, partialName, local string, expected fileRecord) error {
	linkErr := linkDownloadedFile(partialName, local)
	if linkErr == nil {
		return nil
	}
	if _, err := os.Lstat(local); err == nil {
		return fmt.Errorf("发布文件失败（目标已存在）: %w", linkErr)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("检查发布目标失败: %w", err)
	}
	if err := copyAndPublishNoReplace(ctx, partialName, local, expected); err != nil {
		return fmt.Errorf("硬链接发布失败，复制回退也未能安全发布: %w", err)
	}
	return nil
}

func copyAndPublishNoReplace(ctx context.Context, partialName, local string, expected fileRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source, err := os.Open(partialName)
	if err != nil {
		return err
	}
	defer source.Close()
	metadataName := filepath.Join(filepath.Dir(partialName), "partial.json")
	metadata, err := readRecord(metadataName)
	if err != nil {
		return err
	}
	if !metadata.matches(expected) {
		return fmt.Errorf("下载部分文件元数据与发布记录不匹配")
	}
	var randomName [16]byte
	if _, err = rand.Read(randomName[:]); err != nil {
		return err
	}
	temporaryName := ".nas-download-publish-" + hex.EncodeToString(randomName[:]) + ".tmp"
	metadata.PublishTempName = temporaryName
	metadata.PublishTempOwned = false
	if err = writeRecord(metadataName, metadata); err != nil {
		return err
	}
	temporaryPath := filepath.Join(filepath.Dir(local), temporaryName)
	temporary, err := os.OpenFile(temporaryPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		metadata.PublishTempName = ""
		clearErr := writeRecord(metadataName, metadata)
		return errors.Join(err, clearErr)
	}
	defer func() {
		_ = temporary.Close()
		if temporaryPath != "" {
			_ = os.Remove(temporaryPath)
		}
	}()
	metadata.PublishTempOwned = true
	if err = writeRecord(metadataName, metadata); err != nil {
		return err
	}
	digest := sha256.New()
	writer := io.MultiWriter(temporary, digest)
	buffer := make([]byte, 256*1024)
	var copied int64
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		count, readErr := source.Read(buffer)
		if count > 0 {
			written, writeErr := writer.Write(buffer[:count])
			copied += int64(written)
			if writeErr != nil {
				return writeErr
			}
			if written != count {
				return io.ErrShortWrite
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if copied != expected.Size || hex.EncodeToString(digest.Sum(nil)) != expected.SHA256 {
		return fmt.Errorf("复制后的下载内容与已验证凭据不一致")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = temporary.Sync(); err != nil {
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	if err = publishFileNoReplace(temporaryPath, local); err != nil {
		return err
	}
	temporaryPath = ""
	return nil
}

func cleanupRegisteredPublishTemp(destinationDir, metadataName string, expected *fileRecord) error {
	metadata, err := readRecord(metadataName)
	if err != nil {
		return err
	}
	if expected != nil && !metadata.matches(*expected) {
		return nil
	}
	name := metadata.PublishTempName
	if name == "" {
		if metadata.PublishTempOwned {
			return fmt.Errorf("registered publish ownership has no temporary name")
		}
		return nil
	}
	if filepath.Base(name) != name || !strings.HasPrefix(name, ".nas-download-publish-") || !strings.HasSuffix(name, ".tmp") {
		return fmt.Errorf("invalid registered publish temporary name")
	}
	if !metadata.PublishTempOwned {
		metadata.PublishTempName = ""
		return writeRecord(metadataName, metadata)
	}
	path := filepath.Join(destinationDir, name)
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("registered publish temporary is not a regular file")
		}
		if err = os.Remove(path); err != nil {
			return err
		}
		if err = syncPublishedDirectory(destinationDir); err != nil {
			return fmt.Errorf("同步已清理的发布临时文件目录失败: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	metadata.PublishTempName = ""
	metadata.PublishTempOwned = false
	return writeRecord(metadataName, metadata)
}

func removeVerifiedPublishedPartial(ctx context.Context, finalInfo os.FileInfo, receipt, expected fileRecord, partialName, metadataName string) {
	partialInfo, err := os.Stat(partialName)
	if err != nil || !partialInfo.Mode().IsRegular() || partialInfo.Size() != expected.Size {
		return
	}
	if os.SameFile(finalInfo, partialInfo) {
		_ = removePublishedPartial(partialName)
		return
	}
	metadata, err := readRecord(metadataName)
	if err != nil || !metadata.matches(expected) || receipt.SHA256 == "" {
		return
	}
	digest, err := hashFile(ctx, partialName)
	if err != nil || digest != receipt.SHA256 {
		return
	}
	// A verified copy may remain if the process stopped after final publication
	// but before unlinking the bound partial. Remove it only after rechecking both
	// the partial's cloud identity and its content against the final receipt.
	_ = removePublishedPartial(partialName)
}

type fileRecord struct {
	FsID             int64  `json:"fs_id"`
	Size             int64  `json:"size"`
	Mtime            int64  `json:"mtime"`
	CloudMD5         string `json:"cloud_md5"`
	RelativePath     string `json:"relative_path"`
	SHA256           string `json:"sha256,omitempty"`
	PublishTempName  string `json:"publish_temp_name,omitempty"`
	PublishTempOwned bool   `json:"publish_temp_owned,omitempty"`
}

func recordFor(info baiduapi.FileInfo, relative string) fileRecord {
	return fileRecord{FsID: info.FsID, Size: info.Size, Mtime: info.Mtime, CloudMD5: info.MD5, RelativePath: relative}
}
func (r fileRecord) matches(other fileRecord) bool {
	r.SHA256 = ""
	other.SHA256 = ""
	r.PublishTempName = ""
	other.PublishTempName = ""
	r.PublishTempOwned = false
	other.PublishTempOwned = false
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
	root, err := filepath.Abs(d.LocalDownloadDirectory())
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
		// Current Go versions classify Windows junctions separately from symlinks.
		if stat.Mode()&os.ModeSymlink != 0 || localPathReparse(stat) {
			return fmt.Errorf("symbolic link rejected: %s", current)
		}
	}
	return nil
}
func ensureDirectory(dir string) error { return durablefs.MkdirAll(dir, 0755) }

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
	if err = durablefs.ReplaceFile(tempName, name); err != nil {
		return err
	}
	return durablefs.SyncDirectory(filepath.Dir(name))
}
func hashFile(ctx context.Context, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return hashReader(ctx, f)
}

func hashReader(ctx context.Context, reader io.Reader) (string, error) {
	hash := sha256.New()
	buffer := make([]byte, 32*1024)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, readErr := reader.Read(buffer)
		// Cancellation during the final Read must not produce a valid digest.
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if n > 0 {
			hash.Write(buffer[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (d *Downloader) transfer(ctx context.Context, info baiduapi.FileInfo, relative, local string) (resultErr error) {
	transferID, idErr := newSourceOperationID()
	if idErr != nil {
		return idErr
	}
	batchID := ""
	d.queue.mu.Lock()
	for _, job := range d.queue.jobs {
		if job.Info.FsID == info.FsID || strings.HasPrefix(info.Path, strings.TrimSuffix(job.Info.Path, "/")+"/") {
			batchID = job.AttemptID
			break
		}
	}
	d.queue.mu.Unlock()
	started := time.Now()
	// Count only writes performed by this invocation, excluding resumed bytes
	// and verified local files reused without another download.
	var writtenThisAttempt int64
	trace := func(event, format string, args ...any) {
		fields := []any{time.Now().UTC().Format(time.RFC3339Nano), transferID, batchID, info.FsID, info.Path, local, event}
		log.Printf("[Download] at_utc=%s transfer_id=%q batch_id=%q fsid=%d path=%q local_path=%q event=%s "+format, append(fields, args...)...)
	}
	trace("transfer_started", "expected_bytes=%d", info.Size)
	defer func() {
		if resultErr == nil {
			trace("transfer_finished", "outcome=success written_bytes_this_attempt=%d expected_bytes=%d elapsed_ms=%d", writtenThisAttempt, info.Size, time.Since(started).Milliseconds())
			return
		}
		status, code := 0, 0
		var apiErr *baiduapi.APIError
		if errors.As(resultErr, &apiErr) {
			status, code = apiErr.Status, apiErr.Code
		}
		outcome := "success"
		if resultErr != nil {
			outcome = "failed"
		}
		if errors.Is(resultErr, context.Canceled) {
			outcome = "cancelled"
		}
		trace("transfer_finished", "outcome=%s written_bytes_this_attempt=%d elapsed_ms=%d http_status=%d provider_code=%d retryable=%t error_type=%T", outcome, writtenThisAttempt, time.Since(started).Milliseconds(), status, code, baiduapi.IsRetryable(resultErr), resultErr)
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if info.Size < 0 {
		return fmt.Errorf("negative cloud file size")
	}
	if err := recordDownloadManifestState(ctx, info, relative, "downloading", ""); err != nil {
		return err
	}
	defer func() {
		if resultErr == nil {
			return
		}
		status, summary := "failed", "文件处理失败，请查看任务事件与下载日志"
		if errors.Is(resultErr, context.Canceled) {
			status, summary = "cancelled", "本次下载已中断"
		} else if errors.Is(resultErr, ErrAuthenticationWaiting) || baiduapi.IsAuthenticationError(resultErr) {
			status, summary = "auth_wait", "等待重新授权"
		}
		// Keep only a bounded, sanitized status after cancellation. The worker
		// still owns its immutable manifest binding until this defer completes.
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := recordDownloadManifestState(saveCtx, info, relative, status, summary); err != nil {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if err := ensureDirectory(filepath.Dir(local)); err != nil {
		return err
	}
	root, err := filepath.Abs(d.LocalDownloadDirectory())
	if err != nil {
		return err
	}
	key := sha256.Sum256([]byte(relative))
	stateRel := filepath.Join(stateDirectory, hex.EncodeToString(key[:]))
	if err = rejectSymlinks(root, stateRel); err != nil {
		return err
	}
	state := filepath.Join(root, stateRel)
	if err = durablefs.MkdirAll(state, 0700); err != nil {
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
		trace("local_verify_started", "bytes=%d reused=true", info.Size)
		digest, hashErr := hashFile(ctx, local)
		if hashErr != nil {
			return hashErr
		}
		if digest != record.SHA256 {
			return fmt.Errorf("本地文件 SHA256 与完成记录不符，保留原文件: %s", local)
		}
		if err = syncPublishedDirectory(filepath.Dir(local)); err != nil {
			return fmt.Errorf("同步已发布文件所在目录失败: %w", err)
		}
		if metadata, metadataErr := readRecord(metaName); metadataErr == nil && metadata.PublishTempName != "" {
			if err = cleanupRegisteredPublishTemp(filepath.Dir(local), metaName, &expected); err != nil {
				return fmt.Errorf("清理已登记的发布临时文件失败: %w", err)
			}
		}
		removeVerifiedPublishedPartial(ctx, stat, record, expected, partialName, metaName)
		d.updateProgress(info.FsID, info.Size)
		trace("local_verified", "bytes=%d reused=true", info.Size)
		return recordDownloadManifestResult(ctx, info, relative, "local_verified", digest)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}

	record, recordErr := readRecord(metaName)
	if recordErr != nil && !errors.Is(recordErr, os.ErrNotExist) {
		return fmt.Errorf("读取部分文件元数据失败，保留现状以避免丢失发布恢复记录: %w", recordErr)
	}
	if recordErr == nil && record.PublishTempName != "" {
		if err = cleanupRegisteredPublishTemp(filepath.Dir(local), metaName, nil); err != nil {
			return fmt.Errorf("恢复并清理中断的发布临时文件失败: %w", err)
		}
		record.PublishTempName = ""
	}
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
		digest, hashErr := hashFile(ctx, partialName)
		// Cancelled verification is not evidence of corrupt partial data. Stop
		// before the zero-offset recovery path can truncate the bound partial.
		if err := ctx.Err(); err != nil {
			return err
		}
		if receiptErr != nil || hashErr != nil || !receipt.matches(expected) || receipt.SHA256 == "" || receipt.SHA256 != digest {
			offset = 0
		}
	}
	reservation, err := d.reserveLocalSpace(info.FsID, filepath.Dir(local), info.Size-offset)
	if err != nil {
		return err
	}
	defer func() { reservation.Release() }()
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
		client, _, authErr := d.authorizedClient(ctx)
		if authErr != nil {
			return authErr
		}
		client = client.WithDownloadTrace(transferID, batchID, info.FsID, info.Path)
		link, linkErr := client.GetDownloadLink(info.FsID)
		if linkErr != nil {
			return linkErr
		}
		body, length, downloadErr := client.DownloadFileWithRange(link, offset)
		if errors.Is(downloadErr, baiduapi.ErrRangeIgnored) {
			trace("range_restart", "previous_offset=%d reason=server_ignored_range", offset)
			reservation.Release()
			reservation, err = d.reserveLocalSpace(info.FsID, filepath.Dir(local), info.Size)
			if err != nil {
				return err
			}
			offset = 0
			body, length, downloadErr = client.DownloadFileWithRange(link, 0)
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
		statistics := d.beginTransferStatistics(info.Path)
		defer func() {
			if statsErr := statistics(0, true); resultErr == nil && statsErr != nil {
				resultErr = statsErr
			}
		}()
		lastLog := time.Now()
		buf := make([]byte, 128*1024)
		for {
			if err = ctx.Err(); err != nil {
				break
			}
			n, readErr := body.Read(buf)
			if statsErr := statistics(int64(n), false); statsErr != nil {
				err = statsErr
				break
			}
			if n > 0 {
				if int64(n) > info.Size-written {
					err = fmt.Errorf("download exceeds expected size")
					break
				}
				var count int
				count, err = writePartialChunk(out, buf[:n])
				writtenThisAttempt += int64(count)
				written += int64(count)
				d.updateProgress(info.FsID, written)
				if time.Since(lastLog) >= 30*time.Second {
					trace("transfer_progress", "completed_bytes=%d expected_bytes=%d elapsed_ms=%d", written, info.Size, time.Since(started).Milliseconds())
					lastLog = time.Now()
				}
				reservation.UpdateRemaining(info.Size - written)
				if err != nil {
					if localStorageFullCheck(err) {
						err = reservation.fullError(info.Size-written, err)
					}
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
		if statsErr := statistics(0, true); err == nil && statsErr != nil {
			err = statsErr
		}
		if err == nil && written != info.Size {
			// A chunked response can end cleanly below the known file size.
			// Preserve short-stream classification so the queue can resume it.
			err = fmt.Errorf("下载长度不符: got %d, want %d: %w", written, info.Size, io.ErrUnexpectedEOF)
		}
		if err == nil {
			err = out.Sync()
		}
		closeErr := out.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			if localStorageFullCheck(err) {
				return reservation.fullError(info.Size-written, err)
			}
			return err
		}
	}
	trace("local_verify_started", "bytes=%d reused=false", info.Size)
	digest, err := hashFile(ctx, partialName)
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
	// Prefer the atomic hard-link path. If the filesystem does not support
	// links, copy to a synced sibling and atomically publish without replacement.
	trace("local_publish_started", "bytes=%d", info.Size)
	if err = publishDownloadedFile(ctx, partialName, local, expected); err != nil {
		return err
	}
	if err = syncPublishedDirectory(filepath.Dir(local)); err != nil {
		return fmt.Errorf("同步已发布文件所在目录失败: %w", err)
	}
	if err = cleanupRegisteredPublishTemp(filepath.Dir(local), metaName, &expected); err != nil {
		return fmt.Errorf("清理已登记的发布临时文件失败: %w", err)
	}
	if err = removePublishedPartial(partialName); err != nil {
		return fmt.Errorf("清理临时文件失败: %w", err)
	}
	d.updateProgress(info.FsID, info.Size)
	trace("local_published", "bytes=%d verified=true", info.Size)
	return recordDownloadManifestResult(ctx, info, relative, "published", expected.SHA256)
}
