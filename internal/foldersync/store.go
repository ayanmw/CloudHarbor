package foldersync

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Config struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Mode       string    `json:"mode"`
	LocalRoot  string    `json:"local_root"`
	RemoteRoot string    `json:"remote_root"`
	LocalID    string    `json:"local_root_id"`
	RemoteID   string    `json:"remote_root_id"`
	Account    string    `json:"account_scope"`
	Created    time.Time `json:"created_at_utc"`
}
type Run struct {
	ID        string     `json:"id"`
	ConfigID  string     `json:"config_id"`
	Status    string     `json:"status"`
	Started   time.Time  `json:"started_at_utc"`
	Finished  *time.Time `json:"finished_at_utc"`
	Error     string     `json:"error"`
	StepCount int        `json:"step_count"`
	Confirmed int        `json:"confirmed"`
}
type StepRecord struct {
	Ordinal  int             `json:"ordinal"`
	Step     Step            `json:"plan"`
	Status   string          `json:"status"`
	Recorded time.Time       `json:"recorded_at_utc"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    string          `json:"error"`
}
type Event struct {
	Sequence string          `json:"sequence"`
	Kind     string          `json:"kind"`
	Recorded time.Time       `json:"recorded_at_utc"`
	Evidence json.RawMessage `json:"evidence"`
}
type Remote interface {
	Inspect(context.Context, string) (account, rootID string, err error)
	Scan(context.Context, Config, []Entry) ([]Entry, error)
	Apply(context.Context, Config, Step, string, func(string, any) error) (any, error)
	Download(context.Context, Config, Entry, string) error
}
type Manager struct {
	db               *sql.DB
	remote           Remote
	forbidden        []string
	resolveForbidden func() []string
	ctx              context.Context
	cancel           context.CancelFunc
	mu               sync.Mutex
	active           bool
	closed           bool
	wg               sync.WaitGroup
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func encoded(v any) string { b, _ := json.Marshal(v); return string(b) }
func Open(dir string, remote Remote, forbidden []string, resolver ...func() []string) (*Manager, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "sync.sqlite"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{db: db, remote: remote, forbidden: forbidden, ctx: ctx, cancel: cancel}
	if len(resolver) > 0 {
		m.resolveForbidden = resolver[0]
	}
	_, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS sync_configs(id TEXT PRIMARY KEY, body TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sync_baselines(config_id TEXT PRIMARY KEY, run_id TEXT NOT NULL, body TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sync_runs(id TEXT PRIMARY KEY,config_id TEXT NOT NULL,status TEXT NOT NULL,body TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sync_steps(run_id TEXT NOT NULL,ordinal INTEGER NOT NULL,status TEXT NOT NULL,body TEXT NOT NULL,PRIMARY KEY(run_id,ordinal));
CREATE TABLE IF NOT EXISTS sync_entries(config_id TEXT NOT NULL,side TEXT NOT NULL,path TEXT NOT NULL,run_id TEXT NOT NULL,body TEXT NOT NULL,PRIMARY KEY(config_id,side,path));
CREATE TABLE IF NOT EXISTS sync_events(sequence INTEGER PRIMARY KEY AUTOINCREMENT,run_id TEXT NOT NULL,kind TEXT NOT NULL,recorded TEXT NOT NULL,evidence TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS sync_events_run ON sync_events(run_id,sequence);
CREATE TRIGGER IF NOT EXISTS sync_event_no_update BEFORE UPDATE ON sync_events BEGIN SELECT RAISE(ABORT,'sync evidence is immutable'); END;
CREATE TRIGGER IF NOT EXISTS sync_event_no_delete BEFORE DELETE ON sync_events BEGIN SELECT RAISE(ABORT,'sync evidence is immutable'); END;`)
	if err != nil {
		db.Close()
		cancel()
		return nil, err
	}
	if err = m.initAnalyses(); err != nil {
		db.Close()
		cancel()
		return nil, err
	}
	if err = m.initResolutions(); err != nil {
		db.Close()
		cancel()
		return nil, err
	}
	if err = m.initResolutionChecks(); err != nil {
		db.Close()
		cancel()
		return nil, err
	}
	if err = m.initResolutionExecutions(); err != nil {
		db.Close()
		cancel()
		return nil, err
	}
	if err = m.initResolutionVerifications(); err != nil {
		db.Close()
		cancel()
		return nil, err
	}
	if err = m.initSubtreePlans(); err != nil {
		db.Close()
		cancel()
		return nil, err
	}
	if err = m.initResolutionBatches(); err != nil {
		db.Close()
		cancel()
		return nil, err
	}
	if err = m.initBatchChecks(); err != nil {
		db.Close()
		cancel()
		return nil, err
	}
	if err = m.initBatchExecutions(); err != nil {
		db.Close()
		cancel()
		return nil, err
	}
	if err = m.initBatchVerifications(); err != nil {
		db.Close()
		cancel()
		return nil, err
	}
	// Recover by recording uncertainty, never by replaying the last operation.
	rows, err := db.Query(`SELECT body FROM sync_runs WHERE status='running'`)
	if err != nil {
		m.Close()
		return nil, err
	}
	var interrupted []Run
	for rows.Next() {
		var body string
		var r Run
		if err = rows.Scan(&body); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(body), &r); err != nil {
			break
		}
		interrupted = append(interrupted, r)
	}
	if e := rows.Err(); err == nil {
		err = e
	}
	rows.Close()
	if err != nil {
		m.Close()
		return nil, err
	}
	for _, r := range interrupted {
		steps, e := m.Steps(r.ID)
		if e != nil {
			m.Close()
			return nil, e
		}
		for _, s := range steps {
			if s.Status == "intent" {
				s.Status = "unknown"
				s.Error = "进程中断，结果待核对，禁止盲目重发"
				if e = m.saveStep(r.ID, s); e != nil {
					m.Close()
					return nil, e
				}
			}
		}
		r.Status = "interrupted"
		r.Error = "进程中断；先核对已准入操作"
		if e = m.finish(r); e != nil {
			m.Close()
			return nil, e
		}
	}
	return m, nil
}
func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
	return m.db.Close()
}
func (m *Manager) event(run, kind string, evidence any) error {
	_, err := m.db.Exec(`INSERT INTO sync_events(run_id,kind,recorded,evidence) VALUES(?,?,?,?)`, run, kind, time.Now().UTC().Format(time.RFC3339Nano), encoded(evidence))
	return err
}
func (m *Manager) config(id string) (Config, error) {
	var c Config
	var body string
	err := m.db.QueryRow(`SELECT body FROM sync_configs WHERE id=?`, id).Scan(&body)
	if err == nil {
		err = json.Unmarshal([]byte(body), &c)
	}
	return c, err
}
func (m *Manager) Configs() ([]Config, error) {
	result := []Config{}
	rows, err := m.db.Query(`SELECT body FROM sync_configs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var body string
		var c Config
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(body), &c); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}
func (m *Manager) Create(ctx context.Context, c Config) (Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.active {
		return c, fmt.Errorf("同步执行中，暂不能修改配置")
	}
	if c.Mode != "pull" && c.Mode != "both" || c.Name == "" || len(c.Name) > 120 || !filepath.IsAbs(c.LocalRoot) {
		return c, fmt.Errorf("同步配置无效")
	}
	absolute, err := filepath.Abs(c.LocalRoot)
	if err != nil {
		return c, err
	}
	c.LocalRoot = filepath.Clean(absolute)
	for _, p := range m.exclusions() {
		if overlap(c.LocalRoot, p) {
			return c, fmt.Errorf("同步目录不能与账号状态或下载目录重叠")
		}
	}
	id, err := rootIdentity(c.LocalRoot)
	if err != nil {
		return c, err
	}
	if _, err = os.Lstat(filepath.Join(c.LocalRoot, privateDirectory)); !os.IsNotExist(err) {
		return c, fmt.Errorf("目录已有保留名称 .nas-sync-history，请先选择其他目录")
	}
	configs, err := m.Configs()
	if err != nil {
		return c, err
	}
	for _, old := range configs {
		if overlap(c.LocalRoot, old.LocalRoot) || under(c.RemoteRoot, old.RemoteRoot) || under(old.RemoteRoot, c.RemoteRoot) {
			return c, fmt.Errorf("同步目录不能与已有配置重叠")
		}
	}
	account, remoteID, err := m.remote.Inspect(ctx, c.RemoteRoot)
	if err != nil {
		return c, err
	}
	c.ID = newID()
	c.LocalID = id
	c.RemoteID = remoteID
	c.Account = account
	c.Created = time.Now().UTC()
	tx, err := m.db.Begin()
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO sync_configs VALUES(?,?)`, c.ID, encoded(c)); err != nil {
		return c, err
	}
	if _, err = tx.Exec(`INSERT INTO sync_events(run_id,kind,recorded,evidence) VALUES(?,?,?,?)`, c.ID, "config_created", c.Created.Format(time.RFC3339Nano), encoded(c)); err != nil {
		return c, err
	}
	return c, tx.Commit()
}
func (m *Manager) Runs(config string) ([]Run, error) {
	result := []Run{}
	rows, err := m.db.Query(`SELECT body FROM sync_runs WHERE config_id=? ORDER BY rowid DESC LIMIT 100`, config)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var body string
		var r Run
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func (m *Manager) runPage(config, before string) ([]Run, bool, string, error) {
	args := []any{config}
	query := `SELECT body FROM sync_runs WHERE config_id=?`
	if before != "" {
		var rowID int64
		if err := m.db.QueryRow(`SELECT rowid FROM sync_runs WHERE config_id=? AND id=?`, config, before).Scan(&rowID); err != nil {
			return nil, false, "", err
		}
		query += ` AND rowid<?`
		args = append(args, rowID)
	}
	query += ` ORDER BY rowid DESC LIMIT 101`
	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, false, "", err
	}
	defer rows.Close()
	result := []Run{}
	for rows.Next() {
		var body string
		var r Run
		if err = rows.Scan(&body); err != nil {
			return nil, false, "", err
		}
		if err = json.Unmarshal([]byte(body), &r); err != nil {
			return nil, false, "", err
		}
		result = append(result, r)
	}
	if err = rows.Err(); err != nil {
		return nil, false, "", err
	}
	more := len(result) > 100
	next := ""
	if more {
		result = result[:100]
		next = result[99].ID
	}
	return result, more, next, nil
}

func (m *Manager) displayEvents(run string) ([]Event, bool, error) {
	rows, err := m.db.Query(`SELECT sequence,kind,recorded,CASE WHEN kind='detected_changes' THEN json_object('change_items',json_array_length(evidence),'full_details_in_export',json('true')) WHEN kind IN ('snapshot_before','snapshot_after') THEN json_object('local_items',json_array_length(evidence,'$.local'),'remote_items',json_array_length(evidence,'$.remote'),'full_snapshot_in_export',json('true')) ELSE evidence END FROM sync_events WHERE run_id=? ORDER BY sequence DESC LIMIT 201`, run)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	result := []Event{}
	for rows.Next() {
		var e Event
		var at, raw string
		if err = rows.Scan(&e.Sequence, &e.Kind, &at, &raw); err != nil {
			return nil, false, err
		}
		e.Recorded, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, false, err
		}
		e.Evidence = json.RawMessage(raw)
		result = append(result, e)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(result) > 200
	if more {
		result = result[:200]
	}
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, more, nil
}
func (m *Manager) Steps(run string) ([]StepRecord, error) {
	result := []StepRecord{}
	rows, err := m.db.Query(`SELECT body FROM sync_steps WHERE run_id=? ORDER BY ordinal`, run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var body string
		var r StepRecord
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
func (m *Manager) saveStep(run string, s StepRecord) error {
	s.Recorded = time.Now().UTC()
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO sync_steps VALUES(?,?,?,?) ON CONFLICT(run_id,ordinal) DO UPDATE SET status=excluded.status,body=excluded.body`, run, s.Ordinal, s.Status, encoded(s)); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO sync_events(run_id,kind,recorded,evidence) VALUES(?,?,?,?)`, run, "step_"+s.Status, s.Recorded.Format(time.RFC3339Nano), encoded(s)); err != nil {
		return err
	}
	return tx.Commit()
}
func (m *Manager) finish(r Run) error {
	now := time.Now().UTC()
	r.Finished = &now
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE sync_runs SET status=?,body=? WHERE id=?`, r.Status, encoded(r), r.ID); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO sync_events(run_id,kind,recorded,evidence) VALUES(?,?,?,?)`, r.ID, "run_"+r.Status, now.Format(time.RFC3339Nano), encoded(r)); err != nil {
		return err
	}
	return tx.Commit()
}

func (m *Manager) progress(r Run) error {
	result, err := m.db.Exec(`UPDATE sync_runs SET body=? WHERE id=? AND status='running'`, encoded(r), r.ID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("同步运行身份或状态变化")
	}
	return nil
}
func (m *Manager) baseline(c string) (Snapshot, error) {
	var result Snapshot
	var body string
	err := m.db.QueryRow(`SELECT body FROM sync_baselines WHERE config_id=?`, c).Scan(&body)
	if err == sql.ErrNoRows {
		return result, nil
	}
	if err == nil {
		err = json.Unmarshal([]byte(body), &result)
	}
	return result, err
}
func (m *Manager) cache(c, run string, s Snapshot) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM sync_entries WHERE config_id=?`, c); err != nil {
		return err
	}
	for side, entries := range map[string][]Entry{"local": s.Local, "remote": s.Remote} {
		for _, e := range entries {
			if _, err = tx.Exec(`INSERT INTO sync_entries VALUES(?,?,?,?,?)`, c, side, e.Path, run, encoded(e)); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
func (m *Manager) Events(run string) ([]Event, error) {
	result := []Event{}
	rows, err := m.db.Query(`SELECT sequence,kind,recorded,evidence FROM sync_events WHERE run_id=? ORDER BY sequence`, run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e Event
		var raw, at string
		if err := rows.Scan(&e.Sequence, &e.Kind, &at, &raw); err != nil {
			return nil, err
		}
		e.Recorded, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, err
		}
		e.Evidence = json.RawMessage(raw)
		result = append(result, e)
	}
	return result, rows.Err()
}
