package taskstore

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"web_browser_baidu_onekey_upload/internal/durablefs"
)

func migrateV1(store *Store, scope string) error {
	jobsPath := filepath.Join(store.dir, "jobs")
	if err := safePath(jobsPath); err != nil {
		return err
	}
	dir, err := openJobDirectory(jobsPath)
	if err != nil {
		return err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	tempPath := filepath.Join(store.dir, "queue.sqlite.migrating")
	finalPath := filepath.Join(store.dir, "queue.sqlite")
	for _, name := range []string{tempPath, finalPath} {
		if err := safePath(name); err != nil {
			return err
		}
		if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	db, err := openSQLite(tempPath, scope)
	if err != nil {
		return err
	}
	migrationID, err := newRunID()
	if err != nil {
		db.Close()
		return err
	}
	published := false
	defer func() {
		db.Close()
		if !published {
			os.Remove(tempPath)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		tx.Rollback()
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") && !entry.IsDir() {
			continue
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return rollback(fmt.Errorf("unexpected legacy state entry: %s", entry.Name()))
		}
		id, parseErr := strconv.ParseInt(strings.TrimSuffix(entry.Name(), ".json"), 10, 64)
		if parseErr != nil || id <= 0 || strconv.FormatInt(id, 10)+".json" != entry.Name() {
			return rollback(fmt.Errorf("invalid legacy job filename: %s", entry.Name()))
		}
		file, openErr := openJobFileAt(dir, entry.Name())
		if openErr != nil {
			return rollback(openErr)
		}
		body, readErr := io.ReadAll(io.LimitReader(file, 1024*1024+1))
		file.Close()
		if readErr != nil || len(body) > 1024*1024 || !json.Valid(body) {
			if readErr != nil {
				return rollback(readErr)
			}
			return rollback(fmt.Errorf("corrupt legacy job %d", id))
		}
		if err = validateLegacyJob(id, body); err != nil {
			return rollback(err)
		}
		event := Event{Kind: "legacy_import", RunID: migrationID, EvidenceType: "legacy_import", Evidence: json.RawMessage(`{"source_format":1}`)}
		if err = saveJobTx(ctx, tx, migrationID, id, body, event); err != nil {
			return rollback(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if err = db.Close(); err != nil {
		return err
	}
	check, err := openSQLite(tempPath, scope)
	if err != nil {
		return err
	}
	var imported int
	if err = check.QueryRow(`SELECT count(*) FROM jobs`).Scan(&imported); err != nil {
		check.Close()
		return err
	}
	var sourceCount int
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			sourceCount++
		}
	}
	if imported != sourceCount {
		check.Close()
		return fmt.Errorf("legacy migration record count mismatch: source=%d target=%d", sourceCount, imported)
	}
	if err = verifyLegacyEquivalence(check, dir, entries); err != nil {
		check.Close()
		return err
	}
	if err = check.Close(); err != nil {
		return err
	}
	if err = os.Rename(tempPath, finalPath); err != nil {
		return err
	}
	if err = durablefs.SyncDirectory(store.dir); err != nil {
		return err
	}
	body, _ := json.Marshal(manifest{Version: sqliteSchemaVersion, Scope: scope})
	if err = atomicWrite(filepath.Join(store.dir, "manifest.json"), body); err != nil {
		return err
	}
	published = true
	return nil
}

func validateLegacyJob(id int64, body []byte) error {
	var job struct {
		Version int `json:"version"`
		File    struct {
			FsID           int64  `json:"fs_id"`
			Path           string `json:"path"`
			ServerFilename string `json:"server_filename"`
			Size           int64  `json:"size"`
			IsDir          int    `json:"isdir"`
			Mtime          int64  `json:"server_mtime"`
			MD5            string `json:"md5"`
		} `json:"file"`
		Mode              string    `json:"mode"`
		Status            string    `json:"status"`
		SourceState       string    `json:"source_state"`
		SourceOperationID string    `json:"source_operation_id"`
		SourceAttemptID   string    `json:"source_attempt_id"`
		AttemptID         string    `json:"attempt_id"`
		Attempts          int       `json:"attempts"`
		Failures          int       `json:"failures"`
		Error             string    `json:"error"`
		Created           time.Time `json:"created"`
		Updated           time.Time `json:"updated"`
		NextAttempt       time.Time `json:"next_attempt"`
	}
	if err := json.Unmarshal(body, &job); err != nil {
		return fmt.Errorf("invalid legacy job %d: %w", id, err)
	}
	if job.Version != 1 || job.File.FsID != id || (job.Mode != "keep" && job.Mode != "delete") || job.Attempts < 0 || job.Failures < 0 {
		return fmt.Errorf("invalid legacy job identity or counters: %d", id)
	}
	if (job.SourceOperationID == "") != (job.SourceAttemptID == "") {
		return fmt.Errorf("invalid legacy source operation IDs: %d", id)
	}
	for _, value := range []string{job.SourceOperationID, job.SourceAttemptID, job.AttemptID} {
		if value == "" {
			continue
		}
		decoded, err := hex.DecodeString(value)
		if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != value {
			return fmt.Errorf("invalid legacy attempt ID: %d", id)
		}
	}
	switch job.Status {
	case "pending", "downloading", "retry_wait", "auth_wait", "failed", "done", "cancelled", "source_unknown":
	default:
		return fmt.Errorf("invalid legacy job status: %d", id)
	}
	switch job.SourceState {
	case "":
		if job.Status == "source_unknown" {
			return fmt.Errorf("invalid legacy source state: %d", id)
		}
	case "sending", "unknown":
		if job.Status != "source_unknown" {
			return fmt.Errorf("invalid legacy source status: %d", id)
		}
	case "changed":
		if job.Status != "failed" {
			return fmt.Errorf("invalid legacy source status: %d", id)
		}
	case "confirmed":
		if job.Status != "done" {
			return fmt.Errorf("invalid legacy source status: %d", id)
		}
	default:
		return fmt.Errorf("invalid legacy source state: %d", id)
	}
	return nil
}

func verifyLegacyEquivalence(db *sql.DB, dir *os.File, entries []os.DirEntry) error {
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSuffix(entry.Name(), ".json"), 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10)+".json" != entry.Name() {
			return fmt.Errorf("invalid legacy job filename: %s", entry.Name())
		}
		file, err := openJobFileAt(dir, entry.Name())
		if err != nil {
			return err
		}
		want, readErr := io.ReadAll(io.LimitReader(file, 1024*1024+1))
		file.Close()
		if readErr != nil || len(want) > 1024*1024 {
			if readErr != nil {
				return readErr
			}
			return fmt.Errorf("oversized legacy job %d", id)
		}
		var got []byte
		if err = db.QueryRow(`SELECT payload_json FROM jobs WHERE id=?`, id).Scan(&got); err != nil {
			return err
		}
		if string(got) != string(want) {
			return fmt.Errorf("legacy job %d changed during migration", id)
		}
	}
	return nil
}
