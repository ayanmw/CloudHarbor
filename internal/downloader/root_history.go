package downloader

import (
	"encoding/json"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

// RootDestination is historical display evidence, not proof that files exist
// there and never an input for choosing a file operation target.
type RootDestination struct {
	Path          string    `json:"path"`
	AttemptID     string    `json:"attempt_id,omitempty"`
	RecordedAtUTC time.Time `json:"recorded_at_utc"`
}

func copyRootDestination(value *RootDestination) *RootDestination {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (d *Downloader) recordRootDestination(info baiduapi.FileInfo, local string) error {
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	job := q.jobs[info.FsID]
	// Direct transfer helpers may not have a scheduled root job.
	if job == nil {
		return nil
	}
	if job.Destination != nil && job.Destination.AttemptID == job.AttemptID && job.Destination.Path == local {
		return nil
	}
	previous, updated := job.Destination, job.Updated
	job.Destination = &RootDestination{Path: local, AttemptID: job.AttemptID, RecordedAtUTC: time.Now().UTC()}
	evidence, _ := json.Marshal(job.Destination)
	if err := q.persistWithEvent(job, taskstore.Event{Kind: "download_destination_recorded", AttemptID: job.AttemptID, EvidenceType: "local_target_observation", Evidence: evidence}); err != nil {
		job.Destination, job.Updated = previous, updated
		return err
	}
	return nil
}
