package uploads

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

const resumeBridgePrefix = "upload_resume_bridge_v1:"

type uploadResumeBridge struct {
	AttemptID string                         `json:"attempt_id"`
	Admission baiduapi.UploadResumeAdmission `json:"admission"`
}

// The old journal remains immutable. This records a new local authorization
// observation; it never rewrites old credential fingerprints or timestamps.
func (m *Manager) resumeAdmissionRecorder(item, attempt string) func(baiduapi.UploadResumeAdmission) error {
	recorded := false
	return func(a baiduapi.UploadResumeAdmission) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if !validID(item) || !validID(attempt) || m.ctx.Err() != nil || m.activeAttempts[item].ID != attempt || m.activeAttempts[item].RunnerReturned || recorded {
			return errors.New("resume admission outside active attempt")
		}
		if err := m.checkMutationAdmissionLocked(item); err != nil {
			return err
		}
		current, err := m.latestUploadAttempt(item)
		if err != nil {
			return err
		}
		if current.AttemptID != attempt || a.SourceAttemptID == attempt {
			return errors.New("resume source does not match attempt history")
		}
		sourceID, source, err := m.uploadResumeSource(item, current.PreviousAttemptID)
		if err != nil {
			return err
		}
		if source == nil || sourceID != a.SourceAttemptID {
			return errors.New("resume source does not match attempt history")
		}
		journal := *source
		if !journal.CreateTracked || journal.CreateAdmission != nil || len(journal.PendingParts) != 0 || a.SourceSequence != journal.LastSequence || a.SourcePlanDigest != journal.PlanDigest || !slices.Equal(a.RemainingParts, journal.NeverAdmittedParts) {
			return errors.New("resume source has unresolved or changed evidence")
		}
		if err = baiduapi.ValidateUploadSessionScope(journal.Plan, a.CurrentScope); err != nil {
			return err
		}
		if a.ObservedAtUTC.IsZero() || a.ObservedAtUTC.Year() < 1 || a.ObservedAtUTC.Year() > 9999 {
			return errors.New("invalid resume observation time")
		}
		a.ObservedAtUTC = a.ObservedAtUTC.UTC()
		a.CurrentScope.CheckedAtUTC = a.CurrentScope.CheckedAtUTC.UTC()
		a.RemainingParts = append([]int{}, a.RemainingParts...)
		body, err := json.Marshal(uploadResumeBridge{attempt, a})
		if err != nil {
			return err
		}
		if _, err = m.db.Exec(`INSERT INTO events(item,state,sent,recorded,error) SELECT id,'session_resume_admitted',sent,?,? FROM items WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), resumeBridgePrefix+string(body), item); err != nil {
			return m.evidencePersistenceFailureLocked(err)
		}
		recorded = true
		return nil
	}
}

func (m *Manager) readResumeBridge(item, attempt string) (uploadResumeBridge, int64, error) {
	var result uploadResumeBridge
	rows, err := m.db.Query(`SELECT sequence,error,recorded,state FROM events WHERE item=? AND substr(error,1,?)=? ORDER BY sequence`, item, len(resumeBridgePrefix), resumeBridgePrefix)
	if err != nil {
		return result, 0, err
	}
	defer rows.Close()
	var found int64
	for rows.Next() {
		var sequence int64
		var body, recorded, state string
		if err = rows.Scan(&sequence, &body, &recorded, &state); err != nil {
			return uploadResumeBridge{}, 0, err
		}
		var bridge uploadResumeBridge
		if json.Unmarshal([]byte(strings.TrimPrefix(body, resumeBridgePrefix)), &bridge) != nil || !validID(bridge.AttemptID) {
			return uploadResumeBridge{}, 0, errors.New("invalid resume bridge")
		}
		if bridge.AttemptID != attempt {
			continue
		}
		a := bridge.Admission
		if found != 0 || state != "session_resume_admitted" || !validID(a.SourceAttemptID) || a.SourceAttemptID == attempt || a.SourceSequence <= 0 || a.SourceSequence >= sequence || a.ObservedAtUTC.IsZero() || !a.CurrentScope.Valid() {
			return uploadResumeBridge{}, 0, errors.New("invalid resume bridge sequence or scope")
		}
		if _, err = time.Parse(time.RFC3339Nano, recorded); err != nil {
			return uploadResumeBridge{}, 0, err
		}
		found = sequence
		result = bridge
	}
	if err = rows.Err(); err != nil {
		return uploadResumeBridge{}, 0, err
	}
	if found == 0 {
		return result, 0, sql.ErrNoRows
	}
	return result, found, nil
}
