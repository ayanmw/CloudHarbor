// Package taskstore persists task projections and append-only evidence in SQLite,
// with an OS-backed single-writer lock. Credentials remain in a separate private
// sidecar and are never returned by Load.
package taskstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"web_browser_baidu_onekey_upload/internal/durablefs"
)

type Store struct {
	mu     sync.Mutex
	dir    string
	lock   *os.File
	db     *sql.DB
	runID  string
	closed bool
}
type Record struct {
	ID   int64
	Data []byte
}
type manifest struct {
	Version int    `json:"version"`
	Scope   string `json:"scope"`
}

func safePath(name string) error {
	st, err := os.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("state path is a symlink: %s", name)
	}
	return nil
}
func Open(dir, scope string) (*Store, error) {
	if scope == "" {
		return nil, fmt.Errorf("empty queue scope")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{filepath.Dir(absolute), absolute} {
		if err = safePath(name); err != nil {
			return nil, err
		}
	}
	if err = durablefs.MkdirAll(absolute, 0700); err != nil {
		return nil, err
	}
	lockName := filepath.Join(absolute, "writer.lock")
	if err = safePath(lockName); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(lockName, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockFile(lock); err != nil {
		lock.Close()
		return nil, fmt.Errorf("download directory is already in use: %w", err)
	}
	runID, err := newRunID()
	if err != nil {
		lock.Close()
		return nil, err
	}
	s := &Store{dir: absolute, lock: lock, runID: runID}
	success := false
	defer func() {
		if !success {
			s.Close()
		}
	}()
	manifestName := filepath.Join(absolute, "manifest.json")
	if err = safePath(manifestName); err != nil {
		return nil, err
	}
	body, err := os.ReadFile(manifestName)
	if errors.Is(err, os.ErrNotExist) {
		entries, readErr := os.ReadDir(filepath.Join(absolute, "jobs"))
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return nil, readErr
		}
		if len(entries) > 0 {
			return nil, fmt.Errorf("queue manifest missing but job records exist")
		}
		dbPath := filepath.Join(absolute, "queue.sqlite")
		if _, statErr := os.Lstat(dbPath); statErr == nil {
			return nil, fmt.Errorf("queue manifest missing but SQLite database exists")
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return nil, statErr
		}
		s.db, err = openSQLite(dbPath, scope)
		if err != nil {
			return nil, err
		}
		if err = s.db.Close(); err != nil {
			s.db = nil
			return nil, err
		}
		s.db = nil
		body, _ = json.Marshal(manifest{Version: sqliteSchemaVersion, Scope: scope})
		if err = atomicWrite(manifestName, body); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		var m manifest
		if err = json.Unmarshal(body, &m); err != nil || m.Scope != scope {
			return nil, fmt.Errorf("queue manifest corrupt, unsupported, or bound to a different queue")
		}
		switch m.Version {
		case 1:
			if err = migrateV1(s, scope); err != nil {
				return nil, err
			}
		case sqliteSchemaVersion:
		default:
			return nil, fmt.Errorf("unsupported queue format version %d", m.Version)
		}
	}
	if err = safePath(filepath.Join(absolute, "queue.sqlite")); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(absolute, "queue.sqlite")
	if _, err = os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("queue manifest exists but SQLite database is missing")
	} else if err != nil {
		return nil, err
	}
	if err = verifyExistingSQLite(dbPath, scope); err != nil {
		return nil, err
	}
	s.db, err = openSQLite(dbPath, scope)
	if err != nil {
		return nil, err
	}
	success = true
	return s, nil
}

func atomicWrite(name string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(name), "record-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err = temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	if _, err = temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = os.Rename(temp.Name(), name); err != nil {
		return err
	}
	return durablefs.SyncDirectory(filepath.Dir(name))
}

func (s *Store) Save(id int64, data []byte) error {
	return s.SaveWithEvent(id, data, Event{})
}

func (s *Store) SaveWithEvent(id int64, data []byte, event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	if id <= 0 || len(data) > 1024*1024 || !json.Valid(data) {
		return fmt.Errorf("invalid job record")
	}
	return s.saveTxLocked([]Record{{ID: id, Data: data}}, []Event{event})
}

// SaveBatch commits all job projections and their state evidence in one transaction.
// Callers must keep source work available until this succeeds.
func (s *Store) SaveBatch(records []Record) error {
	return s.SaveBatchWithEvents(records, nil)
}

func (s *Store) SaveBatchWithEvents(records []Record, events []Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	seen := make(map[int64]struct{}, len(records))
	if len(events) != 0 && len(events) != len(records) {
		return fmt.Errorf("event count does not match job count")
	}
	for _, record := range records {
		if record.ID <= 0 || len(record.Data) > 1024*1024 || !json.Valid(record.Data) {
			return fmt.Errorf("invalid job record")
		}
		if _, ok := seen[record.ID]; ok {
			return fmt.Errorf("duplicate job record %d", record.ID)
		}
		seen[record.ID] = struct{}{}
	}
	return s.saveTxLocked(records, events)
}

func (s *Store) saveTxLocked(records []Record, events []Event) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, record := range records {
		var event Event
		if len(events) != 0 {
			event = events[i]
		}
		if err = saveJobTx(ctx, tx, s.runID, record.ID, record.Data, event); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Load() (map[int64]json.RawMessage, error) {
	result := make(map[int64]json.RawMessage)
	err := s.LoadEach(func(id int64, body json.RawMessage) error {
		// LoadEach reuses its record buffer after the callback returns; Load's
		// map API must retain an independent copy for every record.
		result[id] = append(json.RawMessage(nil), body...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// LoadEach validates and visits one record at a time in filename order. The
// callback must not call methods on the same Store, and body is only owned by
// the callback until it returns.
func (s *Store) LoadEach(visit func(int64, json.RawMessage) error) error {
	return s.loadEachSQL(visit, true)
}

// LoadEachRaw visits bounded record bytes without parsing their JSON. The
// visitor must validate or decode each body before trusting it, and may only
// use the body until it returns.
func (s *Store) LoadEachRaw(visit func(int64, json.RawMessage) error) error {
	return s.loadEachSQL(visit, false)
}

func (s *Store) loadEachSQL(visit func(int64, json.RawMessage) error, validateJSON bool) error {
	if visit == nil {
		return fmt.Errorf("nil job record visitor")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	rows, err := s.db.Query(`SELECT id,payload_json FROM jobs ORDER BY (CAST(id AS TEXT) || '.json') COLLATE BINARY`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var body []byte
		if err = rows.Scan(&id, &body); err != nil {
			return err
		}
		if len(body) > 1024*1024 || (validateJSON && !json.Valid(body)) {
			return fmt.Errorf("corrupt job %d", id)
		}
		if err = visit(id, body); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) Events(id int64) ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	rows, err := s.db.Query(`SELECT `+eventColumns+` FROM job_events WHERE job_id=? ORDER BY sequence`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Event, 0)
	for rows.Next() {
		event, scanErr := eventFromRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, event)
	}
	return result, rows.Err()
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var dbErr error
	if s.db != nil {
		dbErr = s.db.Close()
	}
	// Closing releases the OS lock even after a process crash. Keep the inode:
	// unlinking it would let another writer lock a different file at this path.
	lockErr := s.lock.Close()
	if dbErr != nil {
		return dbErr
	}
	return lockErr
}

func (s *Store) SaveSettings(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	if len(data) > 4096 || !json.Valid(data) {
		return fmt.Errorf("invalid settings")
	}
	name := filepath.Join(s.dir, "settings.json")
	if err := safePath(name); err != nil {
		return err
	}
	return atomicWrite(name, data)
}
func (s *Store) LoadSettings() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	name := filepath.Join(s.dir, "settings.json")
	if err := safePath(name); err != nil {
		return nil, err
	}
	body, err := os.ReadFile(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(body) > 4096 || !json.Valid(body) {
		return nil, fmt.Errorf("corrupt settings")
	}
	return body, nil
}
