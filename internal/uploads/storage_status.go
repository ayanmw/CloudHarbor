package uploads

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type StorageSnapshot struct {
	Directory             string  `json:"directory"`
	StagingBytes          string  `json:"staging_bytes"`
	StagingFiles          string  `json:"staging_files"`
	Partial               bool    `json:"partial"`
	DisappearedFiles      string  `json:"disappeared_files"`
	ScanStartedAtUTC      string  `json:"scan_started_at_utc"`
	ScanFinishedAtUTC     string  `json:"scan_finished_at_utc"`
	AvailableBytes        *string `json:"available_bytes"`
	AvailableError        string  `json:"available_error"`
	AvailableCheckedAtUTC string  `json:"available_checked_at_utc"`
}

// This is a non-atomic metadata observation, not a reservation or allocated-block total.
func readStorageSnapshot(ctx context.Context, dir string, probe func(string) (int64, error)) (StorageSnapshot, error) {
	result := StorageSnapshot{Directory: dir, ScanStartedAtUTC: now()}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	handle, err := os.Open(dir)
	if err != nil {
		return result, err
	}
	defer handle.Close()
	var bytes, files, disappeared int64
	for {
		if err = ctx.Err(); err != nil {
			return result, err
		}
		entries, readErr := handle.ReadDir(128)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return result, readErr
		}
		for _, entry := range entries {
			if err = ctx.Err(); err != nil {
				return result, err
			}
			name := entry.Name()
			suffix := filepath.Ext(name)
			if (suffix != ".data" && suffix != ".part") || !validID(strings.TrimSuffix(name, suffix)) {
				continue
			}
			info, infoErr := entry.Info()
			if errors.Is(infoErr, os.ErrNotExist) {
				disappeared++
				continue
			}
			if infoErr != nil {
				return result, infoErr
			}
			if !info.Mode().IsRegular() {
				continue
			}
			if info.Size() < 0 || info.Size() > math.MaxInt64-bytes || files == math.MaxInt64 {
				return result, errors.New("staging size overflows int64")
			}
			bytes += info.Size()
			files++
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	result.StagingBytes = strconv.FormatInt(bytes, 10)
	result.StagingFiles = strconv.FormatInt(files, 10)
	result.DisappearedFiles = strconv.FormatInt(disappeared, 10)
	result.Partial = disappeared > 0
	result.ScanFinishedAtUTC = now()
	if err = ctx.Err(); err != nil {
		return result, err
	}
	available, probeErr := probe(dir)
	result.AvailableCheckedAtUTC = now()
	if probeErr != nil || available < 0 {
		result.AvailableError = "当前无法读取暂存目录所在卷的可用空间，请检查磁盘或挂载状态。"
	} else {
		value := strconv.FormatInt(available, 10)
		result.AvailableBytes = &value
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}
func (m *Manager) HandleStorage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if m.ctx.Err() != nil {
		http.Error(w, "上传管理器已关闭", http.StatusServiceUnavailable)
		return
	}
	value, err := readStorageSnapshot(r.Context(), m.dir, uploadAvailableBytes)
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "暂存文件统计暂不可读，请稍后重新核对", http.StatusServiceUnavailable)
		return
	}
	write(w, value)
}
