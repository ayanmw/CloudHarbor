package taskstore

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func (s *Store) AdministratorKey(configured string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", os.ErrClosed
	}
	if configured != "" {
		if len(configured) < 10 || len(configured) > 4096 {
			return "", fmt.Errorf("ADMIN_PASSWORD must contain 10..4096 bytes")
		}
		return configured, nil
	}
	body, err := s.readPrivate("admin-key.json", 8192)
	if err != nil {
		return "", err
	}
	var record struct {
		Version int    `json:"version"`
		Key     string `json:"key"`
	}
	if body != nil {
		if json.Unmarshal(body, &record) != nil || record.Version != 1 || len(record.Key) < 10 || len(record.Key) > 4096 {
			return "", fmt.Errorf("invalid administrator key record")
		}
		if len(record.Key) != 43 {
			return record.Key, nil
		}
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(record.Key)
		if decodeErr != nil || len(decoded) != 32 {
			return "", fmt.Errorf("invalid administrator key record")
		}
		return record.Key, nil
	}
	random := make([]byte, 32)
	if _, err = rand.Read(random); err != nil {
		return "", err
	}
	record.Version = 1
	record.Key = base64.RawURLEncoding.EncodeToString(random)
	body, _ = json.Marshal(record)
	if err = atomicWrite(filepath.Join(s.dir, "admin-key.json"), body); err != nil {
		return "", err
	}
	return record.Key, nil
}
