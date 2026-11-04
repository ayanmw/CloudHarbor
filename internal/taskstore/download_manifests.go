package taskstore

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"strings"
	"time"
)

const DownloadManifestBatchLimit = 512

const downloadManifestSchema = `
CREATE TABLE IF NOT EXISTS download_manifests (
 manifest_id TEXT PRIMARY KEY CHECK(length(manifest_id)=32),
 root_id INTEGER NOT NULL REFERENCES jobs(id) ON DELETE RESTRICT,
 root_path TEXT NOT NULL,
 attempt_id TEXT NOT NULL,
 started_at_utc INTEGER NOT NULL,
 published_at_utc INTEGER,
 UNIQUE(root_id,manifest_id)
);
CREATE TABLE IF NOT EXISTS download_manifest_entries (
 manifest_id TEXT NOT NULL REFERENCES download_manifests(manifest_id) ON DELETE RESTRICT,
 fs_id INTEGER NOT NULL CHECK(fs_id>0),
 path TEXT NOT NULL,
 parent_path TEXT NOT NULL,
 name TEXT NOT NULL,
 is_dir INTEGER NOT NULL CHECK(is_dir IN (0,1)),
 size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),
 provider_mtime INTEGER NOT NULL,
 cloud_md5 TEXT NOT NULL,
 relative_path TEXT NOT NULL,
 observed_at_utc INTEGER NOT NULL,
 observation_source TEXT NOT NULL CHECK(observation_source IN ('root_task_snapshot','directory_listing')),
 search_path TEXT NOT NULL,
 PRIMARY KEY(manifest_id,fs_id),
 UNIQUE(manifest_id,path)
);
CREATE TABLE IF NOT EXISTS download_manifest_current (
 root_id INTEGER PRIMARY KEY REFERENCES jobs(id) ON DELETE RESTRICT,
 manifest_id TEXT NOT NULL UNIQUE,
 FOREIGN KEY(root_id,manifest_id) REFERENCES download_manifests(root_id,manifest_id) ON DELETE RESTRICT
);
CREATE INDEX IF NOT EXISTS download_manifests_root_order ON download_manifests(root_id,started_at_utc,manifest_id);
CREATE TABLE IF NOT EXISTS download_manifest_totals (
 manifest_id TEXT PRIMARY KEY REFERENCES download_manifests(manifest_id) ON DELETE RESTRICT,
 files INTEGER NOT NULL CHECK(files>=0),
 folders INTEGER NOT NULL CHECK(folders>=0),
 size_bytes INTEGER NOT NULL CHECK(size_bytes>=0),
 completed INTEGER NOT NULL DEFAULT 0 CHECK(completed>=0 AND completed<=files),
 completed_bytes INTEGER NOT NULL DEFAULT 0 CHECK(completed_bytes>=0 AND completed_bytes<=size_bytes)
);
CREATE TABLE IF NOT EXISTS download_manifest_results (
 manifest_id TEXT NOT NULL,
 fs_id INTEGER NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('published','local_verified')),
 sha256 TEXT NOT NULL CHECK(length(sha256)=64),
 recorded_at_utc INTEGER NOT NULL,
 PRIMARY KEY(manifest_id,fs_id),
 FOREIGN KEY(manifest_id,fs_id) REFERENCES download_manifest_entries(manifest_id,fs_id) ON DELETE RESTRICT
);
CREATE TABLE IF NOT EXISTS download_manifest_states (
 manifest_id TEXT NOT NULL,
 fs_id INTEGER NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('downloading','failed','cancelled','auth_wait')),
 error_summary TEXT NOT NULL,
 run_id TEXT NOT NULL,
 recorded_at_utc INTEGER NOT NULL,
 PRIMARY KEY(manifest_id,fs_id),
 FOREIGN KEY(manifest_id,fs_id) REFERENCES download_manifest_entries(manifest_id,fs_id) ON DELETE RESTRICT
);
CREATE TRIGGER IF NOT EXISTS download_manifest_results_no_update BEFORE UPDATE ON download_manifest_results BEGIN SELECT RAISE(ABORT,'download results are append-only'); END;
CREATE TRIGGER IF NOT EXISTS download_manifest_results_no_delete BEFORE DELETE ON download_manifest_results BEGIN SELECT RAISE(ABORT,'download results are append-only'); END;
CREATE TRIGGER IF NOT EXISTS download_manifest_results_totals AFTER INSERT ON download_manifest_results BEGIN UPDATE download_manifest_totals SET completed=completed+1,completed_bytes=completed_bytes+(SELECT size_bytes FROM download_manifest_entries WHERE manifest_id=NEW.manifest_id AND fs_id=NEW.fs_id) WHERE manifest_id=NEW.manifest_id; DELETE FROM download_manifest_states WHERE manifest_id=NEW.manifest_id AND fs_id=NEW.fs_id; END;
`

type DownloadManifest struct {
	ID             string     `json:"id"`
	RootID         int64      `json:"root_id,string"`
	RootPath       string     `json:"root_path"`
	AttemptID      string     `json:"attempt_id,omitempty"`
	StartedAtUTC   time.Time  `json:"started_at_utc"`
	PublishedAtUTC *time.Time `json:"published_at_utc,omitempty"`
}

type DownloadManifestEntry struct {
	FsID              int64     `json:"fs_id,string"`
	Path              string    `json:"path"`
	Name              string    `json:"name"`
	IsDir             int       `json:"is_dir"`
	Size              int64     `json:"size"`
	Mtime             int64     `json:"mtime"`
	CloudMD5          string    `json:"cloud_md5,omitempty"`
	RelativePath      string    `json:"relative_path"`
	ObservedAtUTC     time.Time `json:"observed_at_utc"`
	ObservationSource string    `json:"observation_source"`
}

func validDownloadManifestID(id string) bool {
	decoded, err := hex.DecodeString(id)
	return err == nil && len(decoded) == 16 && hex.EncodeToString(decoded) == id
}

func validManifestCloudPath(value string) bool {
	return strings.HasPrefix(value, "/") && value != "/" && path.Clean(value) == value && !strings.ContainsAny(value, "\x00\\")
}

func (s *Store) BeginDownloadManifest(ctx context.Context, m DownloadManifest) error {
	if !validDownloadManifestID(m.ID) || m.RootID <= 0 || !validManifestCloudPath(m.RootPath) || m.StartedAtUTC.IsZero() || m.PublishedAtUTC != nil || (m.AttemptID != "" && !validDownloadManifestID(m.AttemptID)) {
		return fmt.Errorf("invalid download manifest")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO download_manifests(manifest_id,root_id,root_path,attempt_id,started_at_utc) VALUES(?,?,?,?,?)`, m.ID, m.RootID, m.RootPath, m.AttemptID, m.StartedAtUTC.UTC().UnixMicro())
	return err
}

func (s *Store) AppendDownloadManifestEntries(ctx context.Context, id string, entries []DownloadManifestEntry) error {
	if !validDownloadManifestID(id) || len(entries) < 1 || len(entries) > DownloadManifestBatchLimit {
		return fmt.Errorf("invalid download manifest batch")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var rootPath string
	var published sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT root_path,published_at_utc FROM download_manifests WHERE manifest_id=?`, id).Scan(&rootPath, &published); err != nil {
		return err
	}
	if published.Valid {
		return fmt.Errorf("download manifest already published")
	}
	statement, err := tx.PrepareContext(ctx, `INSERT INTO download_manifest_entries(manifest_id,fs_id,path,parent_path,name,is_dir,size_bytes,provider_mtime,cloud_md5,relative_path,observed_at_utc,observation_source,search_path) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, e := range entries {
		suffix := strings.TrimPrefix(e.Path, rootPath)
		if e.FsID <= 0 || !validManifestCloudPath(e.Path) || (e.Path != rootPath && !strings.HasPrefix(e.Path, rootPath+"/")) || e.Name != path.Base(e.Path) || (e.IsDir != 0 && e.IsDir != 1) || e.Size < 0 || e.ObservedAtUTC.IsZero() || e.RelativePath != path.Base(rootPath)+suffix {
			return fmt.Errorf("invalid download manifest entry")
		}
		parent := path.Dir(e.Path)
		source := "directory_listing"
		if e.Path == rootPath {
			parent = ""
			source = "root_task_snapshot"
		}
		if _, err = statement.ExecContext(ctx, id, e.FsID, e.Path, parent, e.Name, e.IsDir, e.Size, e.Mtime, e.CloudMD5, e.RelativePath, e.ObservedAtUTC.UTC().UnixMicro(), source, strings.ToLower(e.Path)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Publishing changes the visible pointer in the same transaction as sealing the
// manifest. A failed or interrupted preparation leaves the old pointer intact.
func (s *Store) PublishDownloadManifest(ctx context.Context, id string, at time.Time) error {
	if !validDownloadManifestID(id) || at.IsZero() {
		return fmt.Errorf("invalid download manifest publication")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var rootID int64
	var rootPath string
	var published sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT root_id,root_path,published_at_utc FROM download_manifests WHERE manifest_id=?`, id).Scan(&rootID, &rootPath, &published); err != nil {
		return err
	}
	if published.Valid {
		return fmt.Errorf("download manifest already published")
	}
	var rootCount, orphanCount int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM download_manifest_entries WHERE manifest_id=? AND fs_id=? AND path=?`, id, rootID, rootPath).Scan(&rootCount); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM download_manifest_entries e WHERE e.manifest_id=? AND e.path<>? AND NOT EXISTS (SELECT 1 FROM download_manifest_entries p WHERE p.manifest_id=e.manifest_id AND p.path=e.parent_path AND p.is_dir=1)`, id, rootPath).Scan(&orphanCount); err != nil {
		return err
	}
	if rootCount != 1 || orphanCount != 0 {
		return fmt.Errorf("download manifest tree incomplete")
	}
	// Aggregate once while sealing. Polling the workspace reads this projection
	// by primary key; successful file evidence updates it in the same transaction.
	if _, err = tx.ExecContext(ctx, `INSERT INTO download_manifest_totals(manifest_id,files,folders,size_bytes) SELECT ?,COALESCE(SUM(CASE WHEN is_dir=0 THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN is_dir=1 THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN is_dir=0 THEN size_bytes ELSE 0 END),0) FROM download_manifest_entries WHERE manifest_id=?`, id, id); err != nil {
		return err
	}
	// Wall-clock adjustments must not prevent an otherwise valid publication;
	// both observed timestamps are retained without inventing elapsed duration.
	if _, err = tx.ExecContext(ctx, `UPDATE download_manifests SET published_at_utc=? WHERE manifest_id=?`, at.UTC().UnixMicro(), id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO download_manifest_current(root_id,manifest_id) VALUES(?,?) ON CONFLICT(root_id) DO UPDATE SET manifest_id=excluded.manifest_id`, rootID, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CurrentDownloadManifest(ctx context.Context, rootID int64) (*DownloadManifest, error) {
	if rootID <= 0 {
		return nil, fmt.Errorf("invalid download root")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	var m DownloadManifest
	var started, published int64
	err := s.db.QueryRowContext(ctx, `SELECT m.manifest_id,m.root_id,m.root_path,m.attempt_id,m.started_at_utc,m.published_at_utc FROM download_manifest_current c JOIN download_manifests m ON m.manifest_id=c.manifest_id WHERE c.root_id=?`, rootID).Scan(&m.ID, &m.RootID, &m.RootPath, &m.AttemptID, &started, &published)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.StartedAtUTC = time.UnixMicro(started).UTC()
	v := time.UnixMicro(published).UTC()
	m.PublishedAtUTC = &v
	return &m, nil
}

func (s *Store) DownloadManifestEntries(ctx context.Context, id string, offset, limit int) ([]DownloadManifestEntry, error) {
	if !validDownloadManifestID(id) || offset < 0 || limit < 1 || limit > 200 {
		return nil, fmt.Errorf("invalid download manifest page")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	rows, err := s.db.QueryContext(ctx, `SELECT fs_id,path,name,is_dir,size_bytes,provider_mtime,cloud_md5,relative_path,observed_at_utc,observation_source FROM download_manifest_entries WHERE manifest_id=? ORDER BY path,fs_id LIMIT ? OFFSET ?`, id, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]DownloadManifestEntry, 0, limit)
	for rows.Next() {
		var e DownloadManifestEntry
		var observed int64
		if err = rows.Scan(&e.FsID, &e.Path, &e.Name, &e.IsDir, &e.Size, &e.Mtime, &e.CloudMD5, &e.RelativePath, &observed, &e.ObservationSource); err != nil {
			return nil, err
		}
		e.ObservedAtUTC = time.UnixMicro(observed).UTC()
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func validateDownloadManifestSchema(ctx context.Context, db *sql.DB) error {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE (type='table' AND name IN ('download_manifests','download_manifest_entries','download_manifest_current','download_manifest_results','download_manifest_totals','download_manifest_states')) OR (type='index' AND name='download_manifests_root_order') OR (type='trigger' AND name IN ('download_manifest_results_no_update','download_manifest_results_no_delete','download_manifest_results_totals'))`).Scan(&count); err != nil {
		return err
	}
	if count != 10 {
		return fmt.Errorf("SQLite download manifest schema incomplete")
	}
	rows, err := db.QueryContext(ctx, `SELECT m.manifest_id,m.root_id,m.root_path,m.attempt_id,m.started_at_utc,m.published_at_utc,e.fs_id,e.path,e.parent_path,e.name,e.is_dir,e.size_bytes,e.provider_mtime,e.cloud_md5,e.relative_path,e.observed_at_utc,e.observation_source,e.search_path,c.root_id,c.manifest_id,r.kind,r.sha256,r.recorded_at_utc,t.files,t.folders,t.size_bytes,t.completed,t.completed_bytes,st.status,st.error_summary,st.run_id,st.recorded_at_utc FROM download_manifests m JOIN download_manifest_entries e ON e.manifest_id=m.manifest_id JOIN download_manifest_current c ON c.manifest_id=m.manifest_id LEFT JOIN download_manifest_results r ON r.manifest_id=e.manifest_id AND r.fs_id=e.fs_id JOIN download_manifest_totals t ON t.manifest_id=m.manifest_id LEFT JOIN download_manifest_states st ON st.manifest_id=e.manifest_id AND st.fs_id=e.fs_id LIMIT 0`)
	if err != nil {
		return err
	}
	return rows.Close()
}
