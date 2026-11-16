package library

import (
	"database/sql"
	"encoding/json"
	"errors"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
)

func openStore(dir string) (*sql.DB, error) {
	p := filepath.Join(dir, "library.sqlite")
	if i, e := os.Lstat(p); e == nil {
		if !i.Mode().IsRegular() || reparse(i) {
			return nil, ErrUnsafe
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return nil, e
	}
	q := url.Values{}
	for _, s := range []string{"foreign_keys(1)", "busy_timeout(5000)", "journal_mode(DELETE)", "synchronous(3)"} {
		q.Add("_pragma", s)
	}
	db, e := sql.Open("sqlite", filepath.ToSlash(p)+"?"+q.Encode())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	tx, e := db.Begin()
	if e != nil {
		db.Close()
		return nil, e
	}
	_, e = tx.Exec(`CREATE TABLE IF NOT EXISTS library_config(singleton INTEGER PRIMARY KEY CHECK(singleton=1),body TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS library_snapshots(root_revision TEXT NOT NULL,path TEXT NOT NULL,body TEXT NOT NULL,error TEXT NOT NULL DEFAULT '',error_at TEXT NOT NULL DEFAULT '',PRIMARY KEY(root_revision,path));
CREATE TABLE IF NOT EXISTS library_observations(sequence INTEGER PRIMARY KEY AUTOINCREMENT,root_revision TEXT NOT NULL,path TEXT NOT NULL,observed_at_utc TEXT NOT NULL,error TEXT NOT NULL,item_count INTEGER NOT NULL);`)
	if e == nil {
		e = tx.Commit()
	} else {
		tx.Rollback()
	}
	if e != nil {
		db.Close()
		return nil, e
	}
	if e = os.Chmod(p, 0600); e != nil {
		db.Close()
		return nil, e
	}
	return db, nil
}
func (m *Manager) config() (Config, error) {
	var body string
	err := m.db.QueryRow(`SELECT body FROM library_config WHERE singleton=1`).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err = json.Unmarshal([]byte(body), &c); err != nil {
		return c, err
	}
	if !filepath.IsAbs(c.Root) || len(c.Revision) != 64 || c.RootID == "" || c.SavedAt == "" {
		return Config{}, ErrUnsafe
	}
	return c, nil
}
func (m *Manager) saveConfig(c Config) error {
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	_, e = m.db.Exec(`INSERT INTO library_config VALUES(1,?) ON CONFLICT(singleton) DO UPDATE SET body=excluded.body`, string(b))
	return e
}
func (m *Manager) observation(c Config, p string) (Listing, error) {
	var body, reason, at string
	err := m.db.QueryRow(`SELECT body,error,error_at FROM library_snapshots WHERE root_revision=? AND path=?`, c.Revision, p).Scan(&body, &reason, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return Listing{Entries: []Entry{}}, nil
	}
	if err != nil {
		return Listing{}, err
	}
	var l Listing
	err = json.Unmarshal([]byte(body), &l)
	l.Error = reason
	l.ErrorAt = at
	return l, err
}
func (m *Manager) saveObservation(c Config, p string, l Listing, reason string) error {
	b, e := json.Marshal(l)
	if e != nil {
		return e
	}
	tx, e := m.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.Exec(`INSERT INTO library_snapshots(root_revision,path,body,error,error_at) VALUES(?,?,?,'','') ON CONFLICT(root_revision,path) DO UPDATE SET body=excluded.body,error='',error_at=''`, c.Revision, p, string(b))
	if e != nil {
		return e
	}
	_, e = tx.Exec(`INSERT INTO library_observations(root_revision,path,observed_at_utc,error,item_count) VALUES(?,?,?,?,?)`, c.Revision, p, l.ObservedAt, reason, len(l.Entries))
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (m *Manager) saveFailure(c Config, p, at, reason string) error {
	tx, e := m.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.Exec(`UPDATE library_snapshots SET error=?,error_at=? WHERE root_revision=? AND path=?`, reason, at, c.Revision, p)
	if e != nil {
		return e
	}
	_, e = tx.Exec(`INSERT INTO library_observations(root_revision,path,observed_at_utc,error,item_count) VALUES(?,?,?,?,0)`, c.Revision, p, at, reason)
	if e != nil {
		return e
	}
	return tx.Commit()
}
