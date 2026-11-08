package foldersync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

func (m *Manager) initResolutionVerifications() error {
	_, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS sync_resolution_verification_attempts(run_id TEXT PRIMARY KEY,decision_id TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS sync_resolution_verifications(decision_id TEXT PRIMARY KEY,execution_run_id TEXT NOT NULL,verification_run_id TEXT NOT NULL);
 CREATE TRIGGER IF NOT EXISTS sync_resolution_verification_no_update BEFORE UPDATE ON sync_resolution_verifications BEGIN SELECT RAISE(ABORT,'completion link is immutable'); END;
 CREATE TRIGGER IF NOT EXISTS sync_resolution_verification_no_delete BEFORE DELETE ON sync_resolution_verifications BEGIN SELECT RAISE(ABORT,'completion link is immutable'); END;
 CREATE TRIGGER IF NOT EXISTS sync_resolution_attempt_no_update BEFORE UPDATE ON sync_resolution_verification_attempts BEGIN SELECT RAISE(ABORT,'verification admission is immutable'); END;
 CREATE TRIGGER IF NOT EXISTS sync_resolution_attempt_no_delete BEFORE DELETE ON sync_resolution_verification_attempts BEGIN SELECT RAISE(ABORT,'verification admission is immutable'); END;`)
	return err
}

func (m *Manager) ResolutionVerification(ctx context.Context, decision string) (Run, error) {
	var result Run
	var body string
	err := m.db.QueryRowContext(ctx, `SELECT r.body FROM sync_resolution_verifications v JOIN sync_runs r ON r.id=v.verification_run_id WHERE v.decision_id=?`, decision).Scan(&body)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(body), &result)
	return result, err
}

func (m *Manager) LatestResolutionVerification(ctx context.Context, decision string) (Run, error) {
	completed, err := m.ResolutionVerification(ctx, decision)
	if err == nil || err != sql.ErrNoRows {
		return completed, err
	}
	var result Run
	var body string
	err = m.db.QueryRowContext(ctx, `SELECT r.body FROM sync_resolution_verification_attempts a JOIN sync_runs r ON r.id=a.run_id WHERE a.decision_id=? ORDER BY a.rowid DESC LIMIT 1`, decision).Scan(&body)
	if err == sql.ErrNoRows {
		if batchID, e := m.resolutionBatchForDecision(ctx, decision); e == nil {
			return m.LatestResolutionBatchVerification(ctx, batchID)
		} else if e != sql.ErrNoRows {
			return result, e
		}
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(body), &result)
	return result, err
}

func (m *Manager) StartResolutionVerification(decisionID string) (Run, error) {
	m.mu.Lock()
	locked := true
	defer func() {
		if locked {
			m.mu.Unlock()
		}
	}()
	if m.closed {
		return Run{}, fmt.Errorf("同步服务已关闭")
	}
	completed, err := m.ResolutionVerification(m.ctx, decisionID)
	if err == nil {
		return completed, nil
	}
	if err != sql.ErrNoRows {
		return Run{}, err
	}
	var body string
	err = m.db.QueryRow(`SELECT r.body FROM sync_resolution_verification_attempts a JOIN sync_runs r ON r.id=a.run_id WHERE a.decision_id=? AND r.status='running' ORDER BY a.rowid DESC LIMIT 1`, decisionID).Scan(&body)
	if err == nil {
		var run Run
		err = json.Unmarshal([]byte(body), &run)
		return run, err
	}
	if err != sql.ErrNoRows {
		return Run{}, err
	}
	if m.active {
		return Run{}, fmt.Errorf("已有同步或核对执行中")
	}
	decision, err := m.Resolution(m.ctx, decisionID)
	if err != nil {
		return Run{}, err
	}
	execution, err := m.ResolutionExecution(m.ctx, decisionID)
	if err != nil || execution.Status != "verification_pending" {
		return Run{}, fmt.Errorf("所选决策尚无全部步骤已确认的执行记录")
	}
	// A child cannot bypass its batch's all-output completion gate via the
	// standalone UI/API. Release the reservation before entering the batch gate.
	if batchID, e := m.resolutionBatchForDecision(m.ctx, decisionID); e == nil {
		locked = false
		m.mu.Unlock()
		return m.StartResolutionBatchVerification(batchID)
	} else if e != sql.ErrNoRows {
		return Run{}, e
	}
	c, err := m.config(decision.Config.ID)
	if err != nil || encoded(c) != encoded(decision.Config) {
		return Run{}, fmt.Errorf("同步配置已变化")
	}
	var unresolved int
	if err = m.db.QueryRow(`SELECT count(*) FROM sync_steps s JOIN sync_runs r ON r.id=s.run_id WHERE r.config_id=? AND s.status IN ('intent','unknown')`, c.ID).Scan(&unresolved); err != nil {
		return Run{}, err
	}
	if unresolved > 0 {
		return Run{}, fmt.Errorf("历史写入结果未知，不能推进基线")
	}
	run := Run{ID: newID(), ConfigID: c.ID, Status: "running", Started: time.Now().UTC()}
	tx, err := m.db.Begin()
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO sync_runs VALUES(?,?,?,?)`, run.ID, c.ID, run.Status, encoded(run)); err != nil {
		return Run{}, err
	}
	if _, err = tx.Exec(`INSERT INTO sync_resolution_verification_attempts VALUES(?,?)`, run.ID, decisionID); err != nil {
		return Run{}, err
	}
	for _, event := range []struct {
		kind  string
		value any
	}{{"run_started", c}, {"resolution_verification_started", map[string]string{"decision_id": decisionID, "execution_run": execution.ID}}} {
		if _, err = tx.Exec(`INSERT INTO sync_events(run_id,kind,recorded,evidence) VALUES(?,?,?,?)`, run.ID, event.kind, run.Started.Format(time.RFC3339Nano), encoded(event.value)); err != nil {
			return Run{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Run{}, err
	}
	m.active = true
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() { m.mu.Lock(); m.active = false; m.mu.Unlock() }()
		m.verifyResolutionScope(c, decisionID, run)
	}()
	return run, nil
}

type pendingResolution struct {
	decision  Resolution
	execution Run
	after     Snapshot
}

func (m *Manager) pendingResolutions(ctx context.Context, c Config) ([]pendingResolution, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT d.body,r.body,(SELECT evidence FROM sync_events WHERE run_id=r.id AND kind='snapshot_after' ORDER BY sequence DESC LIMIT 1) FROM sync_resolution_executions e JOIN sync_resolutions d ON d.id=e.decision_id JOIN sync_runs r ON r.id=e.run_id LEFT JOIN sync_resolution_verifications v ON v.decision_id=e.decision_id WHERE r.config_id=? AND r.status='verification_pending' AND v.decision_id IS NULL ORDER BY e.rowid`, c.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []pendingResolution{}
	for rows.Next() {
		var d, r, a string
		var p pendingResolution
		if err = rows.Scan(&d, &r, &a); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(d), &p.decision) != nil || json.Unmarshal([]byte(r), &p.execution) != nil || json.Unmarshal([]byte(a), &p.after) != nil || p.after.Local == nil || p.after.Remote == nil {
			return nil, fmt.Errorf("待复核决策缺少完成快照")
		}
		if encoded(p.decision.Config) == encoded(c) {
			result = append(result, p)
		}
	}
	return result, rows.Err()
}

func confirmedResolutionMatches(p pendingResolution, current Snapshot) bool {
	original := p.decision.Conflict.Step.Path
	for _, pair := range []struct {
		before, now []Entry
		remote      bool
	}{{p.after.Local, current.Local, false}, {p.after.Remote, current.Remote, true}} {
		expected, actual := index(pair.before), index(pair.now)
		paths := []string{original}
		if p.decision.LocalCopy != "" && (!pair.remote || p.decision.Config.Mode == "both") {
			paths = append(paths, p.decision.LocalCopy)
		}
		for _, path := range paths {
			if p.decision.SubtreeReview != nil {
				wantHash, wantCount := subtreeIdentity(pair.before, path)
				gotHash, gotCount := subtreeIdentity(pair.now, path)
				if wantHash != gotHash || wantCount != gotCount {
					return false
				}
				continue
			}
			a, b := expected[path], actual[path]
			if a == nil || b == nil || !same(a, b) || a.ID != b.ID {
				return false
			}
		}
	}
	return true
}

// This is a read-only full traversal, followed by an atomic local evidence and
// baseline commit. It performs no provider mutation or synchronization step.
func (m *Manager) verifyResolutionScope(c Config, selected string, run Run) {
	m.verifyResolutionScopeRequired(c, []string{selected}, nil, run)
}
func (m *Manager) verifyResolutionScopeRequired(c Config, required []string, batch *ResolutionBatchExecution, run Run) {
	ctx := WithRunID(m.ctx, run.ID)
	fail := func(status string, err error) {
		run.Status = status
		run.Error = err.Error()
		if e := m.finish(run); e != nil {
			_ = m.event(run.ID, "persistence_failure", map[string]string{"error": e.Error()})
		}
	}
	var priorRun, priorBody string
	err := m.db.QueryRowContext(ctx, `SELECT run_id,body FROM sync_baselines WHERE config_id=?`, c.ID).Scan(&priorRun, &priorBody)
	if err != nil && err != sql.ErrNoRows {
		fail("failed", err)
		return
	}
	var base Snapshot
	if priorBody != "" && json.Unmarshal([]byte(priorBody), &base) != nil {
		fail("failed", fmt.Errorf("原始基线不可读"))
		return
	}
	current, err := m.snapshot(ctx, c, nil)
	if err != nil {
		fail("failed", err)
		return
	}
	if current.Local == nil || current.Remote == nil {
		fail("failed", fmt.Errorf("完整范围观察证据缺失"))
		return
	}
	if err = m.event(run.ID, "snapshot_before", current); err != nil {
		fail("failed", err)
		return
	}
	pending, err := m.pendingResolutions(ctx, c)
	if err != nil {
		fail("failed", err)
		return
	}
	// Batch members may already have individual completion links. Reobserve
	// their immutable execution outputs, preserving those earlier associations.
	present := map[string]bool{}
	for _, p := range pending {
		present[p.decision.ID] = true
	}
	for _, id := range required {
		if present[id] {
			continue
		}
		p, e := m.resolutionOutput(ctx, id)
		if e != nil {
			fail("failed", e)
			return
		}
		if encoded(p.decision.Config) != encoded(c) {
			fail("conflict", fmt.Errorf("批次配置不一致"))
			return
		}
		pending = append(pending, p)
		present[id] = true
	}
	matched := []pendingResolution{}
	matchedIDs := map[string]bool{}
	for _, p := range pending {
		if !confirmedResolutionMatches(p, current) {
			continue
		}
		matched = append(matched, p)
		matchedIDs[p.decision.ID] = true
		rebindResolutionBaseline(&base, p.decision, current)
	}
	for _, id := range required {
		if !matchedIDs[id] {
			fail("conflict", fmt.Errorf("所选决策的已确认输出已变化，保留原基线"))
			return
		}
	}
	if remaining := Plan(c.Mode, base, current); len(remaining) > 0 {
		_ = m.event(run.ID, "verification_changes", remaining)
		fail("conflict", fmt.Errorf("完整范围仍有变化或冲突，未推进基线"))
		return
	}
	if err = m.checkRoots(ctx, c); err != nil {
		fail("failed", err)
		return
	}
	currentConfig, err := m.config(c.ID)
	if err != nil || encoded(currentConfig) != encoded(c) {
		fail("conflict", fmt.Errorf("核对期间同步配置变化，保留原基线"))
		return
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		fail("failed", err)
		return
	}
	defer tx.Rollback()
	var actualRun, actualBody string
	err = tx.QueryRowContext(ctx, `SELECT run_id,body FROM sync_baselines WHERE config_id=?`, c.ID).Scan(&actualRun, &actualBody)
	if err != nil && err != sql.ErrNoRows {
		tx.Rollback()
		fail("failed", err)
		return
	}
	if actualRun != priorRun || actualBody != priorBody {
		tx.Rollback()
		fail("conflict", fmt.Errorf("核对期间共同基线变化，需重新检查"))
		return
	}
	run.Status = "complete"
	at := time.Now().UTC()
	run.Finished = &at
	if _, err = tx.Exec(`INSERT INTO sync_baselines VALUES(?,?,?) ON CONFLICT(config_id) DO UPDATE SET run_id=excluded.run_id,body=excluded.body`, c.ID, run.ID, encoded(current)); err != nil {
		tx.Rollback()
		fail("failed", err)
		return
	}
	if _, err = tx.Exec(`UPDATE sync_runs SET status=?,body=? WHERE id=?`, run.Status, encoded(run), run.ID); err != nil {
		tx.Rollback()
		fail("failed", err)
		return
	}
	for _, p := range matched {
		if _, err = tx.Exec(`INSERT INTO sync_resolution_verifications VALUES(?,?,?) ON CONFLICT(decision_id) DO NOTHING`, p.decision.ID, p.execution.ID, run.ID); err != nil {
			tx.Rollback()
			fail("failed", err)
			return
		}
		if _, err = tx.Exec(`INSERT INTO sync_events(run_id,kind,recorded,evidence) VALUES(?,?,?,?)`, run.ID, "resolution_verified", at.Format(time.RFC3339Nano), encoded(map[string]string{"decision_id": p.decision.ID, "execution_run": p.execution.ID})); err != nil {
			tx.Rollback()
			fail("failed", err)
			return
		}
	}
	if batch != nil {
		if _, err = tx.Exec(`INSERT INTO sync_resolution_batch_verifications VALUES(?,?,?,?)`, batch.BatchID, batch.Run.ID, run.ID, at.Format(time.RFC3339Nano)); err != nil {
			tx.Rollback()
			fail("failed", err)
			return
		}
		if _, err = tx.Exec(`INSERT INTO sync_events(run_id,kind,recorded,evidence) VALUES(?,?,?,?)`, run.ID, "batch_verified", at.Format(time.RFC3339Nano), encoded(map[string]any{"batch_id": batch.BatchID, "execution_run": batch.Run.ID, "decision_ids": required})); err != nil {
			tx.Rollback()
			fail("failed", err)
			return
		}
	}
	if _, err = tx.Exec(`INSERT INTO sync_events(run_id,kind,recorded,evidence) VALUES(?,?,?,?)`, run.ID, "baseline_committed", at.Format(time.RFC3339Nano), encoded(run)); err != nil {
		tx.Rollback()
		fail("failed", err)
		return
	}
	if err = tx.Commit(); err != nil {
		fail("unknown", err)
	}
}

// Rebind only the confirmed output's identities in this temporary planning
// copy. Original stored baseline contents remain unchanged until final commit.
// Removed descendants need distinct absent IDs so their former IDs cannot make
// the planner follow them into a retained sibling copy.
func rebindResolutionBaseline(base *Snapshot, decision Resolution, current Snapshot) {
	root := decision.Conflict.Step.Path
	for _, pair := range []struct {
		entries []Entry
		actual  []Entry
	}{{base.Local, current.Local}, {base.Remote, current.Remote}} {
		now := index(pair.actual)
		for i := range pair.entries {
			e := &pair.entries[i]
			if e.Path != root && !(decision.SubtreeReview != nil && under(e.Path, root)) {
				continue
			}
			if actual := now[e.Path]; actual != nil {
				e.ID = actual.ID
			} else {
				e.ID = "resolution-absent:" + decision.ID + ":" + e.Path
			}
		}
	}
}

func (m *Manager) resolutionOutput(ctx context.Context, id string) (pendingResolution, error) {
	var p pendingResolution
	var d, r, a string
	err := m.db.QueryRowContext(ctx, `SELECT d.body,r.body,(SELECT evidence FROM sync_events WHERE run_id=r.id AND kind='snapshot_after' ORDER BY sequence DESC LIMIT 1) FROM sync_resolutions d JOIN sync_resolution_executions e ON e.decision_id=d.id JOIN sync_runs r ON r.id=e.run_id WHERE d.id=? AND r.status='verification_pending'`, id).Scan(&d, &r, &a)
	if err != nil {
		return p, err
	}
	if json.Unmarshal([]byte(d), &p.decision) != nil || json.Unmarshal([]byte(r), &p.execution) != nil || json.Unmarshal([]byte(a), &p.after) != nil || p.after.Local == nil || p.after.Remote == nil {
		return p, fmt.Errorf("缺少完整执行后快照")
	}
	return p, nil
}
