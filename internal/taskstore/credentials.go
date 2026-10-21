package taskstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

var ErrAccountMismatch = errors.New("download directory is bound to a different Baidu account")

func (s *Store) readPrivate(name string, limit int) ([]byte, error) {
	file := filepath.Join(s.dir, name)
	if err := safePath(file); err != nil {
		return nil, err
	}
	f, err := os.Open(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > limit || !json.Valid(body) {
		return nil, fmt.Errorf("invalid private state file: %s", name)
	}
	return body, nil
}
func (s *Store) LoadCredentials() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	return s.readPrivate("credentials.json", 64*1024)
}
func (s *Store) SaveCredentials(body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	if len(body) > 64*1024 || !json.Valid(body) {
		return fmt.Errorf("invalid credential record")
	}
	name := filepath.Join(s.dir, "credentials.json")
	if err := safePath(name); err != nil {
		return err
	}
	return atomicWrite(name, body)
}
func (s *Store) BindAccount(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	value, err := strconv.ParseInt(id, 10, 64)
	if err != nil || value <= 0 || strconv.FormatInt(value, 10) != id {
		return fmt.Errorf("invalid Baidu account ID")
	}
	body, err := s.readPrivate("account.json", 4096)
	if err != nil {
		return err
	}
	var record struct {
		Version int    `json:"version"`
		ID      string `json:"uk"`
	}
	if body != nil {
		if json.Unmarshal(body, &record) != nil || record.Version != 1 || record.ID == "" {
			return fmt.Errorf("invalid account binding")
		}
		if record.ID != id {
			return ErrAccountMismatch
		}
		return nil
	}
	record.Version = 1
	record.ID = id
	body, _ = json.Marshal(record)
	return atomicWrite(filepath.Join(s.dir, "account.json"), body)
}
