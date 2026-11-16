// Package library exposes one explicitly configured, read-only local file library.
// It has no Baidu credentials or provider dependency.
package library

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrUnsafe       = errors.New("文件库路径不允许访问")
	ErrUnavailable  = errors.New("文件库目录离线、不可读或身份已变化，请核对原目录")
	ErrConflict     = errors.New("文件库配置已变化，请刷新后重试")
	ErrBusy         = errors.New("文件库正在读取或下载，请结束后再修改配置")
	ErrUnconfigured = errors.New("尚未配置本站文件库")
	ErrChanged      = errors.New("文件或目录在读取期间变化，请刷新后重试")
)

type Config struct {
	Root     string `json:"root"`
	Revision string `json:"revision"`
	SavedAt  string `json:"saved_at_utc"`
	RootID   string `json:"root_identity"`
}
type Entry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	ID         string `json:"id"`
	Directory  bool   `json:"directory"`
	Size       int64  `json:"size,string"`
	ModifiedAt string `json:"modified_at_utc"`
	Revision   string `json:"revision"` // Metadata observation, not a content checksum.
}
type Listing struct {
	Path         string  `json:"path"`
	Entries      []Entry `json:"entries"`
	Total        int     `json:"total"`
	Offset       int     `json:"offset"`
	Limit        int     `json:"limit"`
	Excluded     int     `json:"excluded"`
	ObservedAt   string  `json:"observed_at_utc"`
	ErrorAt      string  `json:"error_at_utc,omitempty"`
	Error        string  `json:"error,omitempty"`
	Stale        bool    `json:"stale"`
	RootRevision string  `json:"root_revision"`
}
type Manager struct {
	mu        sync.RWMutex
	refreshMu sync.Mutex
	db        *sql.DB
	stateDir  string
	forbidden func() []string
	closed    bool
}

func Open(stateDir string, forbidden func() []string) (*Manager, error) {
	abs, err := filepath.Abs(stateDir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(abs, 0700); err != nil {
		return nil, err
	}
	if err = checkAbsoluteComponents(abs); err != nil {
		return nil, err
	}
	db, err := openStore(abs)
	if err != nil {
		return nil, err
	}
	m := &Manager{db: db, stateDir: abs, forbidden: forbidden}
	if _, err = m.config(); err != nil {
		db.Close()
		return nil, err
	}
	return m, nil
}
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	return m.db.Close()
}
func (m *Manager) Config() (Config, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return Config{}, os.ErrClosed
	}
	return m.config()
}
func (m *Manager) Configure(root, expected string) (Config, error) {
	if !m.mu.TryLock() {
		return Config{}, ErrBusy
	}
	defer m.mu.Unlock()
	if m.closed {
		return Config{}, os.ErrClosed
	}
	current, err := m.config()
	if err != nil {
		return Config{}, err
	}
	if current.Revision != expected {
		return Config{}, ErrConflict
	}
	if !filepath.IsAbs(root) {
		return Config{}, ErrUnsafe
	}
	root = filepath.Clean(root)
	if err = m.checkProtected(root); err != nil {
		return Config{}, err
	}
	if err = checkAbsoluteComponents(root); err != nil {
		return Config{}, err
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return Config{}, ErrUnavailable
	}
	defer r.Close()
	f, err := r.Open(".")
	if err != nil {
		return Config{}, ErrUnavailable
	}
	defer f.Close()
	id, err := identity(f)
	if err != nil {
		return Config{}, err
	}
	var nonce [32]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return Config{}, err
	}
	c := Config{Root: root, RootID: id, Revision: hex.EncodeToString(nonce[:]), SavedAt: utcNow()}
	if err = m.saveConfig(c); err != nil {
		return Config{}, err
	}
	return c, nil
}
func utcNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func (m *Manager) checkProtected(root string) error {
	paths := []string{m.stateDir}
	if m.forbidden != nil {
		paths = append(paths, m.forbidden()...)
	}
	for _, p := range paths {
		if p != "" && overlap(root, p) {
			return ErrUnsafe
		}
	}
	for _, part := range strings.Split(filepath.ToSlash(root), "/") {
		if privateName(part) {
			return ErrUnsafe
		}
	}
	return nil
}
func (m *Manager) openRoot(c Config) (*os.Root, error) {
	if c.Root == "" {
		return nil, ErrUnconfigured
	}
	if err := m.checkProtected(c.Root); err != nil {
		return nil, err
	}
	if err := checkAbsoluteComponents(c.Root); err != nil {
		return nil, ErrUnavailable
	}
	r, err := os.OpenRoot(c.Root)
	if err != nil {
		return nil, ErrUnavailable
	}
	f, err := r.Open(".")
	if err != nil {
		r.Close()
		return nil, ErrUnavailable
	}
	id, err := identity(f)
	f.Close()
	if err != nil || id != c.RootID {
		r.Close()
		return nil, ErrUnavailable
	}
	return r, nil
}
func (m *Manager) List(ctx context.Context, p, query string, offset, limit int) (Listing, error) {
	// Serialize observations so an older scan cannot overwrite a newer snapshot
	// merely because its database write was scheduled later.
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	if err := ctx.Err(); err != nil {
		return Listing{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return Listing{}, os.ErrClosed
	}
	if err := validRelative(p); err != nil {
		return Listing{}, err
	}
	if offset < 0 || limit < 1 || limit > 200 || len(query) > 256 {
		return Listing{}, ErrUnsafe
	}
	c, err := m.config()
	if err != nil {
		return Listing{}, err
	}
	l, err := m.scan(ctx, c, p)
	if err == nil {
		err = m.saveObservation(c, p, l, "")
	}
	if err != nil {
		previous, readErr := m.observation(c, p)
		if readErr != nil {
			return Listing{}, fmt.Errorf("文件库观察读取失败: %w", readErr)
		}
		previous.Stale = true
		previous.Error = err.Error()
		previous.ErrorAt = utcNow()
		previous.RootRevision = c.Revision
		previous.Path = p
		if saveErr := m.saveFailure(c, p, previous.ErrorAt, previous.Error); saveErr != nil {
			return Listing{}, fmt.Errorf("文件库失败证据保存失败: %w", saveErr)
		}
		return page(previous, query, offset, limit), err
	}
	return page(l, query, offset, limit), nil
}
func page(l Listing, query string, offset, limit int) Listing {
	entries := make([]Entry, 0, len(l.Entries))
	q := strings.ToLower(query)
	for _, e := range l.Entries {
		if strings.Contains(strings.ToLower(e.Name), q) {
			entries = append(entries, e)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Directory != b.Directory {
			return a.Directory
		}
		if strings.ToLower(a.Name) == strings.ToLower(b.Name) {
			return a.Name < b.Name
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	l.Total = len(entries)
	l.Offset = offset
	l.Limit = limit
	if offset > len(entries) {
		offset = len(entries)
	}
	end := offset + limit
	if end > len(entries) {
		end = len(entries)
	}
	l.Entries = entries[offset:end]
	return l
}
func observationRevision(c Config, p, id string, info os.FileInfo) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d", c.Revision, p, id, info.Size(), info.ModTime().UnixNano())))
	return hex.EncodeToString(sum[:])
}
