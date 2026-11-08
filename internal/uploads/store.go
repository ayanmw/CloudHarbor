// Package uploads owns private staging and durable upload batches. It is opened
// only after the application's existing account-directory writer lock is held.
package uploads

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	_ "modernc.org/sqlite"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

type Input struct {
	Path string `json:"path"`
	Size string `json:"size"`
}
type Item struct {
	PauseRequested bool   `json:"pause_requested,omitempty"`
	ID             string `json:"id"`
	Path           string `json:"path"`
	Size           string `json:"size"`
	State          string `json:"state"`
	Sent           string `json:"sent"`
	Updated        string `json:"updated_at_utc"`
	Error          string `json:"error"`
	ActualPath     string `json:"actual_path"`
}
type Batch struct {
	DirectorySummary *DirectorySummary `json:"directory_summary,omitempty"`
	Directories      []Directory       `json:"directories,omitempty"`
	ID               string            `json:"id"`
	Target           string            `json:"target"`
	Created          string            `json:"created_at_utc"`
	Items            []Item            `json:"items"`
	Total            int               `json:"total"`
	Completed        int               `json:"completed"`
	Failed           int               `json:"failed"`
	Unknown          int               `json:"unknown"`
}
type Runner func(context.Context, string, string, func(string, int64) error, func(baiduapi.UploadTransferSample) error) (baiduapi.UploadReceipt, error)
type Manager struct {
	admissionError     string
	directoryExecuting bool
	concurrency        int
	concurrencyUpdated string
	runDirectory       DirectoryRunner
	verifyReceipt      ReceiptVerifier
	verifyingReceipts  map[string]bool
	pauseMu            sync.RWMutex
	pauseRequests      map[string]bool
	activeAttempts     map[string]uploadAttemptSnapshot
	db                 *sql.DB
	dir                string
	run                Runner
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup
	mu                 sync.Mutex
	receiving          map[string]bool
	ready              func() bool
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func id() (string, error) {
	var b [16]byte
	_, err := rand.Read(b[:])
	return hex.EncodeToString(b[:]), err
}
func validID(s string) bool {
	raw, err := hex.DecodeString(s)
	return err == nil && len(raw) == 16 && strings.ToLower(s) == s
}
func validPath(s string) bool {
	return strings.HasPrefix(s, "/") && path.Clean(s) == s && !strings.ContainsAny(s, "\\\x00")
}
func Open(dir string, run Runner, readiness ...func() bool) (*Manager, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "uploads.sqlite"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{db: db, dir: dir, run: run, ctx: ctx, cancel: cancel, receiving: map[string]bool{}}
	if len(readiness) > 0 {
		m.ready = readiness[0]
	}
	_, err = db.Exec(`PRAGMA journal_mode=WAL;PRAGMA synchronous=FULL;PRAGMA busy_timeout=5000;
 CREATE TABLE IF NOT EXISTS batches(id TEXT PRIMARY KEY,target TEXT NOT NULL,created TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS items(id TEXT PRIMARY KEY,batch TEXT NOT NULL,relative_path TEXT NOT NULL,size INTEGER NOT NULL,state TEXT NOT NULL,sent INTEGER NOT NULL DEFAULT 0,updated TEXT NOT NULL,error TEXT NOT NULL DEFAULT '',actual_path TEXT NOT NULL DEFAULT '');
 CREATE INDEX IF NOT EXISTS upload_items_batch ON items(batch,id);
 CREATE TABLE IF NOT EXISTS events(sequence INTEGER PRIMARY KEY AUTOINCREMENT,item TEXT NOT NULL,state TEXT NOT NULL,sent INTEGER NOT NULL,recorded TEXT NOT NULL,error TEXT NOT NULL);`)
	if err == nil {
		err = m.initConcurrency()
	}
	if err == nil {
		err = m.initDirectoryStore()
	}
	if err == nil {
		err = recoverInterruptedUploads(db)
	}
	if err == nil {
		err = m.loadPauseRequests()
	}
	if err != nil {
		db.Close()
		cancel()
		return nil, err
	}
	// A synced/renamed staging file may precede the queued transaction at a crash.
	rows, err := db.Query(`SELECT id,size FROM items WHERE state='receiving'`)
	if err != nil {
		db.Close()
		cancel()
		return nil, err
	}
	type pending struct {
		id   string
		size int64
	}
	var recoverable []pending
	for rows.Next() {
		var v pending
		if err = rows.Scan(&v.id, &v.size); err != nil {
			break
		}
		recoverable = append(recoverable, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		db.Close()
		cancel()
		return nil, err
	}
	for _, v := range recoverable {
		if stat, e := os.Stat(filepath.Join(dir, v.id+".data")); e == nil && stat.Mode().IsRegular() && stat.Size() == v.size {
			if err = m.update(v.id, "queued", 0, ""); err != nil {
				db.Close()
				cancel()
				return nil, err
			}
		}
	}
	// No receiver/worker exists yet: reclaim only this manager's terminal payloads
	// and interrupted UUID .part files. History stays in SQLite.
	if err = m.reclaimStartup(); err != nil {
		db.Close()
		cancel()
		return nil, err
	}
	if run != nil {
		m.wg.Add(2)
		go m.worker()
		go m.worker()
	}
	return m, nil
}
func (m *Manager) Close() error {
	// Serialize shutdown with receipt checks joining the lifetime wait group.
	m.mu.Lock()
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
	return m.db.Close()
}
func (m *Manager) Create(target string, inputs []Input) (Batch, error) {
	return m.createManifest(target, inputs, nil)
}

// Directory manifests remain internal until namespace execution and recovery are wired.
func (m *Manager) createManifest(target string, inputs []Input, directories []string) (Batch, error) {
	var b Batch
	normalizedDirectories, validationErr := manifestDirectories(inputs, directories)
	if validationErr != nil {
		return b, validationErr
	}
	if !validPath(target) {
		return b, errors.New("目标目录或上传清单无效（每批1至10000文件）")
	}
	seen := map[string]bool{}
	for _, f := range inputs {
		size, e := strconv.ParseInt(f.Size, 10, 64)
		if e != nil || size < 0 || strconv.FormatInt(size, 10) != f.Size || size > int64(baiduapi.UploadBlockSize)*10000 || f.Path == "" || strings.HasPrefix(f.Path, "/") || path.Clean(f.Path) != f.Path || f.Path == "." || f.Path == ".." || strings.HasPrefix(f.Path, "../") || strings.ContainsAny(f.Path, "\\\x00") || seen[f.Path] {
			return b, errors.New("文件路径、大小或重复目标无效")
		}
		for _, part := range strings.Split(f.Path, "/") {
			if len([]byte(part)) > 255 || strings.IndexFunc(part, func(r rune) bool { return r < 32 || r == 127 }) >= 0 {
				return b, errors.New("文件名称无效")
			}
		}
		seen[f.Path] = true
	}
	for relative := range seen {
		for parent := path.Dir(relative); parent != "."; parent = path.Dir(parent) {
			if seen[parent] {
				return b, errors.New("同批次文件与目录路径冲突")
			}
		}
	}
	var err error
	b.ID, err = id()
	if err != nil {
		return b, err
	}
	b.Target = target
	b.Created = now()
	b.Items = []Item{}
	tx, err := m.db.Begin()
	if err != nil {
		return b, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO batches VALUES(?,?,?)`, b.ID, target, b.Created); err != nil {
		return b, err
	}
	for _, f := range inputs {
		itemID, e := id()
		if e != nil {
			return b, e
		}
		if _, err = tx.Exec(`INSERT INTO items(id,batch,relative_path,size,state,updated) VALUES(?,?,?,?,'receiving',?)`, itemID, b.ID, f.Path, f.Size, b.Created); err != nil {
			return b, err
		}
		b.Items = append(b.Items, Item{ID: itemID, Path: f.Path, Size: f.Size, State: "receiving", Sent: "0", Updated: b.Created})
	}
	for _, relative := range normalizedDirectories {
		directoryID, e := id()
		if e != nil {
			return b, e
		}
		if _, err = tx.Exec(`INSERT INTO upload_directories(id,batch,relative_path,state,updated) VALUES(?,?,?,'queued',?)`, directoryID, b.ID, relative, b.Created); err != nil {
			return b, err
		}
		if _, err = tx.Exec(`INSERT INTO upload_directory_events(directory,state,recorded) VALUES(?,'queued',?)`, directoryID, b.Created); err != nil {
			return b, err
		}
		b.Directories = append(b.Directories, Directory{ID: directoryID, Path: relative, State: "queued", Updated: b.Created})
	}
	b.DirectorySummary = summarizeDirectories(b.Directories)
	return b, tx.Commit()
}
func (m *Manager) Get(batch string) (Batch, error) {
	var b Batch
	if !validID(batch) {
		return b, errors.New("批次ID无效")
	}
	if err := m.db.QueryRow(`SELECT id,target,created FROM batches WHERE id=?`, batch).Scan(&b.ID, &b.Target, &b.Created); err != nil {
		return b, err
	}
	rows, err := m.db.Query(`SELECT id,relative_path,CAST(size AS TEXT),state,CAST(sent AS TEXT),updated,error,actual_path FROM items WHERE batch=? ORDER BY id`, batch)
	if err != nil {
		return b, err
	}
	defer rows.Close()
	b.Items = []Item{}
	for rows.Next() {
		var item Item
		if err = rows.Scan(&item.ID, &item.Path, &item.Size, &item.State, &item.Sent, &item.Updated, &item.Error, &item.ActualPath); err != nil {
			return b, err
		}
		item.PauseRequested = pauseApplicable(item.State) && m.isPauseRequested(item.ID)
		b.Items = append(b.Items, item)
	}
	if err = rows.Err(); err != nil {
		return b, err
	}
	if err = rows.Close(); err != nil {
		return b, err
	}
	b.Directories, err = m.directories(batch)
	b.DirectorySummary = summarizeDirectories(b.Directories)
	return b, err
}
func (m *Manager) List() ([]Batch, error) {
	rows, err := m.db.Query(`SELECT b.id,b.target,b.created,COUNT(i.id),COALESCE(SUM(i.state='confirmed'),0),COALESCE(SUM(i.state='failed'),0),COALESCE(SUM(i.state='unknown'),0) FROM batches b LEFT JOIN items i ON i.batch=b.id GROUP BY b.id ORDER BY b.created DESC,b.id DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Batch{}
	for rows.Next() {
		var b Batch
		if err = rows.Scan(&b.ID, &b.Target, &b.Created, &b.Total, &b.Completed, &b.Failed, &b.Unknown); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	for index := range out {
		out[index].DirectorySummary, err = m.directorySummary(out[index].ID)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (m *Manager) update(item, state string, sent int64, message string) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := now()
	if _, err = tx.Exec(`UPDATE items SET state=?,sent=?,updated=?,error=? WHERE id=?`, state, sent, at, message, item); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO events(item,state,sent,recorded,error) VALUES(?,?,?,?,?)`, item, state, sent, at, message); err != nil {
		return err
	}
	return tx.Commit()
}
func (m *Manager) Receive(ctx context.Context, batch, item string, reader io.Reader) error {
	if !validID(batch) || !validID(item) {
		return errors.New("上传ID无效")
	}
	m.mu.Lock()
	if m.receiving[item] {
		m.mu.Unlock()
		return errors.New("该文件正在接收")
	}
	m.receiving[item] = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.receiving, item); m.mu.Unlock() }()
	var size int64
	var state string
	if err := m.db.QueryRow(`SELECT size,state FROM items WHERE batch=? AND id=?`, batch, item).Scan(&size, &state); err != nil {
		return err
	}
	if state != "receiving" {
		return errors.New("该文件已接收或已停止，请刷新批次")
	}
	part := filepath.Join(m.dir, item+".part")
	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(part)
	n, copyErr := io.Copy(f, io.LimitReader(reader, size+1))
	if copyErr == nil {
		copyErr = ctx.Err()
	}
	if copyErr == nil && n != size {
		copyErr = errors.New("接收字节与文件清单不一致，请重新选择文件")
	}
	if copyErr == nil {
		copyErr = f.Sync()
	}
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(part, filepath.Join(m.dir, item+".data")); err != nil {
		return err
	}
	return m.update(item, "queued", 0, "")
}
func (m *Manager) Cancel(batch string) error {
	if !validID(batch) {
		return errors.New("批次ID无效")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for item := range m.receiving {
		var b string
		m.db.QueryRow(`SELECT batch FROM items WHERE id=?`, item).Scan(&b)
		if b == batch {
			return errors.New("仍在接收文件，请先停止浏览器传输")
		}
	}
	err := m.cancelPendingBatch(batch)
	if err != nil {
		return err
	}
	b, err := m.Get(batch)
	if err != nil {
		return err
	}
	for _, item := range b.Items {
		if item.State == "cancelled" {
			m.setPauseRequested(item.ID, false)
			if e := os.Remove(filepath.Join(m.dir, item.ID+".data")); e != nil && !os.IsNotExist(e) {
				return errors.New("文件已停止，但暂存清理失败；请稍后再次停止以重试清理")
			}
		}
	}
	return nil
}

// Retry only definite failures with retained local data; unknown commits remain isolated.
func (m *Manager) Retry(batch string) error {
	b, err := m.Get(batch)
	if err != nil {
		return err
	}
	if err = m.retryFailedDirectories(batch); err != nil {
		return err
	}
	for _, item := range b.Items {
		if item.State != "failed" {
			continue
		}
		// Explicit retry starts a fresh attempt and atomically retires any pause
		// intent from the failed attempt, so restart behavior matches live behavior.
		if e := m.queueRetainedUpload(batch, item.ID, "failed"); e != nil {
			return e
		}
	}
	return nil
}

func (m *Manager) reclaimStartup() error {
	rows, err := m.db.Query(`SELECT id,state,sent FROM items WHERE state IN ('cancelled','confirmed')`)
	if err != nil {
		return err
	}
	type terminal struct {
		id, state string
		sent      int64
	}
	var items []terminal
	for rows.Next() {
		var i terminal
		if err = rows.Scan(&i.id, &i.state, &i.sent); err != nil {
			break
		}
		items = append(items, i)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range items {
		if !validID(item.id) {
			return errors.New("上传暂存身份无效")
		}
		if e := os.Remove(filepath.Join(m.dir, item.id+".data")); e != nil && !os.IsNotExist(e) {
			if err = m.update(item.id, item.state, item.sent, "上传暂存回收失败，请关闭占用程序后重试停止"); err != nil {
				return err
			}
		}
	}
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".part") && validID(strings.TrimSuffix(entry.Name(), ".part")) {
			if e := os.Remove(filepath.Join(m.dir, entry.Name())); e != nil {
				return errors.New("中断上传暂存仍被占用，请关闭占用程序后重启")
			}
		}
	}
	return nil
}
