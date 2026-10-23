package downloader

import (
	"context"
	"fmt"
)

type CloudEntry struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	Name       string `json:"name"`
	Directory  bool   `json:"directory"`
	Size       int64  `json:"size"`
	CanEnqueue bool   `json:"can_enqueue"`
}
type CloudListing struct {
	Directory  string       `json:"directory"`
	QueueDir   string       `json:"queue_dir"`
	Entries    []CloudEntry `json:"entries"`
	Total      *int         `json:"total"`
	Offset     int          `json:"offset"`
	NextOffset *int         `json:"next_offset,omitempty"`
}

func (d *Downloader) ListCloud(ctx context.Context, directory string, offset, limit int) (CloudListing, error) {
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
	client, epoch, err := d.authorizedClient(ctx)
	if err != nil {
		return CloudListing{}, err
	}
	files, hasMore, err := client.ListDirPage(directory, offset, limit)
	if err != nil {
		d.reportAuthenticationFailure(err)
		return CloudListing{}, err
	}
	for _, f := range files {
		if !validListingFile(directory, f) {
			return CloudListing{}, ErrIntakeConflict
		}
	}
	if _, err = d.intakeClient(ctx, epoch); err != nil {
		return CloudListing{}, err
	}
	result := CloudListing{Directory: directory, QueueDir: d.cfg.QueueDir, Entries: []CloudEntry{}, Offset: offset}
	if hasMore {
		next := offset + len(files)
		result.NextOffset = &next
	}
	d.queue.mu.Lock()
	defer d.queue.mu.Unlock()
	for _, f := range files {
		target := intakeTarget(d.cfg.QueueDir, f.Path)
		allowed := validateIntakePaths([]string{f.Path}, d.cfg.QueueDir) == nil && d.queue.jobs[f.FsID] == nil && !d.queue.blockedIntakes[target] && !d.queue.blockedSources[target]
		result.Entries = append(result.Entries, CloudEntry{ID: fmt.Sprint(f.FsID), Path: f.Path, Name: f.ServerFilename, Directory: f.IsDir == 1, Size: f.Size, CanEnqueue: allowed})
	}
	return result, nil
}
