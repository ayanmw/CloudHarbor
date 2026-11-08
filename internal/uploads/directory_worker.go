package uploads

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path"
	"strconv"
	"time"
)

type DirectoryReceipt struct {
	Status        string `json:"status"`
	Path          string `json:"path"`
	RemoteID      string `json:"fs_id"`
	OperationID   string `json:"operation_id,omitempty"`
	ObservedAtUTC string `json:"observed_at_utc,omitempty"`
}

type DirectoryRunner func(context.Context, string) (DirectoryReceipt, error)

func (m *Manager) SetDirectoryRunner(run DirectoryRunner) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runDirectory = run
}

func validDirectoryReceipt(r DirectoryReceipt, remote string) bool {
	id, e := strconv.ParseInt(r.RemoteID, 10, 64)
	if e != nil || id <= 0 || strconv.FormatInt(id, 10) != r.RemoteID || r.Path != remote {
		return false
	}
	at, e := time.Parse(time.RFC3339Nano, r.ObservedAtUTC)
	if e != nil {
		return false
	}
	_, offset := at.Zone()
	if offset != 0 {
		return false
	}
	return (r.Status == "confirmed" && validID(r.OperationID)) || (r.Status == "existing" && (r.OperationID == "" || validID(r.OperationID)))
}

func (m *Manager) transitionDirectory(id, expected, state, actual, operation, message string, evidence any) (bool, error) {
	body, err := json.Marshal(evidence)
	if err != nil {
		return false, err
	}
	tx, err := m.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	at := now()
	result, err := tx.Exec(`UPDATE upload_directories SET state=?,updated=?,actual_path=?,operation_id=?,error=? WHERE id=? AND state=?`, state, at, actual, operation, message, id, expected)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	if _, err = tx.Exec(`INSERT INTO upload_directory_events(directory,state,recorded,evidence) VALUES(?,?,?,?)`, id, state, at, string(body)); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// Called by the same worker as file transfers: directory writes never add
// parallel namespace writers or bypass account readiness and batch isolation.
func (m *Manager) processNextDirectory() (worked bool, resultErr error) {
	if m.ctx.Err() != nil || (m.ready != nil && !m.ready()) {
		return false, nil
	}
	m.mu.Lock()
	run := m.runDirectory
	if run == nil || m.directoryExecuting || m.admissionError != "" {
		m.mu.Unlock()
		return false, nil
	}
	var directory, batch, relative, target string
	err := m.db.QueryRow(`SELECT d.id,d.batch,d.relative_path,b.target FROM upload_directories d JOIN batches b ON b.id=d.batch
 WHERE d.state='queued' AND NOT EXISTS(SELECT 1 FROM upload_directories u WHERE u.batch=d.batch AND u.state NOT IN ('queued','confirmed','existing'))
 AND NOT EXISTS(SELECT 1 FROM items u WHERE u.batch=d.batch AND u.state='unknown')
 ORDER BY b.created,b.id,length(d.relative_path)-length(replace(d.relative_path,'/','')),d.relative_path LIMIT 1`).Scan(&directory, &batch, &relative, &target)
	if errors.Is(err, sql.ErrNoRows) {
		m.mu.Unlock()
		return false, nil
	}
	if err != nil {
		m.mu.Unlock()
		return false, err
	}
	remote := path.Join(target, relative)
	claimed, err := m.transitionDirectory(directory, "queued", "sending", "", "", "", map[string]string{"batch_id": batch, "requested_path": remote, "evidence_type": "local_send_intent"})
	if err == nil && claimed {
		m.directoryExecuting = true
	}
	m.mu.Unlock()
	if err != nil || !claimed {
		return false, err
	}
	defer func() {
		m.mu.Lock()
		m.directoryExecuting = false
		if resultErr != nil && m.admissionError == "" {
			m.admissionError = resultErr.Error()
		}
		m.mu.Unlock()
	}()
	receipt, runErr := run(m.ctx, remote)
	state, message, actual := "unknown", "目录操作结果未确认；不会自动重发", ""
	if runErr == nil && validDirectoryReceipt(receipt, remote) {
		state = receipt.Status
		actual = receipt.Path
		message = ""
	} else if runErr != nil && receipt.Status == "failed" {
		state = "failed"
		message = runErr.Error()
	} else if runErr != nil {
		message = runErr.Error()
	}
	operation := receipt.OperationID
	if operation != "" && !validID(operation) {
		operation = ""
	}
	changed, err := m.transitionDirectory(directory, "sending", state, actual, operation, message, map[string]any{"receipt": receipt, "requested_path": remote, "error": message})
	if err == nil && !changed {
		err = errors.New("目录执行状态已变化，回执未保存")
	}
	return true, err
}
