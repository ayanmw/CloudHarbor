package taskstore

import (
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"
)

func validateSessionHash(hash string) error {
	if len(hash) != 64 || strings.ToLower(hash) != hash {
		return fmt.Errorf("invalid administrator session hash")
	}
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != 32 {
		return fmt.Errorf("invalid administrator session hash")
	}
	return nil
}

func (s *Store) CreateAdminSession(hash string, createdAt time.Time) error {
	if err := validateSessionHash(hash); err != nil || createdAt.IsZero() {
		return fmt.Errorf("invalid administrator session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	_, err := s.db.Exec(`INSERT INTO admin_sessions(session_hash,created_at_utc) VALUES(?,?)`, hash, createdAt.UTC().UnixNano())
	return err
}

func (s *Store) AdminSessionActive(hash string) (bool, error) {
	if err := validateSessionHash(hash); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false, os.ErrClosed
	}
	var active int
	err := s.db.QueryRow(`SELECT 1 FROM admin_sessions WHERE session_hash=? AND revoked_at_utc IS NULL`, hash).Scan(&active)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	return err == nil, nil
}

func (s *Store) RevokeAdminSession(hash string, revokedAt time.Time) error {
	if err := validateSessionHash(hash); err != nil || revokedAt.IsZero() {
		return fmt.Errorf("invalid administrator session revocation")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	_, err := s.db.Exec(`UPDATE admin_sessions SET revoked_at_utc=? WHERE session_hash=? AND revoked_at_utc IS NULL`, revokedAt.UTC().UnixNano(), hash)
	return err
}

func (s *Store) RevokeAllAdminSessions(revokedAt time.Time) error {
	if revokedAt.IsZero() {
		return fmt.Errorf("invalid administrator session revocation time")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	_, err := s.db.Exec(`UPDATE admin_sessions SET revoked_at_utc=? WHERE revoked_at_utc IS NULL`, revokedAt.UTC().UnixNano())
	return err
}
