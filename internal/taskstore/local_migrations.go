package taskstore

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const LocalDirectoryMigrationEvidenceEventLimit = 100
const LocalDirectoryMigrationPageLimit = 50
const LocalDirectoryMigrationEventPageLimit = 50
const LocalDirectoryMigrationSummaryExportLimit = 5000

var ErrLocalMigrationEvidenceTooLarge = fmt.Errorf("local directory migration evidence exceeds limit")
var ErrLocalMigrationSummaryExportTooLarge = errors.New("local directory migration summary export exceeds limit")

type LocalDirectoryMigrationEvidenceEvent struct {
	Sequence      int64           `json:"sequence"`
	Status        string          `json:"status"`
	Phase         string          `json:"phase"`
	RecordedAtUTC time.Time       `json:"recorded_at_utc"`
	Evidence      json.RawMessage `json:"evidence"`
}

type LocalDirectoryMigrationEvidenceBundle struct {
	Migration LocalDirectoryMigration                `json:"migration"`
	Events    []LocalDirectoryMigrationEvidenceEvent `json:"events"`
}

type LocalDirectoryMigrationFilter struct {
	Status            string `json:"status,omitempty"`
	Query             string `json:"query,omitempty"`
	StartedFromUTC    string `json:"started_from_utc,omitempty"`
	StartedThroughUTC string `json:"started_through_utc,omitempty"`
}

type LocalDirectoryMigration struct {
	ID                   string    `json:"id"`
	SourcePath           string    `json:"source_path"`
	TargetPath           string    `json:"target_path"`
	DesiredQueuePath     string    `json:"desired_queue_path,omitempty"`
	TombstonePath        string    `json:"tombstone_path"`
	Status               string    `json:"status"`
	Phase                string    `json:"phase"`
	TotalFiles           int64     `json:"total_files"`
	TotalBytes           int64     `json:"total_bytes"`
	ProcessedFiles       int64     `json:"processed_files"`
	ProcessedBytes       int64     `json:"processed_bytes"`
	StartedAtUTC         time.Time `json:"started_at_utc"`
	UpdatedAtUTC         time.Time `json:"updated_at_utc"`
	FinishedAtUTC        time.Time `json:"finished_at_utc,omitempty"`
	LastError            string    `json:"last_error,omitempty"`
	VerifiedTargetSHA256 string    `json:"-"`
}

func validLocalMigration(m LocalDirectoryMigration) bool {
	verifiedDigestValid := m.VerifiedTargetSHA256 == ""
	if len(m.VerifiedTargetSHA256) == 64 {
		_, err := hex.DecodeString(m.VerifiedTargetSHA256)
		verifiedDigestValid = err == nil && strings.ToLower(m.VerifiedTargetSHA256) == m.VerifiedTargetSHA256
	}
	return verifiedDigestValid && len(m.ID) == 32 && filepath.IsAbs(m.SourcePath) && filepath.IsAbs(m.TargetPath) && filepath.IsAbs(m.TombstonePath) && len(m.SourcePath) <= 4096 && len(m.TargetPath) <= 4096 && len(m.TombstonePath) <= 8192 && len(m.DesiredQueuePath) <= 4096 &&
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
		SourcePath           string `json:"source_path"`
		TargetPath           string `json:"target_path"`
		DesiredQueue         string `json:"desired_queue_path,omitempty"`
		Status               string `json:"status"`
		Phase                string `json:"phase"`
		TotalFiles           int64  `json:"total_files"`
		TotalBytes           int64  `json:"total_bytes"`
		ProcessedFiles       int64  `json:"processed_files"`
		ProcessedBytes       int64  `json:"processed_bytes"`
		VerifiedTargetSHA256 string `json:"verified_target_sha256,omitempty"`
		Error                string `json:"error,omitempty"`
	}{m.SourcePath, m.TargetPath, m.DesiredQueuePath, m.Status, m.Phase, m.TotalFiles, m.TotalBytes, m.ProcessedFiles, m.ProcessedBytes, m.VerifiedTargetSHA256, m.LastError})
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO local_directory_migration_events(migration_id,status,phase,recorded_at_utc,evidence_json) VALUES(?,?,?,?,?)`, m.ID, m.Status, m.Phase, m.UpdatedAtUTC.UTC().UnixMicro(), evidence)
	return err
}

func (s *Store) LoadActiveLocalDirectoryMigration() (LocalDirectoryMigration, error) {
	return s.loadLocalDirectoryMigration(`SELECT migration_id,source_path,target_path,desired_queue_path,tombstone_path,status,phase,total_files,total_bytes,processed_files,processed_bytes,started_at_utc,updated_at_utc,finished_at_utc,last_error,COALESCE((SELECT json_extract(evidence_json,'$.verified_target_sha256') FROM local_directory_migration_events WHERE migration_id=local_directory_migrations.migration_id AND json_extract(evidence_json,'$.verified_target_sha256') IS NOT NULL ORDER BY sequence DESC LIMIT 1),'') FROM local_directory_migrations WHERE status='running' ORDER BY started_at_utc DESC LIMIT 1`)
}

func (s *Store) LoadLatestLocalDirectoryMigration() (LocalDirectoryMigration, error) {
	return s.loadLocalDirectoryMigration(`SELECT migration_id,source_path,target_path,desired_queue_path,tombstone_path,status,phase,total_files,total_bytes,processed_files,processed_bytes,started_at_utc,updated_at_utc,finished_at_utc,last_error,COALESCE((SELECT json_extract(evidence_json,'$.verified_target_sha256') FROM local_directory_migration_events WHERE migration_id=local_directory_migrations.migration_id AND json_extract(evidence_json,'$.verified_target_sha256') IS NOT NULL ORDER BY sequence DESC LIMIT 1),'') FROM local_directory_migrations ORDER BY started_at_utc DESC,migration_id DESC LIMIT 1`)
}

// ListLocalDirectoryMigrations reads one stable, bounded page and its total from
// the same SQLite snapshot. Tied start times are ordered by migration ID.
func (s *Store) ListLocalDirectoryMigrations(ctx context.Context, offset, limit int) ([]LocalDirectoryMigration, int, error) {
	return s.ListFilteredLocalDirectoryMigrations(ctx, LocalDirectoryMigrationFilter{}, offset, limit)
}

func (s *Store) ListFilteredLocalDirectoryMigrations(ctx context.Context, filter LocalDirectoryMigrationFilter, offset, limit int) ([]LocalDirectoryMigration, int, error) {
	if offset < 0 || limit < 1 || limit > LocalDirectoryMigrationPageLimit {
		return nil, 0, fmt.Errorf("invalid local directory migration page")
	}
	filter, err := normalizeLocalDirectoryMigrationFilter(filter)
	if err != nil {
		return nil, 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	where, args := localDirectoryMigrationFilterSQL(filter)
	var total int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM local_directory_migrations`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	pageArgs := append(append([]any(nil), args...), limit, offset)
	rows, err := tx.QueryContext(ctx, `SELECT migration_id,source_path,target_path,desired_queue_path,tombstone_path,status,phase,total_files,total_bytes,processed_files,processed_bytes,started_at_utc,updated_at_utc,finished_at_utc,last_error FROM local_directory_migrations`+where+` ORDER BY started_at_utc DESC,migration_id DESC LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		return nil, 0, err
	}
	items := make([]LocalDirectoryMigration, 0, limit)
	for rows.Next() {
		var m LocalDirectoryMigration
		var started, updated int64
		var finished sql.NullInt64
		if err = rows.Scan(&m.ID, &m.SourcePath, &m.TargetPath, &m.DesiredQueuePath, &m.TombstonePath, &m.Status, &m.Phase, &m.TotalFiles, &m.TotalBytes, &m.ProcessedFiles, &m.ProcessedBytes, &started, &updated, &finished, &m.LastError); err != nil {
			rows.Close()
			return nil, 0, err
		}
		m.StartedAtUTC, m.UpdatedAtUTC = time.UnixMicro(started).UTC(), time.UnixMicro(updated).UTC()
		if finished.Valid {
			m.FinishedAtUTC = time.UnixMicro(finished.Int64).UTC()
		}
		if !validLocalMigration(m) {
			rows.Close()
			return nil, 0, fmt.Errorf("corrupt local directory migration record")
		}
		items = append(items, m)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	if err = rows.Close(); err != nil {
		return nil, 0, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Store) ExportFilteredLocalDirectoryMigrationSummaries(ctx context.Context, filter LocalDirectoryMigrationFilter) ([]LocalDirectoryMigration, error) {
	filter, err := normalizeLocalDirectoryMigrationFilter(filter)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	where, args := localDirectoryMigrationFilterSQL(filter)
	var total int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM local_directory_migrations`+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	if total > LocalDirectoryMigrationSummaryExportLimit {
		return nil, ErrLocalMigrationSummaryExportTooLarge
	}
	pageArgs := append(append([]any(nil), args...), LocalDirectoryMigrationSummaryExportLimit+1)
	rows, err := tx.QueryContext(ctx, `SELECT migration_id,source_path,target_path,desired_queue_path,tombstone_path,status,phase,total_files,total_bytes,processed_files,processed_bytes,started_at_utc,updated_at_utc,finished_at_utc,last_error FROM local_directory_migrations`+where+` ORDER BY started_at_utc DESC,migration_id DESC LIMIT ?`, pageArgs...)
	if err != nil {
		return nil, err
	}
	items, err := scanLocalDirectoryMigrationRows(rows)
	if err != nil {
		return nil, err
	}
	if len(items) > LocalDirectoryMigrationSummaryExportLimit {
		return nil, ErrLocalMigrationSummaryExportTooLarge
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) LocalDirectoryMigrationEventPage(ctx context.Context, id string, beforeSequence int64, limit int) ([]LocalDirectoryMigrationEvidenceEvent, int64, bool, error) {
	if len(id) != 32 {
		return nil, 0, false, sql.ErrNoRows
	}
	if _, err := hex.DecodeString(id); err != nil {
		return nil, 0, false, sql.ErrNoRows
	}
	if beforeSequence < 0 || limit < 1 || limit > LocalDirectoryMigrationEventPageLimit {
		return nil, 0, false, fmt.Errorf("invalid local directory migration event page")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, false, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, false, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM local_directory_migrations WHERE migration_id=?`, id).Scan(&exists); err != nil {
		return nil, 0, false, err
	}
	query := `SELECT sequence,status,phase,recorded_at_utc,evidence_json FROM local_directory_migration_events WHERE migration_id=? ORDER BY sequence DESC LIMIT ?`
	args := []any{id, limit + 1}
	if beforeSequence > 0 {
		query = `SELECT sequence,status,phase,recorded_at_utc,evidence_json FROM local_directory_migration_events WHERE migration_id=? AND sequence<? ORDER BY sequence DESC LIMIT ?`
		args = []any{id, beforeSequence, limit + 1}
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, false, err
	}
	events := make([]LocalDirectoryMigrationEvidenceEvent, 0, limit+1)
	for rows.Next() {
		var event LocalDirectoryMigrationEvidenceEvent
		var recorded int64
		var evidence []byte
		if err = rows.Scan(&event.Sequence, &event.Status, &event.Phase, &recorded, &evidence); err != nil {
			_ = rows.Close()
			return nil, 0, false, err
		}
		event.RecordedAtUTC = time.UnixMicro(recorded).UTC()
		event.Evidence = append(json.RawMessage(nil), evidence...)
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return nil, 0, false, err
	}
	if err = rows.Close(); err != nil {
		return nil, 0, false, err
	}
	more := len(events) > limit
	if more {
		events = events[:limit]
	}
	var nextBefore int64
	if more && len(events) > 0 {
		nextBefore = events[len(events)-1].Sequence
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, false, err
	}
	return events, nextBefore, more, nil
}

func normalizeLocalDirectoryMigrationFilter(filter LocalDirectoryMigrationFilter) (LocalDirectoryMigrationFilter, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	if (filter.Status != "" && filter.Status != "running" && filter.Status != "failed" && filter.Status != "completed") || !utf8.ValidString(filter.Query) || len(filter.Query) > 256 {
		return LocalDirectoryMigrationFilter{}, fmt.Errorf("invalid local directory migration filter")
	}
	for _, date := range []string{filter.StartedFromUTC, filter.StartedThroughUTC} {
		if date != "" {
			parsed, err := time.Parse("2006-01-02", date)
			if err != nil || parsed.Format("2006-01-02") != date {
				return LocalDirectoryMigrationFilter{}, fmt.Errorf("invalid local directory migration UTC date")
			}
		}
	}
	if filter.StartedFromUTC != "" && filter.StartedThroughUTC != "" && filter.StartedFromUTC > filter.StartedThroughUTC {
		return LocalDirectoryMigrationFilter{}, fmt.Errorf("invalid local directory migration UTC date range")
	}
	return filter, nil
}

func localDirectoryMigrationFilterSQL(filter LocalDirectoryMigrationFilter) (string, []any) {
	conditions := make([]string, 0, 4)
	args := make([]any, 0, 5)
	if filter.Status != "" {
		conditions = append(conditions, "status=?")
		args = append(args, filter.Status)
	}
	if filter.Query != "" {
		pattern := "%" + escapeLocalMigrationSearch(filter.Query) + "%"
		conditions = append(conditions, `(source_path LIKE ? ESCAPE '\' OR target_path LIKE ? ESCAPE '\')`)
		args = append(args, pattern, pattern)
	}
	if filter.StartedFromUTC != "" {
		start, _ := time.Parse("2006-01-02", filter.StartedFromUTC)
		conditions = append(conditions, "started_at_utc>=?")
		args = append(args, start.UTC().UnixMicro())
	}
	if filter.StartedThroughUTC != "" {
		through, _ := time.Parse("2006-01-02", filter.StartedThroughUTC)
		conditions = append(conditions, "started_at_utc<?")
		args = append(args, through.AddDate(0, 0, 1).UTC().UnixMicro())
	}
	if len(conditions) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func scanLocalDirectoryMigrationRows(rows *sql.Rows) ([]LocalDirectoryMigration, error) {
	items := make([]LocalDirectoryMigration, 0)
	for rows.Next() {
		var m LocalDirectoryMigration
		var started, updated int64
		var finished sql.NullInt64
		if err := rows.Scan(&m.ID, &m.SourcePath, &m.TargetPath, &m.DesiredQueuePath, &m.TombstonePath, &m.Status, &m.Phase, &m.TotalFiles, &m.TotalBytes, &m.ProcessedFiles, &m.ProcessedBytes, &started, &updated, &finished, &m.LastError); err != nil {
			_ = rows.Close()
			return nil, err
		}
		m.StartedAtUTC, m.UpdatedAtUTC = time.UnixMicro(started).UTC(), time.UnixMicro(updated).UTC()
		if finished.Valid {
			m.FinishedAtUTC = time.UnixMicro(finished.Int64).UTC()
		}
		if !validLocalMigration(m) {
			_ = rows.Close()
			return nil, fmt.Errorf("corrupt local directory migration record")
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	return items, rows.Close()
}

func escapeLocalMigrationSearch(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

// ExportLatestLocalDirectoryMigrationEvidence reads the summary and its full
// append-only event history from one SQLite snapshot. It rejects oversized
// histories instead of returning evidence that could look complete when cut off.
func (s *Store) ExportLatestLocalDirectoryMigrationEvidence(ctx context.Context) (LocalDirectoryMigrationEvidenceBundle, error) {
	return s.exportLocalDirectoryMigrationEvidence(ctx, "")
}

// ExportLocalDirectoryMigrationEvidence returns evidence for exactly the
// requested migration ID from one read-only SQLite snapshot.
func (s *Store) ExportLocalDirectoryMigrationEvidence(ctx context.Context, id string) (LocalDirectoryMigrationEvidenceBundle, error) {
	if len(id) != 32 {
		return LocalDirectoryMigrationEvidenceBundle{}, sql.ErrNoRows
	}
	return s.exportLocalDirectoryMigrationEvidence(ctx, id)
}

func (s *Store) exportLocalDirectoryMigrationEvidence(ctx context.Context, id string) (LocalDirectoryMigrationEvidenceBundle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return LocalDirectoryMigrationEvidenceBundle{}, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return LocalDirectoryMigrationEvidenceBundle{}, err
	}
	defer tx.Rollback()
	var bundle LocalDirectoryMigrationEvidenceBundle
	var started, updated int64
	var finished sql.NullInt64
	m := &bundle.Migration
	query := `SELECT migration_id,source_path,target_path,desired_queue_path,tombstone_path,status,phase,total_files,total_bytes,processed_files,processed_bytes,started_at_utc,updated_at_utc,finished_at_utc,last_error FROM local_directory_migrations WHERE migration_id=?`
	if id == "" {
		query = `SELECT migration_id,source_path,target_path,desired_queue_path,tombstone_path,status,phase,total_files,total_bytes,processed_files,processed_bytes,started_at_utc,updated_at_utc,finished_at_utc,last_error FROM local_directory_migrations ORDER BY started_at_utc DESC,migration_id DESC LIMIT 1`
		err = tx.QueryRowContext(ctx, query).Scan(&m.ID, &m.SourcePath, &m.TargetPath, &m.DesiredQueuePath, &m.TombstonePath, &m.Status, &m.Phase, &m.TotalFiles, &m.TotalBytes, &m.ProcessedFiles, &m.ProcessedBytes, &started, &updated, &finished, &m.LastError)
	} else {
		err = tx.QueryRowContext(ctx, query, id).Scan(&m.ID, &m.SourcePath, &m.TargetPath, &m.DesiredQueuePath, &m.TombstonePath, &m.Status, &m.Phase, &m.TotalFiles, &m.TotalBytes, &m.ProcessedFiles, &m.ProcessedBytes, &started, &updated, &finished, &m.LastError)
	}
	if err != nil {
		return LocalDirectoryMigrationEvidenceBundle{}, err
	}
	m.StartedAtUTC, m.UpdatedAtUTC = time.UnixMicro(started).UTC(), time.UnixMicro(updated).UTC()
	if finished.Valid {
		m.FinishedAtUTC = time.UnixMicro(finished.Int64).UTC()
	}
	if !validLocalMigration(*m) {
		return LocalDirectoryMigrationEvidenceBundle{}, fmt.Errorf("corrupt local directory migration record")
	}
	rows, err := tx.QueryContext(ctx, `SELECT sequence,status,phase,recorded_at_utc,evidence_json FROM local_directory_migration_events WHERE migration_id=? ORDER BY sequence ASC LIMIT ?`, m.ID, LocalDirectoryMigrationEvidenceEventLimit+1)
	if err != nil {
		return LocalDirectoryMigrationEvidenceBundle{}, err
	}
	bundle.Events = make([]LocalDirectoryMigrationEvidenceEvent, 0, LocalDirectoryMigrationEvidenceEventLimit)
	for rows.Next() {
		var event LocalDirectoryMigrationEvidenceEvent
		var recorded int64
		var evidence []byte
		if err = rows.Scan(&event.Sequence, &event.Status, &event.Phase, &recorded, &evidence); err != nil {
			rows.Close()
			return LocalDirectoryMigrationEvidenceBundle{}, err
		}
		event.RecordedAtUTC = time.UnixMicro(recorded).UTC()
		event.Evidence = append(json.RawMessage(nil), evidence...)
		bundle.Events = append(bundle.Events, event)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return LocalDirectoryMigrationEvidenceBundle{}, err
	}
	if err = rows.Close(); err != nil {
		return LocalDirectoryMigrationEvidenceBundle{}, err
	}
	if len(bundle.Events) > LocalDirectoryMigrationEvidenceEventLimit {
		return LocalDirectoryMigrationEvidenceBundle{}, ErrLocalMigrationEvidenceTooLarge
	}
	if err = tx.Commit(); err != nil {
		return LocalDirectoryMigrationEvidenceBundle{}, err
	}
	return bundle, nil
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
	err := s.db.QueryRow(query).Scan(&m.ID, &m.SourcePath, &m.TargetPath, &m.DesiredQueuePath, &m.TombstonePath, &m.Status, &m.Phase, &m.TotalFiles, &m.TotalBytes, &m.ProcessedFiles, &m.ProcessedBytes, &started, &updated, &finished, &m.LastError, &m.VerifiedTargetSHA256)
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
