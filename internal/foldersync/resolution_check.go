package foldersync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type ResolutionCheck struct {
	CheckScope         string                `json:"check_scope,omitempty"`
	ExecutionSupported *bool                 `json:"execution_supported,omitempty"`
	Subtree            *SubtreeCheckEvidence `json:"subtree,omitempty"`
	ID                 string                `json:"id"`
	DecisionID         string                `json:"decision_id"`
	Started            time.Time             `json:"started_at_utc"`
	Finished           time.Time             `json:"finished_at_utc"`
	Status             string                `json:"status"`
	Reason             string                `json:"reason"`
	Snapshot           *Snapshot             `json:"snapshot,omitempty"`
}

func (m *Manager) initResolutionChecks() error {
	_, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS sync_resolution_check_intents(id TEXT PRIMARY KEY,decision_id TEXT NOT NULL,body TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS sync_resolution_checks(id TEXT PRIMARY KEY,decision_id TEXT NOT NULL,body TEXT NOT NULL);
 CREATE TRIGGER IF NOT EXISTS sync_resolution_check_intent_no_update BEFORE UPDATE ON sync_resolution_check_intents BEGIN SELECT RAISE(ABORT,'check intent is immutable'); END;
 CREATE TRIGGER IF NOT EXISTS sync_resolution_check_intent_no_delete BEFORE DELETE ON sync_resolution_check_intents BEGIN SELECT RAISE(ABORT,'check intent is immutable'); END;
 CREATE TRIGGER IF NOT EXISTS sync_resolution_check_no_update BEFORE UPDATE ON sync_resolution_checks BEGIN SELECT RAISE(ABORT,'check evidence is immutable'); END;
 CREATE TRIGGER IF NOT EXISTS sync_resolution_check_no_delete BEFORE DELETE ON sync_resolution_checks BEGIN SELECT RAISE(ABORT,'check evidence is immutable'); END;`)
	return err
}

// A verified result is a timestamped observation, never permission to execute
// later without another identity/content check. Reads cannot advance a baseline.
func (m *Manager) CheckResolution(ctx context.Context, id string) (ResolutionCheck, error) {
	m.mu.Lock()
	if m.closed || m.active {
		m.mu.Unlock()
		return ResolutionCheck{}, fmt.Errorf("已有同步或核对执行中")
	}
	m.active = true
	m.wg.Add(1)
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.active = false; m.mu.Unlock(); m.wg.Done() }()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(m.ctx, cancel)
	defer stop()
	decision, err := m.Resolution(ctx, id)
	if err != nil {
		return ResolutionCheck{}, err
	}
	result := ResolutionCheck{ID: newID(), DecisionID: id, Started: time.Now().UTC(), Status: "incomplete", Reason: "已记录核对开始；尚无完成证据，不自动恢复或执行。"}
	subtree := decision.State == "review_required" && decision.SubtreeReview != nil && decision.SubtreeReview.PlanVersion == "subtree-steps-v1"
	if subtree {
		result.CheckScope = "subtree"
	} else {
		result.CheckScope = "file"
	}
	canExecute := decision.State == "preview" && len(decision.Effects) > 0
	if subtree {
		_, e := m.subtreeExecutionSteps(ctx, decision)
		canExecute = e == nil
	}
	result.ExecutionSupported = &canExecute
	if _, err = m.db.ExecContext(ctx, `INSERT INTO sync_resolution_check_intents VALUES(?,?,?)`, result.ID, id, encoded(result)); err != nil {
		return ResolutionCheck{}, err
	}
	finish := func(status, reason string) (ResolutionCheck, error) {
		result.Status = status
		result.Reason = reason
		result.Finished = time.Now().UTC()
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_, err := m.db.ExecContext(persist, `INSERT INTO sync_resolution_checks VALUES(?,?,?)`, result.ID, id, encoded(result))
		return result, err
	}
	if !canExecute && !subtree {
		return finish("rejected", "decision_not_executable")
	}
	if subtree {
		expected, parseErr := strconv.Atoi(decision.SubtreeReview.PlanStepCount)
		var count int
		if parseErr != nil || expected < 1 {
			return finish("rejected", "subtree_plan_incomplete")
		}
		if err = m.db.QueryRowContext(ctx, `SELECT count(*) FROM sync_resolution_subtree_steps WHERE decision_id=?`, id).Scan(&count); err != nil {
			return finish("failed", "history_check_failed")
		}
		if count != expected {
			return finish("rejected", "subtree_plan_incomplete")
		}
	}
	current, err := m.config(decision.Config.ID)
	if err != nil {
		return finish("rejected", "configuration_missing")
	}
	if encoded(current) != encoded(decision.Config) {
		return finish("rejected", "configuration_changed")
	}
	var unresolved int
	if err = m.db.QueryRowContext(ctx, `SELECT count(*) FROM sync_steps s JOIN sync_runs r ON r.id=s.run_id WHERE r.config_id=? AND s.status IN ('intent','unknown')`, current.ID).Scan(&unresolved); err != nil {
		return finish("failed", "history_check_failed")
	}
	if unresolved > 0 {
		return finish("rejected", "unresolved_previous_write")
	}
	// Passing no cached hashes forces fresh remote content verification for this
	// explicitly selected synchronization root, instead of trusting old metadata.
	snapshot, err := m.snapshot(WithRunID(ctx, result.ID), current, nil)
	if err != nil {
		if ctx.Err() != nil {
			return finish("failed", "check_cancelled")
		}
		return finish("failed", "current_scan_failed")
	}
	result.Snapshot = &snapshot
	reason, proof := compareResolutionSnapshot(decision, snapshot)
	result.Subtree = proof
	if reason != "" {
		return finish("rejected", reason)
	}
	return finish("verified", "current_sources_match_preview")
}

func (m *Manager) ResolutionCheck(ctx context.Context, id string) (ResolutionCheck, error) {
	var result ResolutionCheck
	var body string
	err := m.db.QueryRowContext(ctx, `SELECT body FROM sync_resolution_checks WHERE id=?`, id).Scan(&body)
	if err == sql.ErrNoRows {
		err = m.db.QueryRowContext(ctx, `SELECT body FROM sync_resolution_check_intents WHERE id=?`, id).Scan(&body)
	}
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(body), &result)
	return result, err
}

func (m *Manager) ResolutionChecks(ctx context.Context, decision string, offset int) ([]ResolutionCheck, bool, error) {
	if offset < 0 || offset > 1000000 {
		return nil, false, fmt.Errorf("核对分页无效")
	}
	rows, err := m.db.QueryContext(ctx, `SELECT coalesce(r.body,i.body) FROM sync_resolution_check_intents i LEFT JOIN sync_resolution_checks r ON r.id=i.id WHERE i.decision_id=? ORDER BY i.rowid DESC LIMIT 26 OFFSET ?`, decision, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	result := []ResolutionCheck{}
	for rows.Next() {
		var body string
		var check ResolutionCheck
		if err = rows.Scan(&body); err != nil {
			return nil, false, err
		}
		if err = json.Unmarshal([]byte(body), &check); err != nil {
			return nil, false, err
		}
		check.Snapshot = nil
		result = append(result, check)
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

func compareResolutionSnapshot(decision Resolution, snapshot Snapshot) (string, *SubtreeCheckEvidence) {
	subtree := decision.SubtreeReview != nil && decision.State == "review_required"
	var proof *SubtreeCheckEvidence
	if subtree {
		proof = subtreeCheckEvidence(decision, snapshot)
		if proof.LocalExpectedSHA256 != proof.LocalObservedSHA256 {
			return "local_subtree_changed", proof
		}
		if proof.RemoteExpectedSHA256 != proof.RemoteObservedSHA256 {
			return "remote_subtree_changed", proof
		}
	}
	expected := decision.Conflict.Step
	for _, pair := range []struct {
		side    string
		want    *Entry
		entries []Entry
	}{{"local", expected.Local, snapshot.Local}, {"remote", expected.Remote, snapshot.Remote}} {
		got := index(pair.entries)[expected.Path]
		if !subtree && (!same(pair.want, got) || (pair.want != nil && pair.want.ID != got.ID)) {
			return pair.side + "_changed", proof
		}
	}
	if decision.LocalCopy != "" {
		for _, entry := range append(append([]Entry{}, snapshot.Local...), snapshot.Remote...) {
			if strings.EqualFold(entry.Path, decision.LocalCopy) || strings.HasPrefix(strings.ToLower(entry.Path), strings.ToLower(decision.LocalCopy)+"/") {
				return "copy_path_occupied", proof
			}
		}
	}
	return "", proof
}
