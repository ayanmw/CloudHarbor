package foldersync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (m *Manager) initResolutionExecutions() error {
	_, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS sync_resolution_executions(decision_id TEXT PRIMARY KEY,run_id TEXT NOT NULL UNIQUE);
 CREATE TRIGGER IF NOT EXISTS sync_resolution_execution_no_update BEFORE UPDATE ON sync_resolution_executions BEGIN SELECT RAISE(ABORT,'resolution admission is immutable'); END;
 CREATE TRIGGER IF NOT EXISTS sync_resolution_execution_no_delete BEFORE DELETE ON sync_resolution_executions BEGIN SELECT RAISE(ABORT,'resolution admission is immutable'); END;`)
	return err
}

func (m *Manager) ResolutionExecution(ctx context.Context, decision string) (Run, error) {
	var result Run
	var body string
	err := m.db.QueryRowContext(ctx, `SELECT r.body FROM sync_resolution_executions e JOIN sync_runs r ON r.id=e.run_id WHERE e.decision_id=?`, decision).Scan(&body)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(body), &result)
	return result, err
}

// One decision admits at most one run, including failed, interrupted and unknown
// runs. A repeated HTTP request only reads the original durable admission.
func (m *Manager) StartResolution(decisionID, checkID string) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Run{}, fmt.Errorf("同步服务已关闭")
	}
	var body string
	err := m.db.QueryRow(`SELECT r.body FROM sync_resolution_executions e JOIN sync_runs r ON r.id=e.run_id WHERE e.decision_id=?`, decisionID).Scan(&body)
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
	check, err := m.ResolutionCheck(m.ctx, checkID)
	if err != nil {
		return Run{}, err
	}
	subtree := decision.State == "review_required" && decision.SubtreeReview != nil
	stepCount := len(decision.Effects)
	if subtree {
		steps, e := m.subtreeExecutionSteps(m.ctx, decision)
		if e != nil {
			return Run{}, e
		}
		stepCount = len(steps)
		proof := check.Subtree
		if check.CheckScope != "subtree" || check.ExecutionSupported == nil || !*check.ExecutionSupported || proof == nil || proof.SourceSnapshotSHA256 != decision.SnapshotSHA256 || proof.Root != decision.Conflict.Step.Path || proof.LocalExpectedSHA256 != proof.LocalObservedSHA256 || proof.RemoteExpectedSHA256 != proof.RemoteObservedSHA256 {
			return Run{}, fmt.Errorf("缺少完整目录核对证据")
		}
	}
	if (!subtree && (decision.State != "preview" || len(decision.Effects) == 0)) || check.DecisionID != decision.ID || check.Status != "verified" || check.Finished.IsZero() {
		return Run{}, fmt.Errorf("缺少本决策已完成的当前核对；未执行")
	}
	c, err := m.config(decision.Config.ID)
	if err != nil || encoded(c) != encoded(decision.Config) {
		return Run{}, fmt.Errorf("同步配置已变化；未执行")
	}
	var unresolved int
	if err = m.db.QueryRow(`SELECT count(*) FROM sync_steps s JOIN sync_runs r ON r.id=s.run_id WHERE r.config_id=? AND s.status IN ('intent','unknown')`, c.ID).Scan(&unresolved); err != nil {
		return Run{}, err
	}
	if unresolved > 0 {
		return Run{}, fmt.Errorf("历史写入结果未知，禁止重发")
	}
	run := Run{ID: newID(), ConfigID: c.ID, Status: "running", Started: time.Now().UTC(), StepCount: stepCount}
	tx, err := m.db.Begin()
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO sync_runs VALUES(?,?,?,?)`, run.ID, c.ID, run.Status, encoded(run)); err != nil {
		return Run{}, err
	}
	if _, err = tx.Exec(`INSERT INTO sync_resolution_executions VALUES(?,?)`, decision.ID, run.ID); err != nil {
		return Run{}, err
	}
	for _, event := range []struct {
		kind     string
		evidence any
	}{{"run_started", c}, {"resolution_admitted", map[string]string{"decision_id": decision.ID, "check_id": check.ID, "source_run": decision.SourceRun}}} {
		if _, err = tx.Exec(`INSERT INTO sync_events(run_id,kind,recorded,evidence) VALUES(?,?,?,?)`, run.ID, event.kind, run.Started.Format(time.RFC3339Nano), encoded(event.evidence)); err != nil {
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
		if subtree {
			m.executeSubtreeResolution(decision, run)
		} else {
			m.executeResolution(decision, run)
		}
	}()
	return run, nil
}

func (m *Manager) executeResolution(decision Resolution, run Run) {
	ctx := WithRunID(m.ctx, run.ID)
	c := decision.Config
	finish := func(status string, err error) {
		run.Status = status
		if err != nil {
			run.Error = err.Error()
		}
		if e := m.finish(run); e != nil {
			_ = m.event(run.ID, "persistence_failure", map[string]string{"error": e.Error()})
		}
	}
	initial, err := m.snapshot(ctx, c, nil)
	if err != nil {
		finish("failed", err)
		return
	}
	source := decision.Conflict.Step
	for _, pair := range []struct {
		want    *Entry
		entries []Entry
	}{{source.Local, initial.Local}, {source.Remote, initial.Remote}} {
		got := index(pair.entries)[source.Path]
		if !same(pair.want, got) || (pair.want != nil && pair.want.ID != got.ID) {
			finish("conflict", fmt.Errorf("执行前原冲突对象已变化；未执行"))
			return
		}
	}
	if decision.LocalCopy != "" {
		for _, entries := range [][]Entry{initial.Local, initial.Remote} {
			for _, e := range entries {
				if strings.EqualFold(e.Path, decision.LocalCopy) {
					finish("conflict", fmt.Errorf("固定副本路径已占用；未执行"))
					return
				}
			}
		}
	}
	if err = m.event(run.ID, "snapshot_before", initial); err != nil {
		finish("failed", err)
		return
	}
	for i, effect := range decision.Effects {
		if err = m.checkRoots(ctx, c); err != nil {
			finish("failed", err)
			return
		}
		step := Step{Path: effect.Path, From: effect.From, Reason: "明确选择的固定冲突决策：" + decision.ID}
		switch effect.Action {
		case "move":
			step.Action = "local_move"
			step.Local = source.Local
		case "download":
			step.Action = "download"
			step.Remote = source.Remote
			if decision.Choice == "keep_remote" {
				step.Local = source.Local
			}
		case "upload":
			step.Action = "upload"
			step.Local = source.Local
			step.Remote = source.Remote
			if decision.Choice == "keep_both" {
				current, e := localEntry(ctx, c.LocalRoot, decision.LocalCopy)
				if e != nil || !sameContent(current, source.Local) || current.ID != source.Local.ID {
					finish("conflict", fmt.Errorf("保留的本地副本已变化"))
					return
				}
				step.Local = current
				step.Remote = nil
			}
		default:
			finish("failed", fmt.Errorf("固定决策含未知步骤"))
			return
		}
		if step.Local != nil {
			if err = verifyLocalExpected(ctx, c, step.Local); err != nil {
				finish("conflict", err)
				return
			}
		}
		record := StepRecord{Ordinal: i + 1, Step: step, Status: "intent"}
		if err = m.saveStep(run.ID, record); err != nil {
			finish("failed", err)
			return
		}
		var result any
		if step.Action == "upload" {
			result, err = m.remote.Apply(ctx, c, step, run.ID, func(kind string, evidence any) error { return m.event(run.ID, kind, evidence) })
		} else {
			result, err = m.applyLocal(ctx, c, step, run.ID)
		}
		if err != nil {
			record.Status = "unknown"
			var notStarted *NotStartedError
			if errors.As(err, &notStarted) {
				record.Status = "failed"
			}
			record.Error = err.Error()
			if persistErr := m.saveStep(run.ID, record); persistErr != nil {
				finish("unknown", persistErr)
				return
			}
			finish(record.Status, err)
			return
		}
		record.Status = "confirmed"
		record.Result = json.RawMessage(encoded(result))
		if err = m.saveStep(run.ID, record); err != nil {
			finish("unknown", err)
			return
		}
		run.Confirmed++
		if err = m.progress(run); err != nil {
			finish("unknown", err)
			return
		}
	}
	after, err := m.snapshot(ctx, c, nil)
	if err != nil {
		finish("failed", err)
		return
	}
	if err = m.event(run.ID, "snapshot_after", after); err != nil {
		finish("failed", err)
		return
	}
	expected := source.Remote
	if decision.Choice == "keep_local" {
		expected = source.Local
	}
	if !sameContent(index(after.Local)[source.Path], expected) || !sameContent(index(after.Remote)[source.Path], expected) {
		finish("conflict", fmt.Errorf("执行后的两端内容不符合选择，保留原基线"))
		return
	}
	if decision.Choice == "keep_both" {
		if !sameContent(index(after.Local)[decision.LocalCopy], source.Local) || (c.Mode == "both" && !sameContent(index(after.Remote)[decision.LocalCopy], source.Local)) {
			finish("conflict", fmt.Errorf("保留副本复核未通过，保留原基线"))
			return
		}
	}
	// Applying the chosen item is not proof that all other conflicts are solved.
	// A separate complete synchronization verification must commit the baseline.
	finish("verification_pending", nil)
}
