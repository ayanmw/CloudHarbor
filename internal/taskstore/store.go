// Package taskstore persists one JSON record per root job, with an OS-backed
// single-writer lock. Job records contain no tokens; credentials use a separate
// private sidecar and are never returned by Load.
package taskstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"web_browser_baidu_onekey_upload/internal/durablefs"
)

type Store struct {
	mu     sync.Mutex
	dir    string
	lock   *os.File
	closed bool
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
	s := &Store{dir: absolute, lock: lock}
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
		body, _ = json.Marshal(manifest{Version: 1, Scope: scope})
		if err = atomicWrite(manifestName, body); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		var m manifest
		if err = json.Unmarshal(body, &m); err != nil || m.Version != 1 || m.Scope != scope {
			return nil, fmt.Errorf("queue manifest corrupt, unsupported, or bound to a different queue")
		}
	}
	jobs := filepath.Join(absolute, "jobs")
	if err = safePath(jobs); err != nil {
		return nil, err
	}
	if err = durablefs.MkdirAll(jobs, 0700); err != nil {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	if id <= 0 || len(data) > 1024*1024 || !json.Valid(data) {
		return fmt.Errorf("invalid job record")
	}
	name := filepath.Join(s.dir, "jobs", strconv.FormatInt(id, 10)+".json")
	if err := safePath(name); err != nil {
		return err
	}
	return atomicWrite(name, data)
}
func (s *Store) Load() (map[int64]json.RawMessage, error) {
	result := make(map[int64]json.RawMessage)
	err := s.LoadEach(func(id int64, body json.RawMessage) error {
		result[id] = body
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
	return s.loadEach(visit, true)
}

// LoadEachRaw visits bounded record bytes without parsing their JSON. The
// visitor must validate or decode each body before trusting it, and may only
// use the body until it returns.
func (s *Store) LoadEachRaw(visit func(int64, json.RawMessage) error) error {
	return s.loadEach(visit, false)
}

func (s *Store) loadEach(visit func(int64, json.RawMessage) error, validateJSON bool) error {
	if visit == nil {
		return fmt.Errorf("nil job record visitor")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	jobsDir, err := openJobDirectory(filepath.Join(s.dir, "jobs"))
	if err != nil {
		return err
	}
	defer jobsDir.Close()
	entries, err := jobsDir.ReadDir(-1)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	var body bytes.Buffer
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return fmt.Errorf("unexpected state entry: %s", entry.Name())
		}
		id, err := strconv.ParseInt(strings.TrimSuffix(entry.Name(), ".json"), 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10)+".json" != entry.Name() {
			return fmt.Errorf("invalid job filename: %s", entry.Name())
		}
		f, err := openJobFileAt(jobsDir, entry.Name())
		if err != nil {
			return err
		}
		body.Reset()
		n, err := io.CopyN(&body, f, 1024*1024+1)
		f.Close()
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if n > 1024*1024 || (validateJSON && !json.Valid(body.Bytes())) {
			return fmt.Errorf("corrupt job %d", id)
		}
		if err := visit(id, body.Bytes()); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	// Closing releases the OS lock even after a process crash. Keep the inode:
	// unlinking it would let another writer lock a different file at this path.
	return s.lock.Close()
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
