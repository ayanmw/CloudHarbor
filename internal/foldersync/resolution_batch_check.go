package foldersync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type ResolutionBatchCheckItem struct {
	DecisionID string                `json:"decision_id"`
	Status     string                `json:"status"`
	Reason     string                `json:"reason"`
	Subtree    *SubtreeCheckEvidence `json:"subtree,omitempty"`
}
type ResolutionBatchCheck struct {
	ID       string                     `json:"id"`
	BatchID  string                     `json:"batch_id"`
	Started  time.Time                  `json:"started_at_utc"`
	Finished time.Time                  `json:"finished_at_utc"`
	Status   string                     `json:"status"`
	Reason   string                     `json:"reason"`
	Snapshot *Snapshot                  `json:"snapshot,omitempty"`
	Items    []ResolutionBatchCheckItem `json:"items"`
}

func (m *Manager) initBatchChecks() error {
	_, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS sync_resolution_batch_check_intents(id TEXT PRIMARY KEY,batch_id TEXT NOT NULL,body TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS sync_resolution_batch_checks(id TEXT PRIMARY KEY,batch_id TEXT NOT NULL,body TEXT NOT NULL);
 CREATE TRIGGER IF NOT EXISTS sync_batch_check_intent_no_update BEFORE UPDATE ON sync_resolution_batch_check_intents BEGIN SELECT RAISE(ABORT,'batch check intent is immutable'); END;
 CREATE TRIGGER IF NOT EXISTS sync_batch_check_intent_no_delete BEFORE DELETE ON sync_resolution_batch_check_intents BEGIN SELECT RAISE(ABORT,'batch check intent is immutable'); END;
 CREATE TRIGGER IF NOT EXISTS sync_batch_check_no_update BEFORE UPDATE ON sync_resolution_batch_checks BEGIN SELECT RAISE(ABORT,'batch check result is immutable'); END;
 CREATE TRIGGER IF NOT EXISTS sync_batch_check_no_delete BEFORE DELETE ON sync_resolution_batch_checks BEGIN SELECT RAISE(ABORT,'batch check result is immutable'); END;`)
	return err
}
func (m *Manager) CheckResolutionBatch(ctx context.Context, id string) (ResolutionBatchCheck, error) {
	m.mu.Lock()
	if m.closed || m.active {
		m.mu.Unlock()
		return ResolutionBatchCheck{}, fmt.Errorf("已有同步或核对执行中")
	}
	m.active = true
	m.wg.Add(1)
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.active = false; m.mu.Unlock(); m.wg.Done() }()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(m.ctx, cancel)
	defer stop()
	batch, err := m.ResolutionBatch(ctx, id)
	if err != nil {
		return ResolutionBatchCheck{}, err
	}
	result := ResolutionBatchCheck{ID: newID(), BatchID: id, Started: time.Now().UTC(), Status: "incomplete", Reason: "awaiting_completion", Items: []ResolutionBatchCheckItem{}}
	for _, item := range batch.Items {
		result.Items = append(result.Items, ResolutionBatchCheckItem{DecisionID: item.DecisionID, Status: "not_checked", Reason: "not_checked"})
	}
	if _, err = m.db.ExecContext(ctx, `INSERT INTO sync_resolution_batch_check_intents VALUES(?,?,?)`, result.ID, id, encoded(result)); err != nil {
		return result, err
	}
	finish := func(status, reason string) (ResolutionBatchCheck, error) {
		result.Status = status
		result.Reason = reason
		result.Finished = time.Now().UTC()
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_, err := m.db.ExecContext(persist, `INSERT INTO sync_resolution_batch_checks VALUES(?,?,?)`, result.ID, id, encoded(result))
		return result, err
	}
	c, err := m.config(batch.Config.ID)
	if err != nil || encoded(c) != encoded(batch.Config) {
		return finish("rejected", "configuration_changed")
	}
	var unresolved int
	if err = m.db.QueryRowContext(ctx, `SELECT count(*) FROM sync_steps s JOIN sync_runs r ON r.id=s.run_id WHERE r.config_id=? AND s.status IN ('intent','unknown')`, c.ID).Scan(&unresolved); err != nil {
		return finish("failed", "history_check_failed")
	}
	if unresolved > 0 {
		return finish("rejected", "unresolved_previous_write")
	}
	decisions := []Resolution{}
	for _, item := range batch.Items {
		d, e := m.Resolution(ctx, item.DecisionID)
		if e != nil {
			return finish("failed", "decision_unavailable")
		}
		if d.SourceRun != batch.SourceRun || encoded(d.Config) != encoded(c) || d.SnapshotSHA256 != item.SnapshotSHA256 || d.Choice != item.Choice || d.Conflict.Step.Path != item.Root || d.LocalCopy != item.CopyPath {
			return finish("rejected", "batch_source_changed")
		}
		var count int
		if e = m.db.QueryRowContext(ctx, `SELECT count(*) FROM sync_resolution_executions WHERE decision_id=?`, d.ID).Scan(&count); e != nil {
			return finish("failed", "history_check_failed")
		}
		if count > 0 {
			return finish("rejected", "decision_already_admitted")
		}
		if d.State == "review_required" {
			if _, e = m.subtreeExecutionSteps(ctx, d); e != nil {
				return finish("rejected", "decision_not_executable")
			}
		} else if d.State != "preview" || len(d.Effects) == 0 {
			return finish("rejected", "decision_not_executable")
		}
		decisions = append(decisions, d)
	}
	snapshot, err := m.snapshot(WithRunID(ctx, result.ID), c, nil)
	if err != nil {
		if ctx.Err() != nil {
			return finish("failed", "check_cancelled")
		}
		return finish("failed", "current_scan_failed")
	}
	if snapshot.Local == nil || snapshot.Remote == nil {
		return finish("failed", "current_scan_incomplete")
	}
	result.Snapshot = &snapshot
	rejected := false
	for i, d := range decisions {
		reason, proof := compareResolutionSnapshot(d, snapshot)
		item := &result.Items[i]
		item.Subtree = proof
		item.Status = "verified"
		item.Reason = "current_sources_match_preview"
		if reason != "" {
			item.Status = "rejected"
			item.Reason = reason
			rejected = true
		}
	}
	if rejected {
		return finish("rejected", "one_or_more_items_changed")
	}
	return finish("verified", "all_current_sources_match")
}
func (m *Manager) ResolutionBatchCheck(ctx context.Context, id string) (ResolutionBatchCheck, error) {
	var result ResolutionBatchCheck
	var body string
	err := m.db.QueryRowContext(ctx, `SELECT body FROM sync_resolution_batch_checks WHERE id=?`, id).Scan(&body)
	if err == sql.ErrNoRows {
		err = m.db.QueryRowContext(ctx, `SELECT body FROM sync_resolution_batch_check_intents WHERE id=?`, id).Scan(&body)
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(body), &result)
	return result, err
}
func (m *Manager) ResolutionBatchChecks(ctx context.Context, batch string, offset int) ([]ResolutionBatchCheck, bool, error) {
	if offset < 0 || offset > 1000000 {
		return nil, false, fmt.Errorf("批次核对分页无效")
	}
	rows, err := m.db.QueryContext(ctx, `SELECT coalesce(r.body,i.body) FROM sync_resolution_batch_check_intents i LEFT JOIN sync_resolution_batch_checks r ON r.id=i.id WHERE i.batch_id=? ORDER BY i.rowid DESC LIMIT 26 OFFSET ?`, batch, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	result := []ResolutionBatchCheck{}
	for rows.Next() {
		var body string
		var item ResolutionBatchCheck
		if err = rows.Scan(&body); err != nil {
			return nil, false, err
		}
		if err = json.Unmarshal([]byte(body), &item); err != nil {
			return nil, false, err
		}
		item.Snapshot = nil
		result = append(result, item)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(result) > 25
	if more {
		result = result[:25]
	}
	return result, more, nil
}
