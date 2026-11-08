package foldersync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"web_browser_baidu_onekey_upload/internal/durablefs"
)

func (m *Manager) checkRoots(ctx context.Context, c Config) error {
	id, err := rootIdentity(c.LocalRoot)
	if err != nil || id != c.LocalID {
		return fmt.Errorf("本地根目录缺失或身份变化，不能按全量删除处理")
	}
	for _, p := range m.exclusions() {
		if overlap(c.LocalRoot, p) {
			return fmt.Errorf("同步目录与应用存储冲突")
		}
	}
	owner := filepath.Join(c.LocalRoot, privateDirectory, "owner")
	if info, e := os.Lstat(filepath.Dir(owner)); e == nil && (info.Mode()&os.ModeSymlink != 0 || isReparse(info)) {
		return fmt.Errorf("本地隔离目录包含链接")
	}
	if raw, err := os.ReadFile(owner); err == nil {
		if string(raw) != c.ID {
			return fmt.Errorf("本地隔离目录不属于此同步配置")
		}
	} else if !os.IsNotExist(err) {
		return err
	} else if _, e := os.Lstat(filepath.Dir(owner)); !os.IsNotExist(e) {
		return fmt.Errorf("本地隔离目录缺少身份凭据")
	}
	account, remoteID, err := m.remote.Inspect(ctx, c.RemoteRoot)
	if err != nil {
		return err
	}
	if account != c.Account || remoteID != c.RemoteID {
		return fmt.Errorf("远端账号、应用或根目录身份变化")
	}
	return nil
}
func (m *Manager) snapshot(ctx context.Context, c Config, cache []Entry) (Snapshot, error) {
	var s Snapshot
	if err := m.checkRoots(ctx, c); err != nil {
		return s, err
	}
	var err error
	s.Local, err = ScanLocal(ctx, c.LocalRoot)
	if err != nil {
		return s, err
	}
	s.Remote, err = m.remote.Scan(ctx, c, cache)
	if err != nil {
		return s, err
	}
	seen := map[string]bool{}
	for _, e := range s.Remote {
		if !safeRelative(e.Path) {
			return s, fmt.Errorf("远端包含不兼容本地路径：%s", e.Path)
		}
		key := strings.ToLower(filepath.ToSlash(filepath.Clean(e.Path)))
		if seen[key] {
			return s, fmt.Errorf("远端快照项目重复")
		}
		seen[key] = true
	}
	return s, m.checkRoots(ctx, c)
}
func (m *Manager) Start(id string) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := Run{ID: newID(), ConfigID: id, Status: "running", Started: time.Now().UTC()}
	if m.closed || m.active {
		return r, fmt.Errorf("已有同步执行中")
	}
	c, err := m.config(id)
	if err != nil {
		return r, err
	}
	var unresolved int
	if err = m.db.QueryRow(`SELECT count(*) FROM sync_steps s JOIN sync_runs r ON r.id=s.run_id WHERE r.config_id=? AND s.status IN ('unknown','intent')`, id).Scan(&unresolved); err != nil {
		return r, err
	}
	if unresolved > 0 {
		return r, fmt.Errorf("历史同步有结果未知项目，请先只读核对；不能重发")
	}
	tx, err := m.db.Begin()
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO sync_runs VALUES(?,?,?,?)`, r.ID, id, r.Status, encoded(r)); err != nil {
		return r, err
	}
	if _, err = tx.Exec(`INSERT INTO sync_events(run_id,kind,recorded,evidence) VALUES(?,?,?,?)`, r.ID, "run_started", r.Started.Format(time.RFC3339Nano), encoded(c)); err != nil {
		return r, err
	}
	if err = tx.Commit(); err != nil {
		return r, err
	}
	m.active = true
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() { m.mu.Lock(); m.active = false; m.mu.Unlock() }()
		m.execute(c, r)
	}()
	return r, nil
}
func (m *Manager) execute(c Config, r Run) {
	ctx := WithRunID(m.ctx, r.ID)
	fail := func(status string, err error) {
		r.Status = status
		if err != nil {
			r.Error = err.Error()
		}
		if err := m.finish(r); err != nil {
			_ = m.event(r.ID, "persistence_failure", map[string]string{"error": err.Error()})
		}
	}
	base, err := m.baseline(c.ID)
	if err != nil {
		fail("failed", err)
		return
	}
	now, err := m.snapshot(ctx, c, base.Remote)
	if err != nil {
		fail("failed", err)
		return
	}
	if err = m.cache(c.ID, r.ID, now); err != nil {
		fail("failed", err)
		return
	}
	if err = m.event(r.ID, "snapshot_before", now); err != nil {
		fail("failed", err)
		return
	}
	plan := Plan(c.Mode, base, now)
	if err = m.event(r.ID, "detected_changes", ObservedChanges(base, now)); err != nil {
		fail("failed", err)
		return
	}
	r.StepCount = len(plan)
	for i, s := range plan {
		status := "planned"
		if s.Action == "conflict" {
			status = "conflict"
		}
		if err = m.saveStep(r.ID, StepRecord{Ordinal: i + 1, Step: s, Status: status}); err != nil {
			fail("failed", err)
			return
		}
	}
	if err = m.progress(r); err != nil {
		fail("failed", err)
		return
	}
	for _, s := range plan {
		if s.Action == "conflict" {
			fail("conflict", fmt.Errorf("存在冲突，已保留两端；解决冲突后重新检查"))
			return
		}
	}
	for i, s := range plan {
		if err = ctx.Err(); err != nil {
			fail("interrupted", err)
			return
		}
		if err = m.checkRoots(ctx, c); err != nil {
			fail("failed", err)
			return
		}
		// Recheck the complete observed local input immediately before intent.
		if s.Local != nil {
			current, e := localEntry(ctx, c.LocalRoot, s.Local.Path)
			if e != nil || !same(current, s.Local) || current.ID != s.Local.ID {
				fail("conflict", fmt.Errorf("本地项目在计划后变化：%s", s.Local.Path))
				return
			}
		}
		record := StepRecord{Ordinal: i + 1, Step: s, Status: "intent"}
		if err = m.saveStep(r.ID, record); err != nil {
			fail("failed", err)
			return
		}
		var result any
		switch s.Action {
		case "download", "local_mkdir", "local_move", "local_delete":
			result, err = m.applyLocal(ctx, c, s, r.ID)
		default:
			result, err = m.remote.Apply(ctx, c, s, r.ID, func(kind string, evidence any) error { return m.event(r.ID, kind, evidence) })
		}
		if err != nil {
			record.Status = "unknown"
			var notStarted *NotStartedError
			if errors.As(err, &notStarted) {
				record.Status = "failed"
			}
			record.Error = err.Error()
			_ = m.saveStep(r.ID, record)
			fail(record.Status, fmt.Errorf("同步步骤未完成：%s；%w", s.Path, err))
			return
		}
		record.Status = "confirmed"
		record.Result = json.RawMessage(encoded(result))
		if err = m.saveStep(r.ID, record); err != nil {
			fail("unknown", err)
			return
		}
		r.Confirmed++
		if err = m.progress(r); err != nil {
			fail("unknown", err)
			return
		}
	}
	after, err := m.snapshot(ctx, c, now.Remote)
	if err != nil {
		fail("failed", err)
		return
	}
	if err = m.cache(c.ID, r.ID, after); err != nil {
		fail("failed", err)
		return
	}
	if err = m.event(r.ID, "snapshot_after", after); err != nil {
		fail("failed", err)
		return
	}
	// Atomic replacement creates a new filesystem/provider ID. Relink only the
	// successfully verified replacement, retaining the original content/path
	// baseline for the subsequent three-way check.
	afterLocal, afterRemote := index(after.Local), index(after.Remote)
	for _, s := range plan {
		if s.Action == "download" {
			actual := afterLocal[s.Path]
			if !sameContent(actual, s.Remote) {
				fail("conflict", fmt.Errorf("下载发布后内容变化：%s", s.Path))
				return
			}
			if s.Local != nil {
				for i := range base.Local {
					if base.Local[i].ID == s.Local.ID {
						base.Local[i].ID = actual.ID
					}
				}
			}
		}
		if s.Action == "upload" {
			actual := afterRemote[s.Path]
			if !sameContent(actual, s.Local) {
				fail("conflict", fmt.Errorf("上传发布后内容变化：%s", s.Path))
				return
			}
			if s.Remote != nil {
				for i := range base.Remote {
					if base.Remote[i].ID == s.Remote.ID {
						base.Remote[i].ID = actual.ID
					}
				}
			}
		}
	}
	// Verification is another three-way comparison against the original base,
	// not an assumption that successful requests made the trees identical.
	if remaining := Plan(c.Mode, base, after); len(remaining) > 0 {
		_ = m.event(r.ID, "verification_changes", remaining)
		fail("conflict", fmt.Errorf("复核仍有变化或新冲突，未推进同步基线"))
		return
	}
	tx, err := m.db.Begin()
	if err != nil {
		fail("failed", err)
		return
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO sync_baselines VALUES(?,?,?) ON CONFLICT(config_id) DO UPDATE SET run_id=excluded.run_id,body=excluded.body`, c.ID, r.ID, encoded(after)); err != nil {
		tx.Rollback()
		fail("failed", err)
		return
	}
	r.Status = "complete"
	finished := time.Now().UTC()
	r.Finished = &finished
	if _, err = tx.Exec(`UPDATE sync_runs SET status=?,body=? WHERE id=?`, r.Status, encoded(r), r.ID); err != nil {
		tx.Rollback()
		fail("failed", err)
		return
	}
	if _, err = tx.Exec(`INSERT INTO sync_events(run_id,kind,recorded,evidence) VALUES(?,?,?,?)`, r.ID, "baseline_committed", finished.Format(time.RFC3339Nano), encoded(r)); err != nil {
		tx.Rollback()
		fail("failed", err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail("unknown", err)
	}
}

func (m *Manager) backup(c Config, run, p, expectedHash string) (string, error) {
	root := filepath.Join(c.LocalRoot, privateDirectory)
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	owner := filepath.Join(root, "owner")
	if raw, err := os.ReadFile(owner); err == nil {
		if string(raw) != c.ID {
			return "", fmt.Errorf("隔离目录身份冲突")
		}
	} else if os.IsNotExist(err) {
		f, e := os.OpenFile(owner, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return "", e
		}
		_, e = f.WriteString(c.ID)
		if e == nil {
			e = f.Sync()
		}
		f.Close()
		if e != nil {
			return "", e
		}
	} else {
		return "", err
	}
	target := filepath.Join(root, run, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return "", err
	}
	source, err := localPath(c.LocalRoot, p)
	if err != nil {
		return "", err
	}
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, err = io.Copy(io.MultiWriter(out, hash), input)
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil && hex.EncodeToString(hash.Sum(nil)) != expectedHash {
		err = fmt.Errorf("隔离备份内容与原计划不一致")
	}
	if err == nil {
		for parent := filepath.Dir(target); ; parent = filepath.Dir(parent) {
			if err = durablefs.SyncDirectory(parent); err != nil {
				break
			}
			if parent == c.LocalRoot {
				break
			}
			if filepath.Dir(parent) == parent {
				err = fmt.Errorf("隔离备份目录边界无效")
				break
			}
		}
	}
	return target, err
}
func (m *Manager) applyLocal(ctx context.Context, c Config, s Step, run string) (result any, resultErr error) {
	effect := false
	defer func() {
		if resultErr != nil && !effect {
			resultErr = &NotStartedError{resultErr}
		}
	}()
	target, err := localPath(c.LocalRoot, s.Path)
	if err != nil {
		return nil, err
	}
	receipt := map[string]any{"action": s.Action, "path": target, "observed_at_utc": time.Now().UTC()}
	switch s.Action {
	case "local_mkdir":
		effect = true
		if err = os.Mkdir(target, 0755); err != nil {
			return nil, err
		}
	case "local_move":
		source, e := localPath(c.LocalRoot, s.From)
		if e != nil {
			return nil, e
		}
		if _, e = os.Lstat(target); !os.IsNotExist(e) {
			return nil, fmt.Errorf("移动目标已占用")
		}
		if err = verifyLocalExpected(ctx, c, s.Local); err != nil {
			return nil, err
		}
		effect = true
		if err = durablefs.PublishNoReplace(source, target); err != nil {
			return nil, err
		}
		if err = durablefs.SyncDirectory(filepath.Dir(source)); err != nil {
			return nil, err
		}
		receipt["from"] = source
	case "local_delete":
		if s.Local == nil {
			return nil, fmt.Errorf("删除缺少原身份")
		}
		if !s.Local.Directory {
			backup, e := m.backup(c, run, s.Path, s.Local.SHA256)
			if e != nil {
				return nil, e
			}
			receipt["backup"] = backup
		}
		// Copying a large backup may retain an old open handle while an editor
		// atomically replaces the pathname. Recheck after backup, not just before.
		if err = verifyLocalExpected(ctx, c, s.Local); err != nil {
			return nil, err
		}
		effect = true
		if err = os.Remove(target); err != nil {
			return nil, err
		}
	case "download":
		if s.Remote == nil {
			return nil, fmt.Errorf("下载缺少远端身份")
		}
		stage, err := os.CreateTemp(filepath.Dir(target), ".nas-sync-part-")
		if err != nil {
			return nil, err
		}
		temporary := stage.Name()
		stage.Close()
		defer os.Remove(temporary)
		if err = m.remote.Download(ctx, c, *s.Remote, temporary); err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(c.LocalRoot, temporary)
		if err != nil {
			return nil, err
		}
		downloaded, err := localEntry(ctx, c.LocalRoot, filepath.ToSlash(rel))
		if err != nil || !sameContent(downloaded, s.Remote) {
			return nil, fmt.Errorf("下载内容校验失败")
		}
		if s.Local != nil {
			backup, e := m.backup(c, run, s.Local.Path, s.Local.SHA256)
			if e != nil {
				return nil, e
			}
			receipt["backup"] = backup
			current, e := localEntry(ctx, c.LocalRoot, s.Local.Path)
			if e != nil || !same(current, s.Local) || current.ID != s.Local.ID {
				return nil, fmt.Errorf("发布前本地项目变化")
			}
		}
		if s.Local == nil || s.Local.Path != s.Path {
			if _, e := os.Lstat(target); !os.IsNotExist(e) {
				return nil, fmt.Errorf("下载目标已占用")
			}
		}
		effect = true
		if s.Local != nil && s.Local.Path == s.Path {
			err = durablefs.ReplaceFile(temporary, target)
		} else {
			err = durablefs.PublishNoReplace(temporary, target)
		}
		if err != nil {
			return nil, err
		}
		if s.Local != nil && s.Local.Path != s.Path {
			current, e := localEntry(ctx, c.LocalRoot, s.Local.Path)
			if e != nil || !same(current, s.Local) || current.ID != s.Local.ID {
				return nil, fmt.Errorf("发布后旧路径发生变化")
			}
			old, e := localPath(c.LocalRoot, s.Local.Path)
			if e != nil {
				return nil, e
			}
			if e = os.Remove(old); e != nil {
				return nil, e
			}
			if e = durablefs.SyncDirectory(filepath.Dir(old)); e != nil {
				return nil, e
			}
		}
	default:
		return nil, fmt.Errorf("未知本地动作")
	}
	if err = durablefs.SyncDirectory(filepath.Dir(target)); err != nil {
		return nil, err
	}
	return receipt, nil
}

func verifyLocalExpected(ctx context.Context, c Config, expected *Entry) error {
	if expected == nil {
		return fmt.Errorf("缺少本地预期身份")
	}
	current, err := localEntry(ctx, c.LocalRoot, expected.Path)
	if err != nil {
		return err
	}
	if current == nil || !same(current, expected) || current.ID != expected.ID {
		return fmt.Errorf("执行前本地路径身份或内容变化：%s", expected.Path)
	}
	return nil
}
