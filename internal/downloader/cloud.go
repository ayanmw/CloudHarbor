package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type CloudEntry struct {
	ID           string `json:"id"`
	FsID         int64  `json:"fs_id"`
	Path         string `json:"path"`
	Name         string `json:"name"`
	Directory    bool   `json:"directory"`
	Size         int64  `json:"size"`
	SizeComplete bool   `json:"size_complete,omitempty"`
	ProviderSize int64  `json:"provider_size"`
	Mtime        int64  `json:"server_mtime"`
	MD5          string `json:"md5,omitempty"`
	CanEnqueue   bool   `json:"can_enqueue"`
}
type CloudListing struct {
	DisplaySize          *CloudDisplaySizeInfo    `json:"display_size,omitempty"`
	Order                *baiduapi.DirectoryOrder `json:"order,omitempty"`
	Directory            string                   `json:"directory"`
	QueueDir             string                   `json:"queue_dir"`
	Entries              []CloudEntry             `json:"entries"`
	Total                *int                     `json:"total"`
	Offset               int                      `json:"offset"`
	NextOffset           *int                     `json:"next_offset,omitempty"`
	NextBoundary         *CloudPageBoundary       `json:"next_boundary,omitempty"`
	BoundaryChanged      bool                     `json:"boundary_changed,omitempty"`
	BoundaryChangeReason string                   `json:"boundary_change_reason,omitempty"`
	ObservedAtUTC        string                   `json:"observed_at_utc,omitempty"`
}

type CloudPageBoundary struct {
	FsID          int64  `json:"-"`
	Path          string `json:"-"`
	Name          string `json:"-"`
	Directory     bool   `json:"-"`
	Size          int64  `json:"-"`
	ProviderMtime int64  `json:"-"`
	ContentMD5    string `json:"-"`
	Fingerprint   string `json:"fingerprint"`
}

func (b CloudPageBoundary) ComputeFingerprint() string {
	identity, _ := json.Marshal(struct {
		FsID          int64  `json:"fs_id"`
		Path          string `json:"path"`
		Name          string `json:"name"`
		Directory     bool   `json:"directory"`
		Size          int64  `json:"size"`
		ProviderMtime int64  `json:"provider_mtime"`
		ContentMD5    string `json:"content_md5"`
	}{b.FsID, b.Path, b.Name, b.Directory, b.Size, b.ProviderMtime, b.ContentMD5})
	digest := sha256.Sum256(identity)
	return hex.EncodeToString(digest[:])
}

func (d *Downloader) ListCloud(ctx context.Context, directory string, offset, limit int) (CloudListing, error) {
	return d.ListCloudOrdered(ctx, directory, offset, limit, baiduapi.DirectoryOrder{Field: "name"})
}

// FingerprintForListing binds non-default ordering to its page boundary. The
// default retains the existing fingerprint contract for legacy consumers.
func (b CloudPageBoundary) FingerprintForListing(directory string, order baiduapi.DirectoryOrder) string {
	if order.Field == "name" && !order.Descending {
		return b.ComputeFingerprint()
	}
	data, _ := json.Marshal([]any{directory, order.Field, order.Descending, b.ComputeFingerprint()})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (d *Downloader) ListCloudOrdered(ctx context.Context, directory string, offset, limit int, order baiduapi.DirectoryOrder) (CloudListing, error) {
	if !order.Valid() {
		return CloudListing{}, ErrIntakeInvalid
	}
	if !validCloudPath(directory) || offset < 0 || offset > 1000000 || limit < 1 || limit > 100 {
		return CloudListing{}, ErrIntakeInvalid
	}
	ctx, done, err := d.startIntake(ctx)
	if err != nil {
		return CloudListing{}, err
	}
	defer done()
	d.cloudMutex.RLock()
	defer d.cloudMutex.RUnlock()
	client, epoch, err := d.authorizedCloudClient(ctx)
	if err != nil {
		return CloudListing{}, err
	}
	files, hasMore, boundary, err := client.ListDirPageOrdered(directory, offset, limit, order)
	if err != nil {
		d.reportAuthenticationFailure(err)
		return CloudListing{}, err
	}
	observedAtUTC := time.Now().UTC().Format(time.RFC3339Nano)
	for _, f := range files {
		if !validListingFile(directory, f) {
			return CloudListing{}, ErrIntakeConflict
		}
	}
	if boundary != nil && !validListingFile(directory, *boundary) {
		return CloudListing{}, ErrIntakeConflict
	}
	if _, err = d.intakeClient(ctx, epoch); err != nil {
		return CloudListing{}, err
	}
	queueDir := d.QueueDirectory()
	result := CloudListing{Order: &order, Directory: directory, QueueDir: queueDir, Entries: []CloudEntry{}, Offset: offset, ObservedAtUTC: observedAtUTC}
	if hasMore {
		next := offset + len(files)
		result.NextOffset = &next
		if boundary != nil {
			pageBoundary := CloudPageBoundary{FsID: boundary.FsID, Path: boundary.Path, Name: boundary.ServerFilename, Directory: boundary.IsDir == 1, Size: boundary.Size, ProviderMtime: boundary.Mtime, ContentMD5: boundary.MD5}
			pageBoundary.Fingerprint = pageBoundary.FingerprintForListing(directory, order)
			result.NextBoundary = &pageBoundary
		}
	}
	directorySizes := make(map[int64]taskstore.CloudDirectorySize)
	if d.queue.store != nil {
		for _, f := range files {
			if f.IsDir != 1 {
				continue
			}
			if size, sizeErr := d.queue.store.CloudDirectorySize(client.CloudCacheNamespace(), f.Path); sizeErr == nil {
				directorySizes[f.FsID] = size
			}
		}
	}
	d.queue.mu.Lock()
	defer d.queue.mu.Unlock()
	for _, f := range files {
		target := intakeTarget(queueDir, f.Path)
		allowed := validateIntakePaths([]string{f.Path}, queueDir) == nil && d.queue.jobs[f.FsID] == nil && !d.queue.isRemoteBlocked(f.FsID, f.Path) && !d.queue.blockedIntakes[target] && !d.queue.blockedSources[target]
		entry := CloudEntry{ID: fmt.Sprint(f.FsID), FsID: f.FsID, Path: f.Path, Name: f.ServerFilename, Directory: f.IsDir == 1, Size: f.Size, ProviderSize: f.Size, Mtime: f.Mtime, MD5: f.MD5, CanEnqueue: allowed}
		if size, ok := directorySizes[f.FsID]; ok && size.Complete {
			entry.Size = size.Bytes
			entry.SizeComplete = true
		}
		result.Entries = append(result.Entries, entry)
	}
	return result, nil
}
