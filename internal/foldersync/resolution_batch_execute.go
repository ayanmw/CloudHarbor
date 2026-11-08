package foldersync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

type BatchExecutionItem struct {
	DecisionID string `json:"decision_id"`
	Run        Run    `json:"run"`
}
type ResolutionBatchExecution struct {
	BatchID string               `json:"batch_id"`
	CheckID string               `json:"check_id"`
	Run     Run                  `json:"run"`
	Items   []BatchExecutionItem `json:"items"`
}

func (m *Manager) initBatchExecutions() error {
	_, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS sync_resolution_batch_executions(batch_id TEXT PRIMARY KEY,run_id TEXT NOT NULL UNIQUE,body TEXT NOT NULL);
 CREATE TRIGGER IF NOT EXISTS sync_batch_execution_no_update BEFORE UPDATE ON sync_resolution_batch_executions BEGIN SELECT RAISE(ABORT,'batch admission is immutable'); END;
 CREATE TRIGGER IF NOT EXISTS sync_batch_execution_no_delete BEFORE DELETE ON sync_resolution_batch_executions BEGIN SELECT RAISE(ABORT,'batch admission is immutable'); END;`)
	return err
}
func (m *Manager) ResolutionBatchExecution(ctx context.Context, batch string) (ResolutionBatchExecution, error) {
	var result ResolutionBatchExecution
	var body string
	err := m.db.QueryRowContext(ctx, `SELECT body FROM sync_resolution_batch_executions WHERE batch_id=?`, batch).Scan(&body)
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal([]byte(body), &result); err != nil {
		return result, err
	}
	read := func(run *Run) error {
		var body string
		if err := m.db.QueryRowContext(ctx, `SELECT body FROM sync_runs WHERE id=?`, run.ID).Scan(&body); err != nil {
			return err
		}
		return json.Unmarshal([]byte(body), run)
	}
	if err = read(&result.Run); err != nil {
		return result, err
	}
	for i := range result.Items {
		if err = read(&result.Items[i].Run); err != nil {
			return result, err
		}
	}
	return result, nil
}
func (m *Manager) StartResolutionBatch(batchID, checkID string) (ResolutionBatchExecution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ResolutionBatchExecution{}, fmt.Errorf("同步服务已关闭")
	}
	existing, err := m.ResolutionBatchExecution(m.ctx, batchID)
	if err == nil {
		return existing, nil
	}
	if err != sql.ErrNoRows {
		return ResolutionBatchExecution{}, err
	}
	if m.active {
		return ResolutionBatchExecution{}, fmt.Errorf("已有同步或核对执行中")
	}
	batch, err := m.ResolutionBatch(m.ctx, batchID)
	if err != nil {
		return ResolutionBatchExecution{}, err
	}
	check, err := m.ResolutionBatchCheck(m.ctx, checkID)
	if err != nil {
		return ResolutionBatchExecution{}, err
	}
	if check.BatchID != batchID || check.Status != "verified" || check.Finished.IsZero() || check.Snapshot == nil || check.Snapshot.Local == nil || check.Snapshot.Remote == nil || len(check.Items) != len(batch.Items) {
		return ResolutionBatchExecution{}, fmt.Errorf("缺少本批次全部通过的完整核对")
	}
	cfg, err := m.config(batch.Config.ID)
	if err != nil || encoded(cfg) != encoded(batch.Config) {
		return ResolutionBatchExecution{}, fmt.Errorf("同步配置已变化")
	}
	var unresolved int
	if err = m.db.QueryRow(`SELECT count(*) FROM sync_steps s JOIN sync_runs r ON r.id=s.run_id WHERE r.config_id=? AND s.status IN ('intent','unknown')`, cfg.ID).Scan(&unresolved); err != nil {
		return ResolutionBatchExecution{}, err
	}
	if unresolved > 0 {
		return ResolutionBatchExecution{}, fmt.Errorf("历史写入结果未知，禁止重发")
	}
	result := ResolutionBatchExecution{BatchID: batchID, CheckID: checkID, Run: Run{ID: newID(), ConfigID: cfg.ID, Status: "running", Started: time.Now().UTC(), StepCount: len(batch.Items)}, Items: []BatchExecutionItem{}}
	decisions := []Resolution{}
	for i, item := range batch.Items {
		if check.Items[i].DecisionID != item.DecisionID || check.Items[i].Status != "verified" {
			return ResolutionBatchExecution{}, fmt.Errorf("逐项核对不一致")
		}
		d, e := m.Resolution(m.ctx, item.DecisionID)
		if e != nil {
			return ResolutionBatchExecution{}, e
		}
		if d.SourceRun != batch.SourceRun || encoded(d.Config) != encoded(cfg) || d.SnapshotSHA256 != item.SnapshotSHA256 || d.Choice != item.Choice || d.Conflict.Step.Path != item.Root || d.LocalCopy != item.CopyPath {
			return ResolutionBatchExecution{}, fmt.Errorf("批次来源不一致")
		}
		count := len(d.Effects)
		if d.State == "review_required" {
			steps, e := m.subtreeExecutionSteps(m.ctx, d)
			if e != nil {
				return ResolutionBatchExecution{}, e
			}
			count = len(steps)
		} else if d.State != "preview" || count == 0 {
			return ResolutionBatchExecution{}, fmt.Errorf("决策不可执行")
		}
		if strconv.Itoa(count) != item.StepCount {
			return ResolutionBatchExecution{}, fmt.Errorf("固定步骤总数不一致")
		}
		decisions = append(decisions, d)
		result.Items = append(result.Items, BatchExecutionItem{DecisionID: d.ID, Run: Run{ID: newID(), ConfigID: cfg.ID, Status: "running", Started: result.Run.Started, StepCount: count}})
	}
	tx, err := m.db.Begin()
	if err != nil {
		return ResolutionBatchExecution{}, err
	}
	defer tx.Rollback()
	insert := func(run Run) error {
		_, e := tx.Exec(`INSERT INTO sync_runs VALUES(?,?,?,?)`, run.ID, cfg.ID, run.Status, encoded(run))
		return e
	}
	event := func(run, kind string, evidence any) error {
		_, e := tx.Exec(`INSERT INTO sync_events(run_id,kind,recorded,evidence) VALUES(?,?,?,?)`, run, kind, result.Run.Started.Format(time.RFC3339Nano), encoded(evidence))
		return e
	}
	if err = insert(result.Run); err != nil {
		return ResolutionBatchExecution{}, err
	}
	if _, err = tx.Exec(`INSERT INTO sync_resolution_batch_executions VALUES(?,?,?)`, batchID, result.Run.ID, encoded(result)); err != nil {
		return ResolutionBatchExecution{}, err
	}
	if err = event(result.Run.ID, "run_started", cfg); err != nil {
		return ResolutionBatchExecution{}, err
	}
	if err = event(result.Run.ID, "batch_admitted", result); err != nil {
		return ResolutionBatchExecution{}, err
	}
	for i, item := range result.Items {
		if err = insert(item.Run); err != nil {
			return ResolutionBatchExecution{}, err
		}
		if _, err = tx.Exec(`INSERT INTO sync_resolution_executions VALUES(?,?)`, item.DecisionID, item.Run.ID); err != nil {
			return ResolutionBatchExecution{}, fmt.Errorf("决策已准入或无法保存：%w", err)
		}
		if err = event(item.Run.ID, "run_started", cfg); err != nil {
			return ResolutionBatchExecution{}, err
		}
		if err = event(item.Run.ID, "batch_item_reserved", map[string]any{"batch_id": batchID, "parent_run": result.Run.ID, "ordinal": i + 1, "decision_id": item.DecisionID}); err != nil {
			return ResolutionBatchExecution{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return ResolutionBatchExecution{}, err
	}
	m.active = true
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() { m.mu.Lock(); m.active = false; m.mu.Unlock() }()
		m.executeResolutionBatch(batch, result, decisions)
	}()
	return result, nil
}
func (m *Manager) executeResolutionBatch(batch ResolutionBatch, execution ResolutionBatchExecution, decisions []Resolution) {
	parent := execution.Run
	stop := func(status string, cause error, remaining int) {
		for i := remaining; i < len(execution.Items); i++ {
			run := execution.Items[i].Run
			run.Status = "interrupted"
			run.Error = "批次在本项操作前停止；未自动执行"
			if err := m.event(run.ID, "batch_item_not_started", map[string]string{"batch_id": batch.ID}); err != nil {
				status = "unknown"
				cause = err
			}
			if err := m.finish(run); err != nil {
				status = "unknown"
				cause = err
			}
		}
		parent.Status = status
		if cause != nil {
			parent.Error = cause.Error()
		}
		if err := m.finish(parent); err != nil {
			_ = m.event(parent.ID, "persistence_failure", map[string]string{"error": err.Error()})
		}
	}
	current, err := m.snapshot(WithRunID(m.ctx, parent.ID), batch.Config, nil)
	if err != nil {
		stop("failed", err, 0)
		return
	}
	if current.Local == nil || current.Remote == nil {
		stop("failed", fmt.Errorf("当前完整观察证据缺失"), 0)
		return
	}
	for _, d := range decisions {
		if reason, _ := compareResolutionSnapshot(d, current); reason != "" {
			stop("conflict", fmt.Errorf("执行前批次条目已变化：%s", reason), 0)
			return
		}
	}
	if err = m.event(parent.ID, "snapshot_before", current); err != nil {
		stop("failed", err, 0)
		return
	}
	for i, d := range decisions {
		if err = m.ctx.Err(); err != nil {
			stop("interrupted", err, i)
			return
		}
		if err = m.event(execution.Items[i].Run.ID, "batch_item_execution_started", map[string]any{"batch_id": batch.ID, "ordinal": i + 1}); err != nil {
			stop("failed", err, i)
			return
		}
		if d.SubtreeReview != nil {
			m.executeSubtreeResolution(d, execution.Items[i].Run)
		} else {
			m.executeResolution(d, execution.Items[i].Run)
		}
		run, e := m.ResolutionExecution(context.Background(), d.ID)
		if e != nil {
			stop("unknown", e, i+1)
			return
		}
		if e = m.event(parent.ID, "batch_item_finished", map[string]any{"decision_id": d.ID, "run": run, "ordinal": i + 1}); e != nil {
			stop("unknown", e, i+1)
			return
		}
		if run.Status != "verification_pending" {
			stop(run.Status, fmt.Errorf("第%d项停止：%s", i+1, run.Error), i+1)
			return
		}
		parent.Confirmed++
		if e = m.progress(parent); e != nil {
			stop("unknown", e, i+1)
			return
		}
	}
	stop("verification_pending", nil, len(execution.Items))
}
