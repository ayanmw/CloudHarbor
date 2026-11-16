// Package taskstore persists task projections and append-only evidence in SQLite,
// with an OS-backed single-writer lock. Credentials remain in a separate private
// sidecar and are never returned by Load.
package taskstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/durablefs"
)

type Store struct {
	mu              sync.Mutex
	dir             string
	lock            *os.File
	db              *sql.DB
	runID           string
	scope           string
	migrationAction string
	closed          bool
}

type StartupDiagnostics struct {
	RecordedAtUTC       time.Time `json:"recorded_at_utc"`
	RunID               string    `json:"run_id"`
	SchemaVersion       int       `json:"schema_version"`
	ScopeFingerprint    string    `json:"scope_fingerprint"`
	JobCount            int64     `json:"job_count"`
	EventCount          int64     `json:"event_count"`
	LatestEventSequence int64     `json:"latest_event_sequence"`
	DatabaseSizeBytes   int64     `json:"database_size_bytes"`
	MigrationAction     string    `json:"migration_action"`
	IntegrityCheck      string    `json:"integrity_check"`
	JournalMode         string    `json:"journal_mode"`
	Synchronous         int       `json:"synchronous"`
	ForeignKeys         bool      `json:"foreign_keys"`
	BusyTimeoutMS       int       `json:"busy_timeout_ms"`
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
	// Go 1.23 no longer marks junctions as ModeSymlink. Retain the existing
	// private-state boundary by inspecting Windows reparse attributes as well.
	if st.Mode()&os.ModeSymlink != 0 || statePathReparse(st) {
		return fmt.Errorf("state path is a symlink: %s", name)
	}
	return nil
}

func cleanupAbandonedAtomicTemps(dir string) {
	if safePath(dir) != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasPrefix(name, "record-") || !strings.HasSuffix(name, ".tmp") {
			continue
		}
		if len(name) <= len("record-.tmp") {
			continue
		}
		// Temporary files are non-authoritative; failed cleanup can be retried
		// on the next locked startup without blocking queue recovery.
		_ = os.Remove(filepath.Join(dir, name))
	}
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
	s := &Store{dir: absolute, lock: lock, runID: runID, scope: scope}
	// CreateTemp-based atomic writes may leave record-*.tmp after a process
	// crash. The writer lock is already held, so only abandoned files from this
	// store can be cleaned before the current process starts serving requests.
	for _, tempDir := range []string{absolute, filepath.Join(absolute, "jobs"), filepath.Join(absolute, "intake")} {
		cleanupAbandonedAtomicTemps(tempDir)
	}
	migrationAction := "reopened"
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
		migrationAction = "created"
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
		if m.Version < sqliteSchemaVersion && m.Version != 1 {
			if recoveredDB, recoveryErr := openSQLiteExisting(filepath.Join(absolute, "queue.sqlite"), scope); recoveryErr == nil {
				if closeErr := recoveredDB.Close(); closeErr != nil {
					return nil, closeErr
				}
				m.Version = sqliteSchemaVersion
				body, _ = json.Marshal(m)
				if err = atomicWrite(manifestName, body); err != nil {
					return nil, err
				}
				migrationAction = "recovered_manifest"
			}
		}
		switch m.Version {
		case 1:
			migrationAction = "migrated_v1"
			if err = migrateV1(s, scope); err != nil {
				return nil, err
			}
		case 2:
			migrationAction = "migrated_v2"
			if err = migrateV2(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV3(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV4(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV5(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV6(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV7(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV8(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV9(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV10(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV11(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV12(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV13(s, scope); err != nil {
				return nil, err
			}
		case 3:
			migrationAction = "migrated_v3"
			if err = migrateV3(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV4(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV5(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV6(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV7(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV8(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV9(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV10(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV11(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV12(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV13(s, scope); err != nil {
				return nil, err
			}
		case 4:
			migrationAction = "migrated_v4"
			if err = migrateV4(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV5(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV6(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV7(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV8(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV9(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV10(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV11(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV12(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV13(s, scope); err != nil {
				return nil, err
			}
		case 5:
			migrationAction = "migrated_v5"
			if err = migrateV5(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV6(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV7(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV8(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV9(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV10(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV11(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV12(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV13(s, scope); err != nil {
				return nil, err
			}
		case 6:
			migrationAction = "migrated_v6"
			if err = migrateV6(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV7(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV8(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV9(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV10(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV11(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV12(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV13(s, scope); err != nil {
				return nil, err
			}
		case 7:
			migrationAction = "migrated_v7"
			if err = migrateV7(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV8(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV9(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV10(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV11(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV12(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV13(s, scope); err != nil {
				return nil, err
			}
		case 8:
			migrationAction = "migrated_v8"
			if err = migrateV8(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV9(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV10(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV11(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV12(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV13(s, scope); err != nil {
				return nil, err
			}
		case 9:
			migrationAction = "migrated_v9"
			if err = migrateV9(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV10(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV11(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV12(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV13(s, scope); err != nil {
				return nil, err
			}
		case 10:
			migrationAction = "migrated_v10"
			if err = migrateV10(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV11(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV12(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV13(s, scope); err != nil {
				return nil, err
			}
		case 11:
			migrationAction = "migrated_v11"
			if err = migrateV11(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV12(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV13(s, scope); err != nil {
				return nil, err
			}
		case 12:
			migrationAction = "migrated_v12"
			if err = migrateV12(s, scope); err != nil {
				return nil, err
			}
			if err = migrateV13(s, scope); err != nil {
				return nil, err
			}
		case 13:
			migrationAction = "migrated_v13"
			if err = migrateV13(s, scope); err != nil {
				return nil, err
			}
		case 14:
			migrationAction = "migrated_v14"
		case 15:
			migrationAction = "migrated_v15"
		case 16:
			migrationAction = "migrated_v16"
		case 17:
			migrationAction = "migrated_v17"
		case 18:
			migrationAction = "migrated_v18"
		case 19:
			migrationAction = "migrated_v19"
		case 20:
			migrationAction = "migrated_v20"
		case 21:
			migrationAction = "migrated_v21"
		case 22:
			migrationAction = "migrated_v22"
		case 23:
			migrationAction = "migrated_v23"
		case 24:
			migrationAction = "migrated_v24"
		case 25:
			migrationAction = "migrated_v25"
		case 26:
			migrationAction = "migrated_v26"
		case 27:
			migrationAction = "migrated_v27"
		case 28:
			migrationAction = "migrated_v28"
		case 29:
			migrationAction = "migrated_v29"
		case 30:
			migrationAction = "migrated_v30"
		case 31:
			migrationAction = "migrated_v31"
		case 32:
			migrationAction = "migrated_v32"
		case 33:
			migrationAction = "migrated_v33"
		case 34:
			migrationAction = "migrated_v34"
		case 35:
			migrationAction = "migrated_v35"
		case 36:
			migrationAction = "migrated_v36"
		case 37:
			migrationAction = "migrated_v37"
		case 38:
			migrationAction = "migrated_v38"
		case 39:
			migrationAction = "migrated_v39"
		case 40:
			migrationAction = "migrated_v40"
		case 41:
			migrationAction = "migrated_v41"
		case 42:
			migrationAction = "migrated_v42"
		case sqliteSchemaVersion:
		default:
			return nil, fmt.Errorf("unsupported queue format version %d", m.Version)
		}
		if m.Version < sqliteSchemaVersion && m.Version != 1 {
			if m.Version < 20 {
				if err = migrateV14(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV15(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV16(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV17(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV18(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV19(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV20(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV21(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV22(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV23(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV24(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV25(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV26(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV27(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV28(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV29(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV30(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV31(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV32(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV33(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV34(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV35(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV36(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV37(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV38(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV39(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV40(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV41(s, scope); err != nil {
					return nil, err
				}
				if err = migrateV42(s, scope); err != nil {
					return nil, err
				}
			} else {
				migrations := []struct {
					version int
					apply   func(*Store, string) error
				}{
					{20, migrateV20}, {21, migrateV21}, {22, migrateV22}, {23, migrateV23},
					{24, migrateV24}, {25, migrateV25}, {26, migrateV26}, {27, migrateV27}, {28, migrateV28}, {29, migrateV29}, {30, migrateV30}, {31, migrateV31}, {32, migrateV32}, {33, migrateV33}, {34, migrateV34}, {35, migrateV35}, {36, migrateV36}, {37, migrateV37}, {38, migrateV38}, {39, migrateV39}, {40, migrateV40}, {41, migrateV41}, {42, migrateV42},
				}
				for _, migration := range migrations {
					if m.Version <= migration.version {
						if err = migration.apply(s, scope); err != nil {
							return nil, err
						}
					}
				}
			}
		}
	}
	s.migrationAction = migrationAction
	if err = safePath(filepath.Join(absolute, "queue.sqlite")); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(absolute, "queue.sqlite")
	if _, err = os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("queue manifest exists but SQLite database is missing")
	} else if err != nil {
		return nil, err
	}
	s.db, err = openSQLiteExisting(dbPath, scope)
	if err != nil {
		return nil, err
	}
	if err = s.recoverRemoteOperations(); err != nil {
		return nil, err
	}
	if err = s.recoverCleanupBatches(); err != nil {
		return nil, err
	}
	success = true
	return s, nil
}

// OpenUsingExistingScope keeps historical task storage bound to its original
// monitor path when an operator changes that path through persisted settings.
func OpenUsingExistingScope(dir, fallbackScope string) (*Store, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(abs, "manifest.json")
	if err = safePath(filepath.Dir(abs)); err != nil {
		return nil, err
	}
	if err = safePath(abs); err != nil {
		return nil, err
	}
	if err = safePath(manifestPath); err != nil {
		return nil, err
	}
	body, err := os.ReadFile(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return Open(dir, fallbackScope)
	}
	if err != nil {
		return nil, err
	}
	var existing manifest
	if err = json.Unmarshal(body, &existing); err != nil || existing.Scope == "" {
		return nil, fmt.Errorf("queue manifest corrupt or unsupported")
	}
	return Open(dir, existing.Scope)
}

// StartupDiagnostics returns a read-only summary of the queue opened by this
// process. IntegrityCheck reports the successful check already performed by
// Open; this method deliberately does not scan the database a second time.
func (s *Store) StartupDiagnostics() (StartupDiagnostics, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return StartupDiagnostics{}, os.ErrClosed
	}
	diagnostic := StartupDiagnostics{
		RecordedAtUTC:   time.Now().UTC(),
		RunID:           s.runID,
		MigrationAction: s.migrationAction,
		IntegrityCheck:  "ok",
	}
	scopeHash := sha256.Sum256([]byte(s.scope))
	diagnostic.ScopeFingerprint = hex.EncodeToString(scopeHash[:8])
	var version string
	if err := s.db.QueryRow(`SELECT value FROM store_meta WHERE key='schema_version'`).Scan(&version); err != nil {
		return StartupDiagnostics{}, err
	}
	if _, err := fmt.Sscanf(version, "%d", &diagnostic.SchemaVersion); err != nil {
		return StartupDiagnostics{}, fmt.Errorf("read SQLite schema version: %w", err)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM jobs`).Scan(&diagnostic.JobCount); err != nil {
		return StartupDiagnostics{}, err
	}
	if err := s.db.QueryRow(`SELECT count(*), coalesce(max(sequence),0) FROM job_events`).Scan(&diagnostic.EventCount, &diagnostic.LatestEventSequence); err != nil {
		return StartupDiagnostics{}, err
	}
	var foreignKeys int
	for _, pragma := range []struct {
		query string
		dest  any
	}{{"PRAGMA journal_mode", &diagnostic.JournalMode}, {"PRAGMA synchronous", &diagnostic.Synchronous}, {"PRAGMA foreign_keys", &foreignKeys}, {"PRAGMA busy_timeout", &diagnostic.BusyTimeoutMS}} {
		if err := s.db.QueryRow(pragma.query).Scan(pragma.dest); err != nil {
			return StartupDiagnostics{}, err
		}
	}
	diagnostic.ForeignKeys = foreignKeys == 1
	stat, err := os.Stat(filepath.Join(s.dir, "queue.sqlite"))
	if err != nil {
		return StartupDiagnostics{}, err
	}
	diagnostic.DatabaseSizeBytes = stat.Size()
	return diagnostic, nil
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
	if err = durablefs.ReplaceFile(temp.Name(), name); err != nil {
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

// LoadEachRaw visits bounded record bytes in ascending ID order without
// parsing their JSON. The visitor must validate or decode each body before
// trusting it, and may only use the body until it returns.
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
	order := `(CAST(id AS TEXT) || '.json') COLLATE BINARY`
	if !validateJSON {
		// Startup only needs deterministic order; the integer primary key avoids
		// sorting every row by a legacy filename expression.
		order = `id`
	}
	rows, err := s.db.Query(`SELECT id,payload_json FROM jobs ORDER BY ` + order)
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

// EventPage returns the newest bounded event window in sequence order. A
// nonzero beforeSequence is an exclusive upper bound; callers can use the
// returned cursor to fetch older rows without shifting when new events append.
func (s *Store) EventPage(id, beforeSequence int64, limit int) ([]Event, int64, bool, error) {
	if id <= 0 || beforeSequence < 0 || limit < 1 || limit > 50 {
		return nil, 0, false, fmt.Errorf("invalid task event page")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, 0, false, os.ErrClosed
	}
	var exists int
	if err := s.db.QueryRow(`SELECT 1 FROM jobs WHERE id=?`, id).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, 0, false, os.ErrNotExist
		}
		return nil, 0, false, err
	}
	query := `SELECT ` + eventColumns + ` FROM job_events WHERE job_id=?`
	args := []any{id}
	if beforeSequence > 0 {
		query += ` AND sequence<?`
		args = append(args, beforeSequence)
	}
	query += ` ORDER BY sequence DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, false, err
	}
	defer rows.Close()
	events := make([]Event, 0, limit+1)
	for rows.Next() {
		event, scanErr := eventFromRow(rows)
		if scanErr != nil {
			return nil, 0, false, scanErr
		}
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, false, err
	}
	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}
	for left, right := 0, len(events)-1; left < right; left, right = left+1, right-1 {
		events[left], events[right] = events[right], events[left]
	}
	var nextBefore int64
	if hasMore {
		nextBefore = events[0].Sequence
	}
	return events, nextBefore, hasMore, nil
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

func ReadSettingsFile(dir string) ([]byte, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err = safePath(filepath.Dir(abs)); err != nil {
		return nil, err
	}
	if err = safePath(abs); err != nil {
		return nil, err
	}
	name := filepath.Join(abs, "settings.json")
	if err = safePath(name); err != nil {
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
