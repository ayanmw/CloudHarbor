package uploads

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
)

var errUploadPausedAtCheckpoint = errors.New("上传已在安全检查点暂停，继续前会核对暂存与会话；未知请求保持隔离")

const pauseRequestMarker = "upload_pause_requested_v1"
const pauseClearMarker = "upload_pause_cleared_v1"
const pendingPauseSQL = `(SELECT error FROM events WHERE item=items.id AND error IN ('upload_pause_requested_v1','upload_pause_cleared_v1') ORDER BY sequence DESC LIMIT 1)='upload_pause_requested_v1'`

func (m *Manager) isPauseRequested(item string) bool {
	m.pauseMu.RLock()
	defer m.pauseMu.RUnlock()
	return m.pauseRequests[item]
}
func (m *Manager) setPauseRequested(item string, value bool) {
	m.pauseMu.Lock()
	defer m.pauseMu.Unlock()
	if m.pauseRequests == nil {
		m.pauseRequests = map[string]bool{}
	}
	if value {
		m.pauseRequests[item] = true
	} else {
		delete(m.pauseRequests, item)
	}
}
func (m *Manager) loadPauseRequests() error {
	rows, err := m.db.Query(`SELECT e.item,e.error FROM events e JOIN items i ON i.id=e.item WHERE e.error IN ('upload_pause_requested_v1','upload_pause_cleared_v1') AND i.state IN ('queued','paused','hashing','uploading','submitting','finalizing') ORDER BY e.sequence DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := map[string]bool{}
	pending := map[string]bool{}
	for rows.Next() {
		var item, marker string
		if err = rows.Scan(&item, &marker); err != nil {
			return err
		}
		if seen[item] {
			continue
		}
		seen[item] = true
		if marker == pauseRequestMarker {
			pending[item] = true
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	m.pauseMu.Lock()
	m.pauseRequests = pending
	m.pauseMu.Unlock()
	return nil
}
func pauseApplicable(state string) bool {
	return state == "hashing" || state == "uploading" || state == "submitting" || state == "finalizing"
}

// Pause records intent before acknowledging it. Active mutating requests are
// never cancelled: only a future safe callback can acknowledge a paused state.
func (m *Manager) Pause(batch, item string) (string, error) {
	if !validID(batch) || !validID(item) {
		return "", errors.New("上传ID无效")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var state string
	var sent int64
	if err := m.db.QueryRow(`SELECT state,sent FROM items WHERE batch=? AND id=?`, batch, item).Scan(&state, &sent); err != nil {
		return "", err
	}
	if state == "paused" {
		return "paused", nil
	}
	if state != "queued" && !pauseApplicable(state) {
		return "", errors.New("此阶段不能暂停；接收中请先结束浏览器传输，未知结果需核对")
	}
	result := "pause_requested"
	if state == "queued" {
		result = "paused"
	}
	if m.isPauseRequested(item) {
		return result, nil
	}
	tx, err := m.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	at := now()
	if _, err = tx.Exec(`INSERT INTO events(item,state,sent,recorded,error) VALUES(?,?,?,?,?)`, item, result, sent, at, pauseRequestMarker); err != nil {
		return "", err
	}
	if result == "paused" {
		if _, err = tx.Exec(`UPDATE items SET state='paused',updated=?,error=? WHERE batch=? AND id=? AND state='queued'`, at, errUploadPausedAtCheckpoint.Error(), batch, item); err != nil {
			return "", err
		}
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	m.setPauseRequested(item, true)
	return result, nil
}
func (m *Manager) Resume(batch, item string) error {
	return m.queueRetainedUpload(batch, item, "paused")
}

func (m *Manager) queueRetainedUpload(batch, item, expected string) error {
	if !validID(batch) || !validID(item) {
		return errors.New("上传ID无效")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var state, size string
	if err := m.db.QueryRow(`SELECT state,CAST(size AS TEXT) FROM items WHERE batch=? AND id=?`, batch, item).Scan(&state, &size); err != nil {
		return err
	}
	if state != expected {
		if expected == "failed" {
			return nil
		}
		return errors.New("仅能继续已暂停的上传；未知提交不能放行")
	}
	stat, err := os.Stat(filepath.Join(m.dir, item+".data"))
	if err != nil || strconv.FormatInt(stat.Size(), 10) != size {
		return errors.New("本机暂存文件缺失或大小变化，请重新选择并核对")
	}
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := now()
	result, err := tx.Exec(`UPDATE items SET state='queued',sent=0,updated=?,error='' WHERE batch=? AND id=? AND state=?`, at, batch, item, expected)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errors.New("上传状态已变化，请刷新核对")
	}
	if _, err = tx.Exec(`INSERT INTO events(item,state,sent,recorded,error) VALUES(?,'queued',0,?,?)`, item, at, pauseClearMarker); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	m.setPauseRequested(item, false)
	return nil
}
