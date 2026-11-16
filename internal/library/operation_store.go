package library

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
)

type WriteRequest struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	RootRevision string `json:"root_revision"`
	Path         string `json:"path,omitempty"`
	Revision     string `json:"revision,omitempty"`
	Target       string `json:"target,omitempty"`
	ParentID     string `json:"parent_identity,omitempty"`
	TrashID      string `json:"trash_id,omitempty"`
	Size         int64  `json:"size,string"`
}
type Operation struct {
	ItemPath         string       `json:"item_path"`
	RootPath         string       `json:"root_path"`
	RecoveredAt      string       `json:"recovered_at_utc,omitempty"`
	ID               string       `json:"id"`
	RootID           string       `json:"root_identity"`
	Request          WriteRequest `json:"request"`
	Status           string       `json:"status"`
	StartedAt        string       `json:"started_at_utc"`
	FinishedAt       string       `json:"finished_at_utc,omitempty"`
	Error            string       `json:"error,omitempty"`
	ItemID           string       `json:"item_identity,omitempty"`
	OriginalParentID string       `json:"original_parent_identity,omitempty"`
	SHA256           string       `json:"sha256,omitempty"`
	Bytes            int64        `json:"bytes,string"`
	RestoredBy       string       `json:"restored_by,omitempty"`
}

func (m *Manager) initOperations() error {
	_, e := m.db.Exec(`CREATE TABLE IF NOT EXISTS library_operations(sequence INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT NOT NULL UNIQUE,status TEXT NOT NULL,body TEXT NOT NULL);`)
	if e != nil {
		return e
	}
	if _, e = m.db.Exec(`CREATE TABLE IF NOT EXISTS library_owner(singleton INTEGER PRIMARY KEY CHECK(singleton=1),token TEXT NOT NULL)`); e != nil {
		return e
	}
	token := randomID()
	if token == "" {
		return errors.New("无法建立文件库私有区身份")
	}
	if _, e = m.db.Exec(`INSERT OR IGNORE INTO library_owner VALUES(1,?)`, token); e != nil {
		return e
	}
	// An interrupted filesystem operation is evidence to inspect, never a retry job.
	_, e = m.db.Exec(`UPDATE library_operations SET status='unknown',body=json_set(body,'$.recovered_at_utc',?,'$.error',?) WHERE status='prepared'`, utcNow(), "进程中断，结果未知；未自动重发")
	return e
}
func (m *Manager) checkUncertain() error {
	var n int
	if e := m.db.QueryRow(`SELECT count(*) FROM library_operations WHERE status IN ('unknown','prepared')`).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return ErrUncertain
	}
	return nil
}
func (m *Manager) prepareOperation(op Operation) error {
	b, e := json.Marshal(op)
	if e != nil {
		return e
	}
	_, e = m.db.Exec(`INSERT INTO library_operations(id,status,body) VALUES(?,?,?)`, op.ID, op.Status, string(b))
	return e
}
func readOperation(row interface{ Scan(...any) error }) (Operation, error) {
	var b, status string
	if e := row.Scan(&b, &status); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return Operation{}, os.ErrNotExist
		}
		return Operation{}, e
	}
	var o Operation
	e := json.Unmarshal([]byte(b), &o)
	o.Status = status
	if status == "unknown" && o.Error == "" {
		o.Error = "进程中断，结果未知；未自动重发"
	}
	return o, e
}
func (m *Manager) operation(id string) (Operation, error) {
	return readOperation(m.db.QueryRow(`SELECT body,status FROM library_operations WHERE id=?`, id))
}
func (m *Manager) Operation(id string) (Operation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.operation(id)
}
func (m *Manager) Operations(offset, limit int) ([]Operation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if offset < 0 || limit < 1 || limit > 100 {
		return nil, ErrUnsafe
	}
	rows, e := m.db.Query(`SELECT body,status FROM library_operations ORDER BY sequence DESC LIMIT ? OFFSET ?`, limit, offset)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Operation{}
	for rows.Next() {
		o, e := readOperation(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (m *Manager) finishOperation(op Operation) error {
	b, e := json.Marshal(op)
	if e != nil {
		return e
	}
	tx, e := m.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`UPDATE library_operations SET body=?,status=? WHERE id=?`, string(b), op.Status, op.ID); e != nil {
		return e
	}
	if op.Request.Kind == "restore" && op.Status == "succeeded" {
		old, e := readOperation(tx.QueryRow(`SELECT body,status FROM library_operations WHERE id=?`, op.Request.TrashID))
		if e != nil {
			return e
		}
		old.RestoredBy = op.ID
		b, e = json.Marshal(old)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(`UPDATE library_operations SET body=? WHERE id=?`, string(b), old.ID); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(`UPDATE library_snapshots SET error=?,error_at=? WHERE root_revision=?`, "本站操作后需要刷新核对", utcNow(), op.Request.RootRevision); e != nil {
		return e
	}
	return tx.Commit()
}
