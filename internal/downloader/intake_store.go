package downloader

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

type IntakeItem struct {
	Source        baiduapi.FileInfo `json:"source"`
	SourceID      string            `json:"source_id"`
	Target        string            `json:"target"`
	State         string            `json:"state"`
	Error         string            `json:"error,omitempty"`
	AsyncTaskID   string            `json:"async_task_id,omitempty"`
	SyncConfirmed bool              `json:"sync_confirmed,omitempty"`
}

type intakeJournal interface {
	SaveIntake(string, []byte) error
	LoadIntakes() (map[string]json.RawMessage, error)
}
type Intake struct {
	ID       string       `json:"id"`
	State    string       `json:"state"`
	QueueDir string       `json:"queue_dir"`
	Mode     DownloadMode `json:"download_mode"`
	Created  time.Time    `json:"created"`
	Expires  time.Time    `json:"expires"`
	Items    []IntakeItem `json:"items"`
}
type intakeRecord struct {
	Intake
	Version int    `json:"version"`
	Epoch   uint64 `json:"epoch"`
	Boot    string `json:"boot"`
}

func newIntakeID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}
func validIntakeID(id string) bool {
	if len(id) != 32 {
		return false
	}
	decoded, err := hex.DecodeString(id)
	return err == nil && hex.EncodeToString(decoded) == id
}
func cloneIntake(r intakeRecord) intakeRecord {
	r.Items = append([]IntakeItem(nil), r.Items...)
	return r
}

func intakePreviewValid(r intakeRecord, boot string, epoch uint64, now time.Time) bool {
	return r.State == "preview" && r.Boot == boot && r.Epoch == epoch && now.Before(r.Expires)
}

func (d *Downloader) GetIntakes() []Intake {
	d.clientMutex.RLock()
	epoch := d.authEpoch
	d.clientMutex.RUnlock()
	now := time.Now()

	d.intakeMutex.RLock()
	defer d.intakeMutex.RUnlock()
	result := make([]Intake, 0, len(d.intakes))
	for _, r := range d.intakes {
		copy := cloneIntake(r)
		if copy.State == "preview" && !intakePreviewValid(copy, d.intakeBoot, epoch, now) {
			copy.State = "expired"
		}
		result = append(result, copy.Intake)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Created.Equal(result[j].Created) {
			return result[i].ID > result[j].ID
		}
		return result[i].Created.After(result[j].Created)
	})
	return result
}
func (d *Downloader) refreshIntakeBlocks() {
	blocked := make(map[string]bool)
	d.intakeMutex.RLock()
	for _, r := range d.intakes {
		for _, item := range r.Items {
			if item.State == "sending" || item.State == "unknown" {
				blocked[item.Target] = true
			}
		}
	}
	d.intakeMutex.RUnlock()
	d.queue.mu.Lock()
	q := d.queue
	changed := len(q.blockedIntakes) != len(blocked)
	if !changed {
		for target := range q.blockedIntakes {
			if !blocked[target] {
				changed = true
				break
			}
		}
	}
	q.blockedIntakes = blocked
	if changed {
		// Candidate entries cache intake-block eligibility; refresh them when a
		// durable intake moves into or out of quarantine.
		q.clearCandidates()
		for id, info := range q.eligible {
			if job := q.jobs[id]; job != nil && info != nil && job.Info == *info {
				q.enqueueCandidate(job)
			}
		}
		q.signal()
	}
	d.queue.mu.Unlock()
}
func (d *Downloader) saveIntake(record intakeRecord) error {
	body, err := json.Marshal(record)
	if err == nil {
		err = d.intakeJournal.SaveIntake(record.ID, body)
	}
	if err != nil {
		d.queue.mu.Lock()
		d.queue.err = fmt.Errorf("intake journal could not be saved: %w", err)
		d.queue.mu.Unlock()
		d.cancel()
		return err
	}
	d.intakeMutex.Lock()
	d.intakes[record.ID] = cloneIntake(record)
	d.intakeMutex.Unlock()
	d.refreshIntakeBlocks()
	return nil
}
func (d *Downloader) loadIntakes() error {
	boot, err := newIntakeID()
	if err != nil {
		return err
	}
	d.intakeBoot = boot
	records, err := d.intakeJournal.LoadIntakes()
	if err != nil {
		return err
	}
	for id, body := range records {
		var r intakeRecord
		if json.Unmarshal(body, &r) != nil || r.ID != id || !validIntakeID(r.ID) || r.Version != 1 || !validCloudPath(r.QueueDir) || r.QueueDir == "/" || len(r.Items) < 1 || len(r.Items) > 20 || (r.Mode != DownloadModeKeep && r.Mode != DownloadModeDelete) || (r.State != "preview" && r.State != "running" && r.State != "finished") {
			return fmt.Errorf("invalid intake record")
		}
		paths := make([]string, len(r.Items))
		for i, item := range r.Items {
			paths[i] = item.Source.Path
		}
		if validateIntakePaths(paths, r.QueueDir) != nil {
			return fmt.Errorf("invalid intake paths")
		}
		changed := r.State == "running"
		for i := range r.Items {
			item := &r.Items[i]
			if item.Source.FsID <= 0 || item.Source.Size < 0 || (item.Source.IsDir != 0 && item.Source.IsDir != 1) || item.Source.ServerFilename != path.Base(item.Source.Path) || item.SourceID != fmt.Sprint(item.Source.FsID) || item.Target != intakeTarget(r.QueueDir, item.Source.Path) {
				return fmt.Errorf("invalid intake item identity")
			}
			switch item.State {
			case "ready", "moved", "failed", "not_sent", "unknown", "sending":
			default:
				return fmt.Errorf("invalid intake item state")
			}
			if item.State == "sending" {
				item.State = "unknown"
				item.Error = "restart_during_move"
				changed = true
			}
			if r.State == "running" && item.State == "ready" {
				item.State = "not_sent"
				item.Error = "restart_before_send"
			}
		}
		if changed {
			r.State = "finished"
			if err = d.saveIntake(r); err != nil {
				return err
			}
		} else {
			d.intakes[id] = r
		}
	}
	d.refreshIntakeBlocks()
	return nil
}
