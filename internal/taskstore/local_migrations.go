package taskstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type LocalDirectoryMigration struct {
	ID               string    `json:"id"`
	SourcePath       string    `json:"source_path"`
	TargetPath       string    `json:"target_path"`
	DesiredQueuePath string    `json:"desired_queue_path,omitempty"`
	TombstonePath    string    `json:"tombstone_path"`
	Status           string    `json:"status"`
	Phase            string    `json:"phase"`
	TotalFiles       int64     `json:"total_files"`
	TotalBytes       int64     `json:"total_bytes"`
	ProcessedFiles   int64     `json:"processed_files"`
	ProcessedBytes   int64     `json:"processed_bytes"`
	StartedAtUTC     time.Time `json:"started_at_utc"`
	UpdatedAtUTC     time.Time `json:"updated_at_utc"`
	FinishedAtUTC    time.Time `json:"finished_at_utc,omitempty"`
	LastError        string    `json:"last_error,omitempty"`
}

func validLocalMigration(m LocalDirectoryMigration) bool {
	return len(m.ID) == 32 && filepath.IsAbs(m.SourcePath) && filepath.IsAbs(m.TargetPath) && filepath.IsAbs(m.TombstonePath) && len(m.SourcePath) <= 4096 && len(m.TargetPath) <= 4096 && len(m.TombstonePath) <= 8192 && len(m.DesiredQueuePath) <= 4096 &&
		(m.Status == "running" || m.Status == "failed" || m.Status == "completed") &&
		(m.Phase == "copying" || m.Phase == "verifying" || m.Phase == "retiring_source" || m.Phase == "source_retired" || m.Phase == "deleting_source" || m.Phase == "complete") &&
		m.TotalFiles >= 0 && m.TotalBytes >= 0 && m.ProcessedFiles >= 0 && m.ProcessedBytes >= 0 && !m.StartedAtUTC.IsZero() && !m.UpdatedAtUTC.IsZero() && len(m.LastError) <= 2048
}

func (s *Store) CreateLocalDirectoryMigration(m LocalDirectoryMigration) error {
	if !validLocalMigration(m) || m.Status != "running" {
		return fmt.Errorf("invalid local directory migration")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO local_directory_migrations(migration_id,source_path,target_path,desired_queue_path,tombstone_path,status,phase,total_files,total_bytes,processed_files,processed_bytes,started_at_utc,updated_at_utc,finished_at_utc,last_error) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,NULL,?)`, m.ID, m.SourcePath, m.TargetPath, m.DesiredQueuePath, m.TombstonePath, m.Status, m.Phase, m.TotalFiles, m.TotalBytes, m.ProcessedFiles, m.ProcessedBytes, m.StartedAtUTC.UTC().UnixMicro(), m.UpdatedAtUTC.UTC().UnixMicro(), m.LastError)
	if err == nil {
		err = appendLocalMigrationEvent(tx, m)
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateLocalDirectoryMigration(m LocalDirectoryMigration, appendEvent bool) error {
	if !validLocalMigration(m) {
		return fmt.Errorf("invalid local directory migration")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	var finished any
	if !m.FinishedAtUTC.IsZero() {
		finished = m.FinishedAtUTC.UTC().UnixMicro()
	}
	result, err := tx.Exec(`UPDATE local_directory_migrations SET status=?,phase=?,total_files=?,total_bytes=?,processed_files=?,processed_bytes=?,updated_at_utc=?,finished_at_utc=?,last_error=? WHERE migration_id=? AND source_path=? AND target_path=?`, m.Status, m.Phase, m.TotalFiles, m.TotalBytes, m.ProcessedFiles, m.ProcessedBytes, m.UpdatedAtUTC.UTC().UnixMicro(), finished, m.LastError, m.ID, m.SourcePath, m.TargetPath)
	if err == nil {
		var rows int64
		rows, err = result.RowsAffected()
		if err == nil && rows != 1 {
			err = sql.ErrNoRows
		}
	}
	if err == nil && appendEvent {
		err = appendLocalMigrationEvent(tx, m)
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func appendLocalMigrationEvent(tx *sql.Tx, m LocalDirectoryMigration) error {
	evidence, err := json.Marshal(struct {
		SourcePath     string `json:"source_path"`
		TargetPath     string `json:"target_path"`
		DesiredQueue   string `json:"desired_queue_path,omitempty"`
		Status         string `json:"status"`
		Phase          string `json:"phase"`
		TotalFiles     int64  `json:"total_files"`
		TotalBytes     int64  `json:"total_bytes"`
		ProcessedFiles int64  `json:"processed_files"`
		ProcessedBytes int64  `json:"processed_bytes"`
		Error          string `json:"error,omitempty"`
	}{m.SourcePath, m.TargetPath, m.DesiredQueuePath, m.Status, m.Phase, m.TotalFiles, m.TotalBytes, m.ProcessedFiles, m.ProcessedBytes, m.LastError})
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO local_directory_migration_events(migration_id,status,phase,recorded_at_utc,evidence_json) VALUES(?,?,?,?,?)`, m.ID, m.Status, m.Phase, m.UpdatedAtUTC.UTC().UnixMicro(), evidence)
	return err
}

func (s *Store) LoadActiveLocalDirectoryMigration() (LocalDirectoryMigration, error) {
	return s.loadLocalDirectoryMigration(`SELECT migration_id,source_path,target_path,desired_queue_path,tombstone_path,status,phase,total_files,total_bytes,processed_files,processed_bytes,started_at_utc,updated_at_utc,finished_at_utc,last_error FROM local_directory_migrations WHERE status='running' ORDER BY started_at_utc DESC LIMIT 1`)
}

func (s *Store) LoadLatestLocalDirectoryMigration() (LocalDirectoryMigration, error) {
	return s.loadLocalDirectoryMigration(`SELECT migration_id,source_path,target_path,desired_queue_path,tombstone_path,status,phase,total_files,total_bytes,processed_files,processed_bytes,started_at_utc,updated_at_utc,finished_at_utc,last_error FROM local_directory_migrations ORDER BY started_at_utc DESC LIMIT 1`)
}

func (s *Store) loadLocalDirectoryMigration(query string) (LocalDirectoryMigration, error) {
	var m LocalDirectoryMigration
	var started, updated int64
	var finished sql.NullInt64
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return m, os.ErrClosed
	}
	err := s.db.QueryRow(query).Scan(&m.ID, &m.SourcePath, &m.TargetPath, &m.DesiredQueuePath, &m.TombstonePath, &m.Status, &m.Phase, &m.TotalFiles, &m.TotalBytes, &m.ProcessedFiles, &m.ProcessedBytes, &started, &updated, &finished, &m.LastError)
	if err != nil {
		return m, err
	}
	m.StartedAtUTC, m.UpdatedAtUTC = time.UnixMicro(started).UTC(), time.UnixMicro(updated).UTC()
	if finished.Valid {
		m.FinishedAtUTC = time.UnixMicro(finished.Int64).UTC()
	}
	if !validLocalMigration(m) {
		return LocalDirectoryMigration{}, fmt.Errorf("corrupt local directory migration record")
	}
	return m, nil
}
