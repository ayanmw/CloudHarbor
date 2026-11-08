package uploads

import (
	"database/sql"
	"errors"
	"time"
)

type UploadConcurrencyStatus struct {
	AdmissionError string `json:"admission_error"`
	Limit          int    `json:"limit"`
	Maximum        int    `json:"maximum"`
	Admitted       int    `json:"admitted_files"`
	Finishing      int    `json:"finishing_files"`
	UpdatedAtUTC   string `json:"updated_at_utc"`
	ObservedAtUTC  string `json:"observed_at_utc"`
}

func (m *Manager) initConcurrency() error {
	_, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS upload_settings(name TEXT PRIMARY KEY,value INTEGER NOT NULL,updated TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS upload_setting_events(sequence INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT NOT NULL,previous_value INTEGER NOT NULL,value INTEGER NOT NULL,previous_updated TEXT NOT NULL,recorded TEXT NOT NULL);`)
	if err != nil {
		return err
	}
	m.concurrency = 1
	err = m.db.QueryRow(`SELECT value,updated FROM upload_settings WHERE name='file_concurrency'`).Scan(&m.concurrency, &m.concurrencyUpdated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if m.concurrency < 1 || m.concurrency > 2 {
		return errors.New("持久化上传并发配置无效")
	}
	if _, err = time.Parse(time.RFC3339Nano, m.concurrencyUpdated); err != nil {
		return errors.New("上传并发配置时间无效")
	}
	return nil
}

// Publish the in-memory setting only after its SQLite value and history commit.
// Lowering the limit never cancels a previously admitted attempt.
func (m *Manager) SetConcurrency(limit int) error {
	if limit < 1 || limit > 2 {
		return errors.New("上传并发只能设置为1或2")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ctx.Err(); err != nil {
		return err
	}
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := now()
	if _, err = tx.Exec(`INSERT INTO upload_settings(name,value,updated) VALUES('file_concurrency',?,?) ON CONFLICT(name) DO UPDATE SET value=excluded.value,updated=excluded.updated`, limit, at); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO upload_setting_events(name,previous_value,value,previous_updated,recorded) VALUES('file_concurrency',?,?,?,?)`, m.concurrency, limit, m.concurrencyUpdated, at); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	m.concurrency = limit
	m.concurrencyUpdated = at
	return nil
}

// Admission ownership is process-local. Historical SQLite states cannot prove
// a runner is still alive; returned runners retain a slot until final cleanup.
func (m *Manager) ConcurrencyStatus() UploadConcurrencyStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := UploadConcurrencyStatus{Limit: m.concurrency, Maximum: 2, AdmissionError: m.admissionError, UpdatedAtUTC: m.concurrencyUpdated, ObservedAtUTC: now()}
	for _, attempt := range m.activeAttempts {
		if attempt.RunnerReturned {
			status.Finishing++
		} else {
			status.Admitted++
		}
	}
	return status
}
