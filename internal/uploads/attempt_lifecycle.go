package uploads

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const attemptLifecyclePrefix = "upload_attempt_lifecycle_v1:"

// PreviousAttemptID records history, not permission to reuse or replay requests.
type uploadAttemptEvidence struct {
	AttemptID         string `json:"attempt_id"`
	PreviousAttemptID string `json:"previous_attempt_id,omitempty"`
	Phase             string `json:"phase"`
	ObservedAtUTC     string `json:"observed_at_utc"`
}

func (m *Manager) latestUploadAttempt(item string) (uploadAttemptEvidence, error) {
	var e uploadAttemptEvidence
	if !validID(item) {
		return e, errors.New("invalid upload item")
	}
	var body, recorded string
	err := m.db.QueryRow(`SELECT error,recorded FROM events WHERE item=? AND state='attempt_started' AND substr(error,1,?)=? ORDER BY sequence DESC LIMIT 1`, item, len(attemptLifecyclePrefix), attemptLifecyclePrefix).Scan(&body, &recorded)
	if err != nil {
		return e, err
	}
	if json.Unmarshal([]byte(strings.TrimPrefix(body, attemptLifecyclePrefix)), &e) != nil || !validID(e.AttemptID) || (e.PreviousAttemptID != "" && (!validID(e.PreviousAttemptID) || e.PreviousAttemptID == e.AttemptID)) || e.Phase != "hashing" || e.ObservedAtUTC != recorded {
		return uploadAttemptEvidence{}, errors.New("invalid attempt start evidence")
	}
	if _, err = time.Parse(time.RFC3339Nano, e.ObservedAtUTC); err != nil {
		return uploadAttemptEvidence{}, err
	}
	return e, nil
}

// Claim and durable start commit together. No runner is admitted on a failed commit.
func (m *Manager) claimUploadAttempt(item, attempt string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.claimUploadAttemptLocked(item, attempt)
}

func (m *Manager) claimUploadAttemptLocked(item, attempt string) (bool, error) {
	if !validID(item) || !validID(attempt) || m.ctx.Err() != nil {
		return false, errors.New("invalid upload attempt claim")
	}
	if m.activeAttempts[item].ID != "" {
		return false, errors.New("upload attempt is still active")
	}
	prior, err := m.latestUploadAttempt(item)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if prior.AttemptID == attempt {
		return false, errors.New("upload attempt ID already used")
	}
	// Count returned attempts until their result/cleanup is settled too. A
	// smaller saved limit constrains new claims without interrupting owners.
	if m.admissionError != "" || len(m.activeAttempts) >= m.concurrency {
		return false, nil
	}
	tx, err := m.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	at := now()
	result, err := tx.Exec(`UPDATE items SET state='hashing',updated=? WHERE id=? AND state='queued' AND NOT EXISTS(SELECT 1 FROM items u WHERE u.batch=items.batch AND u.state='unknown') AND NOT EXISTS(SELECT 1 FROM upload_directories d WHERE d.batch=items.batch AND d.state NOT IN ('confirmed','existing'))`, at, item)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return false, err
	}
	body, err := json.Marshal(uploadAttemptEvidence{AttemptID: attempt, PreviousAttemptID: prior.AttemptID, Phase: "hashing", ObservedAtUTC: at})
	if err != nil {
		return false, err
	}
	if _, err = tx.Exec(`INSERT INTO events(item,state,sent,recorded,error) SELECT id,'attempt_started',sent,?,? FROM items WHERE id=?`, at, attemptLifecyclePrefix+string(body), item); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	if m.activeAttempts == nil {
		m.activeAttempts = map[string]uploadAttemptSnapshot{}
	}
	m.activeAttempts[item] = uploadAttemptSnapshot{ID: attempt, State: "hashing", Revision: 1}
	return true, nil
}

// Runner return is local lifecycle evidence. It does not mean remote completion.
func (m *Manager) recordUploadAttemptReturned(item, attempt string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.activeAttempts[item]
	if !validID(item) || !validID(attempt) || current.ID != attempt || current.RunnerReturned {
		return errors.New("upload attempt return is outside active execution")
	}
	at := now()
	body, err := json.Marshal(uploadAttemptEvidence{AttemptID: attempt, Phase: current.State, ObservedAtUTC: at})
	if err != nil {
		return err
	}
	if _, err = m.db.Exec(`INSERT INTO events(item,state,sent,recorded,error) SELECT id,'attempt_runner_returned',sent,?,? FROM items WHERE id=?`, at, attemptLifecyclePrefix+string(body), item); err != nil {
		return err
	}
	current.RunnerReturned = true
	m.activeAttempts[item] = current
	return nil
}

func (m *Manager) uploadAttemptStart(item, attempt string) (uploadAttemptEvidence, error) {
	rows, err := m.db.Query(`SELECT error,recorded FROM events WHERE item=? AND state='attempt_started' AND substr(error,1,?)=? ORDER BY sequence DESC`, item, len(attemptLifecyclePrefix), attemptLifecyclePrefix)
	if err != nil {
		return uploadAttemptEvidence{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var body, recorded string
		if err = rows.Scan(&body, &recorded); err != nil {
			return uploadAttemptEvidence{}, err
		}
		var e uploadAttemptEvidence
		if json.Unmarshal([]byte(strings.TrimPrefix(body, attemptLifecyclePrefix)), &e) != nil {
			return uploadAttemptEvidence{}, errors.New("invalid attempt history")
		}
		if e.AttemptID != attempt {
			continue
		}
		if !validID(e.AttemptID) || e.Phase != "hashing" || e.ObservedAtUTC != recorded || (e.PreviousAttemptID != "" && !validID(e.PreviousAttemptID)) {
			return uploadAttemptEvidence{}, errors.New("invalid attempt history")
		}
		if _, err = time.Parse(time.RFC3339Nano, recorded); err != nil {
			return uploadAttemptEvidence{}, err
		}
		return e, nil
	}
	if err = rows.Err(); err != nil {
		return uploadAttemptEvidence{}, err
	}
	return uploadAttemptEvidence{}, sql.ErrNoRows
}
