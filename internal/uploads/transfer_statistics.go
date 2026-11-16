package uploads

import (
	"encoding/json"
	"errors"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

type uploadTransferEvidence struct {
	AttemptID string                        `json:"attempt_id"`
	Sample    baiduapi.UploadTransferSample `json:"sample"`
}

func validTransferSample(sample baiduapi.UploadTransferSample) bool {
	return validTransferPlan(sample) && sample.PartIndex >= 0 && sample.PartIndex < 10000 && sample.PayloadBytesRead >= 0 && sample.PayloadBytesRead <= baiduapi.UploadBlockSize && sample.ActiveNanos >= 0 && !sample.StartedAtUTC.IsZero() && !sample.FinishedAtUTC.IsZero()
}

func (m *Manager) recordTransferSample(item, attempt string, sample baiduapi.UploadTransferSample) error {
	if !validID(item) || !validID(attempt) || !validTransferSample(sample) {
		return errors.New("invalid upload transfer observation")
	}
	sample.StartedAtUTC = sample.StartedAtUTC.UTC()
	sample.FinishedAtUTC = sample.FinishedAtUTC.UTC()
	body, err := json.Marshal(uploadTransferEvidence{AttemptID: attempt, Sample: sample})
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if owner := m.activeAttempts[item]; owner.ID != attempt || owner.RunnerReturned {
		return errors.New("upload transfer observation is outside its active attempt")
	}
	// Do not update item.sent: request reads are distinct from acknowledged parts.
	_, err = m.db.Exec(`INSERT INTO events(item,state,sent,recorded,error) VALUES(?,'uploading',?,?,?)`, item, sample.PayloadBytesRead, now(), transferSamplePrefix+string(body))
	return m.evidencePersistenceFailureLocked(err)
}
