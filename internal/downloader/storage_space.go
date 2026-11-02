package downloader

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

const (
	localStorageBlocked = "local_storage_blocked"
	localStorageFull    = "local_storage_full"
)

var localStorageFullCheck = isLocalStorageFull

type localStorageSpaceError struct {
	Kind           string
	Path           string
	RequiredBytes  int64
	AvailableBytes int64
	ReservedBytes  int64
	CheckedAtUTC   time.Time
	Cause          error
}

type LocalStorageEvidence struct {
	Kind           string    `json:"kind"`
	Path           string    `json:"path"`
	RequiredBytes  int64     `json:"required_bytes"`
	AvailableBytes int64     `json:"available_bytes"`
	ReservedBytes  int64     `json:"reserved_bytes"`
	CheckedAtUTC   time.Time `json:"checked_at_utc"`
}

func localStorageDetails(err error) *LocalStorageEvidence {
	var storageErr *localStorageSpaceError
	if !errors.As(err, &storageErr) {
		return nil
	}
	return &LocalStorageEvidence{Kind: storageErr.Kind, Path: storageErr.Path, RequiredBytes: storageErr.RequiredBytes, AvailableBytes: storageErr.AvailableBytes, ReservedBytes: storageErr.ReservedBytes, CheckedAtUTC: storageErr.CheckedAtUTC.UTC()}
}

func (e *localStorageSpaceError) Error() string {
	if e.Kind == localStorageFull {
		return fmt.Sprintf("本地磁盘空间不足，下载已暂停并保留部分文件；释放空间后手动重试: %s", e.Path)
	}
	if e.AvailableBytes < 0 {
		return fmt.Sprintf("无法确认本地可用空间，下载已阻止；检查目标目录后手动重试: %s", e.Path)
	}
	return fmt.Sprintf("本地可用空间不足，下载已阻止；释放空间后手动重试: %s", e.Path)
}

func (e *localStorageSpaceError) Unwrap() error { return e.Cause }

func checkedAvailableBytes(blocks, blockSize uint64) (int64, error) {
	if blockSize != 0 && blocks > math.MaxInt64/blockSize {
		return 0, errors.New("available byte count overflows int64")
	}
	return int64(blocks * blockSize), nil
}

func storageProbeError(path string, err error) error {
	return fmt.Errorf("query available space for %q: %w", path, err)
}

type localSpaceReservation struct {
	d        *Downloader
	fsID     int64
	path     string
	mu       sync.Mutex
	released bool
}

func (d *Downloader) reserveLocalSpace(fsID int64, path string, required int64) (*localSpaceReservation, error) {
	if fsID <= 0 || required < 0 {
		return nil, errors.New("invalid local space reservation")
	}
	d.localSpaceMu.Lock()
	defer d.localSpaceMu.Unlock()
	if _, exists := d.localSpaceReservations[fsID]; exists {
		return nil, fmt.Errorf("local space already reserved for fs_id %d", fsID)
	}
	var reserved int64
	for _, remaining := range d.localSpaceReservations {
		if remaining > math.MaxInt64-reserved {
			return nil, errors.New("local space reservations overflow int64")
		}
		reserved += remaining
	}
	available, err := d.localSpaceProbe(path)
	checkedAt := time.Now().UTC()
	if err != nil {
		return nil, &localStorageSpaceError{Kind: localStorageBlocked, Path: path, RequiredBytes: required, AvailableBytes: -1, ReservedBytes: reserved, CheckedAtUTC: checkedAt, Cause: err}
	}
	if available < 0 {
		return nil, &localStorageSpaceError{Kind: localStorageBlocked, Path: path, RequiredBytes: required, AvailableBytes: -1, ReservedBytes: reserved, CheckedAtUTC: checkedAt, Cause: errors.New("local space probe returned a negative value")}
	}
	if reserved > available || required > available-reserved {
		return nil, &localStorageSpaceError{Kind: localStorageBlocked, Path: path, RequiredBytes: required, AvailableBytes: available, ReservedBytes: reserved, CheckedAtUTC: checkedAt}
	}
	d.localSpaceReservations[fsID] = required
	return &localSpaceReservation{d: d, fsID: fsID, path: path}, nil
}

func (r *localSpaceReservation) fullError(required int64, cause error) error {
	r.d.localSpaceMu.Lock()
	defer r.d.localSpaceMu.Unlock()
	var reserved int64
	for _, remaining := range r.d.localSpaceReservations {
		reserved += remaining
	}
	available := int64(-1)
	if value, err := r.d.localSpaceProbe(r.path); err == nil && value >= 0 {
		available = value
	}
	return &localStorageSpaceError{Kind: localStorageFull, Path: r.path, RequiredBytes: required, AvailableBytes: available, ReservedBytes: reserved, CheckedAtUTC: time.Now().UTC(), Cause: cause}
}

func (r *localSpaceReservation) UpdateRemaining(remaining int64) {
	if r == nil || remaining < 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.released {
		return
	}
	r.d.localSpaceMu.Lock()
	defer r.d.localSpaceMu.Unlock()
	if _, ok := r.d.localSpaceReservations[r.fsID]; ok {
		r.d.localSpaceReservations[r.fsID] = remaining
	}
}

func (r *localSpaceReservation) Release() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.released {
		return
	}
	r.released = true
	r.d.localSpaceMu.Lock()
	delete(r.d.localSpaceReservations, r.fsID)
	r.d.localSpaceMu.Unlock()
}
