package foldersync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

func (m *Manager) initBatchVerifications() error {
	_, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS sync_resolution_batch_verification_attempts(run_id TEXT PRIMARY KEY,batch_id TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS sync_resolution_batch_verifications(batch_id TEXT PRIMARY KEY,execution_run_id TEXT NOT NULL,verification_run_id TEXT NOT NULL,recorded TEXT NOT NULL);
 CREATE TRIGGER IF NOT EXISTS sync_batch_verification_no_update BEFORE UPDATE ON sync_resolution_batch_verifications BEGIN SELECT RAISE(ABORT,'batch completion is immutable'); END;
 CREATE TRIGGER IF NOT EXISTS sync_batch_verification_no_delete BEFORE DELETE ON sync_resolution_batch_verifications BEGIN SELECT RAISE(ABORT,'batch completion is immutable'); END;
 CREATE TRIGGER IF NOT EXISTS sync_batch_verify_attempt_no_update BEFORE UPDATE ON sync_resolution_batch_verification_attempts BEGIN SELECT RAISE(ABORT,'batch verification admission is immutable'); END;
 CREATE TRIGGER IF NOT EXISTS sync_batch_verify_attempt_no_delete BEFORE DELETE ON sync_resolution_batch_verification_attempts BEGIN SELECT RAISE(ABORT,'batch verification admission is immutable'); END;`)
	return err
}
func (m *Manager) ResolutionBatchVerification(ctx context.Context, batch string) (Run, error) {
	var run Run
	var body string
	err := m.db.QueryRowContext(ctx, `SELECT r.body FROM sync_resolution_batch_verifications v JOIN sync_runs r ON r.id=v.verification_run_id WHERE v.batch_id=?`, batch).Scan(&body)
	if err != nil {
		return run, err
	}
	err = json.Unmarshal([]byte(body), &run)
	return run, err
}
func (m *Manager) LatestResolutionBatchVerification(ctx context.Context, batch string) (Run, error) {
	run, err := m.ResolutionBatchVerification(ctx, batch)
	if err == nil || err != sql.ErrNoRows {
		return run, err
	}
	var body string
	err = m.db.QueryRowContext(ctx, `SELECT r.body FROM sync_resolution_batch_verification_attempts a JOIN sync_runs r ON r.id=a.run_id WHERE a.batch_id=? ORDER BY a.rowid DESC LIMIT 1`, batch).Scan(&body)
	if err != nil {
		return run, err
	}
	err = json.Unmarshal([]byte(body), &run)
	return run, err
}
func (m *Manager) StartResolutionBatchVerification(id string) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Run{}, fmt.Errorf("同步服务已关闭")
	}
	completed, err := m.ResolutionBatchVerification(m.ctx, id)
	if err == nil {
		return completed, nil
	}
	if err != sql.ErrNoRows {
		return Run{}, err
	}
	var body string
	err = m.db.QueryRow(`SELECT r.body FROM sync_resolution_batch_verification_attempts a JOIN sync_runs r ON r.id=a.run_id WHERE a.batch_id=? AND r.status='running' ORDER BY a.rowid DESC LIMIT 1`, id).Scan(&body)
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
	batch, err := m.ResolutionBatch(m.ctx, id)
	if err != nil {
		return Run{}, err
	}
	execution, err := m.ResolutionBatchExecution(m.ctx, id)
	if err != nil || execution.Run.Status != "verification_pending" || execution.Run.Confirmed != len(batch.Items) || len(execution.Items) != len(batch.Items) {
		return Run{}, fmt.Errorf("批次尚无全部项目已确认的执行记录")
	}
	cfg, err := m.config(batch.Config.ID)
	if err != nil || encoded(cfg) != encoded(batch.Config) {
		return Run{}, fmt.Errorf("同步配置已变化")
	}
	required := []string{}
	for i, item := range execution.Items {
		if item.DecisionID != batch.Items[i].DecisionID || item.Run.Status != "verification_pending" || item.Run.Confirmed != item.Run.StepCount {
			return Run{}, fmt.Errorf("子项缺少完整执行凭据")
		}
		required = append(required, item.DecisionID)
	}
	var unresolved int
	if err = m.db.QueryRow(`SELECT count(*) FROM sync_steps s JOIN sync_runs r ON r.id=s.run_id WHERE r.config_id=? AND s.status IN ('intent','unknown')`, cfg.ID).Scan(&unresolved); err != nil {
		return Run{}, err
	}
	if unresolved > 0 {
		return Run{}, fmt.Errorf("历史操作尚未核清")
	}
	run := Run{ID: newID(), ConfigID: cfg.ID, Status: "running", Started: time.Now().UTC()}
	tx, err := m.db.Begin()
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO sync_runs VALUES(?,?,?,?)`, run.ID, cfg.ID, run.Status, encoded(run)); err != nil {
		return Run{}, err
	}
	if _, err = tx.Exec(`INSERT INTO sync_resolution_batch_verification_attempts VALUES(?,?)`, run.ID, id); err != nil {
		return Run{}, err
	}
	for _, event := range []struct {
		kind  string
		value any
	}{{"run_started", cfg}, {"batch_verification_started", map[string]any{"batch_id": id, "execution_run": execution.Run.ID, "decision_ids": required}}} {
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
		m.verifyResolutionScopeRequired(cfg, required, &execution, run)
	}()
	return run, nil
}

func (m *Manager) resolutionBatchForDecision(ctx context.Context, id string) (string, error) {
	var batch string
	err := m.db.QueryRowContext(ctx, `SELECT batch_id FROM sync_resolution_batch_executions WHERE EXISTS(SELECT 1 FROM json_each(body,'$.items') WHERE json_extract(value,'$.decision_id')=?)`, id).Scan(&batch)
	return batch, err
}
