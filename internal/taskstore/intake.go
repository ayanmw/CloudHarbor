package taskstore

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func validIntakeID(id string) bool {
	if len(id) != 32 || strings.ToLower(id) != id {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
func (s *Store) SaveIntake(id string, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	if !validIntakeID(id) || len(body) > 1024*1024 || !json.Valid(body) {
		return fmt.Errorf("invalid intake record")
	}
	dir := filepath.Join(s.dir, "intake")
	if err := safePath(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	name := filepath.Join(dir, id+".json")
	if err := safePath(name); err != nil {
		return err
	}
	return atomicWrite(name, body)
}
func (s *Store) LoadIntakes() (map[string]json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	dir := filepath.Join(s.dir, "intake")
	if err := safePath(dir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	result := make(map[string]json.RawMessage)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") && !entry.IsDir() {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || !validIntakeID(id) {
			return nil, fmt.Errorf("unexpected intake record")
		}
		name := filepath.Join(dir, entry.Name())
		if err = safePath(name); err != nil {
			return nil, err
		}
		file, openErr := os.Open(name)
		if openErr != nil {
			return nil, openErr
		}
		body, readErr := io.ReadAll(io.LimitReader(file, 1024*1024+1))
		file.Close()
		if readErr != nil {
			return nil, readErr
		}
		if len(body) > 1024*1024 || !json.Valid(body) {
			return nil, fmt.Errorf("corrupt intake record")
		}
		result[id] = body
	}
	return result, nil
}
