package downloader

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

var ErrCloudDisplaySizeSnapshotChanged = errors.New("display size snapshot unavailable or changed")

const displaySizeSnapshotLifetime = 15 * time.Minute
const displaySizeSnapshotLimit = 8

// These are observation times, not provider-issued transaction timestamps.
// ReadAtUTC is this response; RemoteObservedAtUTC remains the original read.
type CloudDisplaySizeInfo struct {
	Mixed                  bool      `json:"mixed"`
	SnapshotID             string    `json:"snapshot_id"`
	CacheGeneration        int64     `json:"cache_generation,string"`
	UnknownDirectories     int       `json:"unknown_directories"`
	RemoteReadStartedAtUTC time.Time `json:"remote_read_started_at_utc"`
	RemoteObservedAtUTC    time.Time `json:"remote_observed_at_utc"`
	CacheReadAtUTC         time.Time `json:"cache_read_at_utc"`
	CreatedAtUTC           time.Time `json:"created_at_utc"`
	ExpiresAtUTC           time.Time `json:"expires_at_utc"`
	ReadAtUTC              time.Time `json:"read_at_utc"`
}
type cloudDisplaySizeSnapshot struct {
	owner, directory string
	order            baiduapi.DirectoryOrder
	mixed            bool
	info             CloudDisplaySizeInfo
	entries          []CloudEntry
}

func validDisplaySizeSnapshotID(id string) bool {
	raw, err := hex.DecodeString(id)
	return err == nil && len(raw) == 16 && hex.EncodeToString(raw) == id
}
func (d *Downloader) rememberDisplaySizeSnapshot(s *cloudDisplaySizeSnapshot) {
	d.displaySizeMu.Lock()
	defer d.displaySizeMu.Unlock()
	if d.displaySizeSnapshots == nil {
		d.displaySizeSnapshots = make(map[string]*cloudDisplaySizeSnapshot)
	}
	for id, old := range d.displaySizeSnapshots {
		if !s.info.CreatedAtUTC.Before(old.info.ExpiresAtUTC) {
			delete(d.displaySizeSnapshots, id)
		}
	}
	if len(d.displaySizeSnapshots) >= displaySizeSnapshotLimit {
		var oldest string
		for id, old := range d.displaySizeSnapshots {
			if oldest == "" || old.info.CreatedAtUTC.Before(d.displaySizeSnapshots[oldest].info.CreatedAtUTC) {
				oldest = id
			}
		}
		delete(d.displaySizeSnapshots, oldest)
	}
	d.displaySizeSnapshots[s.info.SnapshotID] = s
}

// ListCloudDisplaySize reads the complete direct-child listing once, then pages
// an immutable display-size order. Tokens are ephemeral derived views, never
// persisted task evidence. Remote writes still require their own online checks.
func (d *Downloader) ListCloudDisplaySize(ctx context.Context, directory string, offset, limit int, descending bool, token string) (CloudListing, error) {
	return d.ListCloudDisplayOrdered(ctx, directory, offset, limit, baiduapi.DirectoryOrder{Field: "size", Descending: descending}, true, token)
}

// All interactive grouping is local to a complete direct-child snapshot, never a page.
func (d *Downloader) ListCloudDisplayOrdered(ctx context.Context, directory string, offset, limit int, order baiduapi.DirectoryOrder, mixed bool, token string) (CloudListing, error) {
	if !order.Valid() {
		return CloudListing{}, ErrIntakeInvalid
	}
	if !validCloudPath(directory) || offset < 0 || offset > 1000000 || limit < 1 || limit > 100 || (token != "" && !validDisplaySizeSnapshotID(token)) {
		return CloudListing{}, ErrIntakeInvalid
	}
	if offset > 0 && token == "" {
		return CloudListing{}, ErrCloudDisplaySizeSnapshotChanged
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
	if d.queue.store == nil {
		return CloudListing{}, ErrIntakeInvalid
	}
	owner := client.CloudCacheNamespace() + ":" + client.ApplicationFingerprint()
	var view *cloudDisplaySizeSnapshot
	if token != "" {
		d.displaySizeMu.Lock()
		view = d.displaySizeSnapshots[token]
		d.displaySizeMu.Unlock()
		if view == nil || view.owner != owner || view.directory != directory || view.order != order || view.mixed != mixed || !time.Now().UTC().Before(view.info.ExpiresAtUTC) {
			return CloudListing{}, ErrCloudDisplaySizeSnapshotChanged
		}
		cache, readErr := d.queue.store.CloudDirectorySizesSnapshot(ctx, client.CloudCacheNamespace(), nil)
		if readErr != nil {
			return CloudListing{}, readErr
		}
		if cache.Generation != view.info.CacheGeneration {
			return CloudListing{}, ErrCloudDisplaySizeSnapshotChanged
		}
	} else {
		started := time.Now().UTC()
		files, readErr := client.ListDir(directory)
		if readErr != nil {
			d.reportAuthenticationFailure(readErr)
			return CloudListing{}, readErr
		}
		observed := time.Now().UTC()
		identities := make([]taskstore.CloudDirectoryIdentity, 0)
		seen := make(map[string]bool, len(files))
		for _, f := range files {
			if !validListingFile(directory, f) || seen[f.Path] {
				return CloudListing{}, ErrIntakeConflict
			}
			seen[f.Path] = true
			if f.IsDir == 1 {
				identities = append(identities, taskstore.CloudDirectoryIdentity{FsID: f.FsID, Path: f.Path})
			}
		}
		cache, readErr := d.queue.store.CloudDirectorySizesSnapshot(ctx, client.CloudCacheNamespace(), identities)
		if readErr != nil {
			return CloudListing{}, readErr
		}
		entries := make([]CloudEntry, 0, len(files))
		unknown := 0
		for _, f := range files {
			entry := CloudEntry{ID: fmt.Sprint(f.FsID), FsID: f.FsID, Path: f.Path, Name: f.ServerFilename, Directory: f.IsDir == 1, Size: f.Size, ProviderSize: f.Size, Mtime: f.Mtime, MD5: f.MD5}
			if entry.Directory {
				size := cache.Sizes[f.FsID]
				if size.Complete {
					entry.Size = size.Bytes
					entry.SizeComplete = true
				} else {
					unknown++
				}
			}
			entries = append(entries, entry)
		}
		sortCloudEntries(entries, order, mixed)
		var id [16]byte
		if _, err = rand.Read(id[:]); err != nil {
			return CloudListing{}, err
		}
		now := time.Now().UTC()
		view = &cloudDisplaySizeSnapshot{owner: owner, directory: directory, order: order, mixed: mixed, entries: entries, info: CloudDisplaySizeInfo{Mixed: mixed, SnapshotID: hex.EncodeToString(id[:]), CacheGeneration: cache.Generation, UnknownDirectories: unknown, RemoteReadStartedAtUTC: started, RemoteObservedAtUTC: observed, CacheReadAtUTC: cache.ReadAtUTC, CreatedAtUTC: now, ExpiresAtUTC: now.Add(displaySizeSnapshotLifetime)}}
	}
	if err = ctx.Err(); err != nil {
		return CloudListing{}, err
	}
	if _, err = d.intakeClient(ctx, epoch); err != nil {
		return CloudListing{}, err
	}
	if token == "" {
		d.rememberDisplaySizeSnapshot(view)
	}
	return d.displaySizePage(view, offset, limit), nil
}

func (d *Downloader) displaySizePage(view *cloudDisplaySizeSnapshot, offset, limit int) CloudListing {
	order := view.order
	total := len(view.entries)
	info := view.info
	info.ReadAtUTC = time.Now().UTC()
	queueDir := d.QueueDirectory()
	result := CloudListing{DisplaySize: &info, Order: &order, Directory: view.directory, QueueDir: queueDir, Entries: []CloudEntry{}, Total: &total, Offset: offset, ObservedAtUTC: info.RemoteObservedAtUTC.Format(time.RFC3339Nano)}
	if offset >= total {
		return result
	}
	end := offset + limit
	if end > total {
		end = total
	}
	result.Entries = append(result.Entries, view.entries[offset:end]...)
	d.queue.mu.Lock()
	for index := range result.Entries {
		entry := &result.Entries[index]
		target := intakeTarget(queueDir, entry.Path)
		entry.CanEnqueue = validateIntakePaths([]string{entry.Path}, queueDir) == nil && d.queue.jobs[entry.FsID] == nil && !d.queue.isRemoteBlocked(entry.FsID, entry.Path) && !d.queue.blockedIntakes[target] && !d.queue.blockedSources[target]
	}
	d.queue.mu.Unlock()
	if end < total {
		next := end
		result.NextOffset = &next
		entry := view.entries[end]
		boundary := CloudPageBoundary{FsID: entry.FsID, Path: entry.Path, Name: entry.Name, Directory: entry.Directory, Size: entry.ProviderSize, ProviderMtime: entry.Mtime, ContentMD5: entry.MD5}
		boundary.Fingerprint = boundary.FingerprintForListing(view.directory, order)
		result.NextBoundary = &boundary
	}
	return result
}
