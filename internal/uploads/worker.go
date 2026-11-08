package uploads

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

type uploadWork struct{ item, batch, relative, target, attempt string }

var errUploadBatchBlocked = errors.New("同批目录或文件尚待核对，或执行器已停止准入；此文件已暂停，暂存保留")

func (m *Manager) batchBlockedLocked(batch string) (bool, error) {
	var blocked bool
	err := m.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM items WHERE batch=? AND state='unknown') OR EXISTS(SELECT 1 FROM upload_directories WHERE batch=? AND state NOT IN ('confirmed','existing'))`, batch, batch).Scan(&blocked)
	return blocked, err
}

func (m *Manager) claimNextUpload() (*uploadWork, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.admissionError != "" || m.ctx.Err() != nil || len(m.activeAttempts) >= m.concurrency {
		return nil, nil
	}
	w := &uploadWork{}
	err := m.db.QueryRow(`SELECT i.id,i.batch,i.relative_path,b.target FROM items i JOIN batches b ON b.id=i.batch WHERE i.state='queued' AND NOT EXISTS(SELECT 1 FROM items u WHERE u.batch=i.batch AND u.state='unknown') AND NOT EXISTS(SELECT 1 FROM upload_directories d WHERE d.batch=i.batch AND d.state NOT IN ('confirmed','existing')) ORDER BY b.created,i.id LIMIT 1`).Scan(&w.item, &w.batch, &w.relative, &w.target)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if w.attempt, err = id(); err != nil {
		return nil, err
	}
	claimed, err := m.claimUploadAttemptLocked(w.item, w.attempt)
	if err != nil || !claimed {
		return nil, err
	}
	return w, nil
}

func (m *Manager) worker() {
	defer m.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
		}
		m.mu.Lock()
		stopped := m.admissionError != ""
		m.mu.Unlock()
		if stopped {
			return
		}
		if m.ready != nil && !m.ready() {
			continue
		}
		worked, err := m.processNextDirectory()
		if err == nil && !worked {
			var w *uploadWork
			w, err = m.claimNextUpload()
			if err == nil && w != nil {
				err = m.executeUpload(w)
			}
		}
		if err != nil {
			// Stop new admissions without aborting an already-sent peer request.
			m.mu.Lock()
			if m.admissionError == "" {
				m.admissionError = err.Error()
			}
			m.mu.Unlock()
			return
		}
	}
}

func (m *Manager) executeUpload(w *uploadWork) (resultErr error) {
	item, attempt := w.item, w.attempt
	// Retain slot ownership through the final state transaction. A peer cannot
	// claim a new file in the gap between runner return and unknown persistence.
	defer func() {
		if resultErr != nil {
			m.mu.Lock()
			if m.admissionError == "" {
				m.admissionError = resultErr.Error()
			}
			m.mu.Unlock()
		}
		m.finishUploadAttempt(item, attempt)
	}()
	lastState := "hashing"
	var sent int64
	reported := false
	attemptCtx, err := m.uploadAttemptContext(item, attempt)
	if err != nil {
		if e := m.recordUploadAttemptReturned(item, attempt); e != nil {
			return e
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		var retained int64
		if e := m.db.QueryRow(`SELECT sent FROM items WHERE id=?`, item).Scan(&retained); e != nil {
			return e
		}
		return m.update(item, "unknown", retained, err.Error())
	}
	receipt, runErr := m.run(attemptCtx, filepath.Join(m.dir, item+".data"), path.Join(w.target, w.relative), func(state string, bytes int64) error {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.isPauseRequested(item) {
			sent = bytes
			return errUploadPausedAtCheckpoint
		}
		if m.admissionError != "" {
			sent = bytes
			return errUploadBatchBlocked
		}
		if state == "submitting" || state == "finalizing" {
			blocked, e := m.batchBlockedLocked(w.batch)
			if e != nil {
				return e
			}
			if blocked {
				sent = bytes
				return errUploadBatchBlocked
			}
		}
		if reported && state == lastState && bytes == sent {
			return nil
		}
		if e := m.update(item, state, bytes, ""); e != nil {
			return e
		}
		m.setUploadAttemptPhaseLocked(item, attempt, state)
		reported = true
		lastState = state
		sent = bytes
		return nil
	}, func(sample baiduapi.UploadTransferSample) error { return m.recordTransferSample(item, attempt, sample) })
	if err = m.recordUploadAttemptReturned(item, attempt); err != nil {
		return err
	}
	if err = m.persistUploadResult(item, receipt, runErr, lastState, sent); err != nil {
		return err
	}
	if runErr == nil {
		os.Remove(filepath.Join(m.dir, item+".data"))
	}
	return nil
}

// Only the short result transaction holds mu; filesystem cleanup retains the
// attempt slot but must not block a peer's progress or management requests.
func (m *Manager) persistUploadResult(item string, receipt baiduapi.UploadReceipt, runErr error, lastState string, sent int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if runErr != nil {
		state := "failed"
		if lastState == "submitting" || lastState == "finalizing" || errors.Is(runErr, baiduapi.ErrUploadUnconfirmed) {
			state = "unknown"
		}
		if m.ctx.Err() != nil && lastState == "hashing" {
			state = "queued"
		}
		if errors.Is(runErr, errUploadPausedAtCheckpoint) || errors.Is(runErr, errUploadBatchBlocked) {
			state = "paused"
		}
		if err := m.update(item, state, sent, runErr.Error()); err != nil {
			return err
		}
		if state != "paused" {
			m.setPauseRequested(item, false)
		}
		return nil
	}
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := now()
	if _, err = tx.Exec(`UPDATE items SET state='confirmed',sent=size,updated=?,error='',actual_path=? WHERE id=?`, at, receipt.Path, item); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO events(item,state,sent,recorded,error) SELECT id,'confirmed',size,?,? FROM items WHERE id=?`, at, fmt.Sprintf("fs_id=%s", receipt.FsID), item); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	m.setPauseRequested(item, false)
	return nil
}
