package foldersync

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type ResolutionBatchItem struct {
	DecisionID       string    `json:"decision_id"`
	Ordinal          int       `json:"conflict_ordinal"`
	Root             string    `json:"root"`
	Choice           string    `json:"choice"`
	CopyPath         string    `json:"copy_path,omitempty"`
	SnapshotSHA256   string    `json:"snapshot_evidence_sha256"`
	SnapshotRecorded time.Time `json:"snapshot_recorded_at_utc"`
	StepCount        string    `json:"step_count"`
}
type ResolutionBatch struct {
	Version   string                `json:"version"`
	ID        string                `json:"id"`
	SourceRun string                `json:"source_run"`
	Config    Config                `json:"source_config"`
	Created   time.Time             `json:"created_at_utc"`
	State     string                `json:"state"`
	StepCount string                `json:"step_count"`
	Items     []ResolutionBatchItem `json:"items"`
}

func (m *Manager) initResolutionBatches() error {
	_, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS sync_resolution_batches(id TEXT PRIMARY KEY,source_run TEXT NOT NULL,body TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS sync_resolution_batch_source ON sync_resolution_batches(source_run);
 CREATE TRIGGER IF NOT EXISTS sync_resolution_batch_no_update BEFORE UPDATE ON sync_resolution_batches BEGIN SELECT RAISE(ABORT,'batch preview is immutable'); END;
 CREATE TRIGGER IF NOT EXISTS sync_resolution_batch_no_delete BEFORE DELETE ON sync_resolution_batches BEGIN SELECT RAISE(ABORT,'batch preview is immutable'); END;`)
	return err
}

// This fixes selection and order only. No current scan, provider mutation,
// execution admission, baseline advancement or restart continuation occurs.
func (m *Manager) CreateResolutionBatch(ctx context.Context, source string, ids []string) (ResolutionBatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	batch := ResolutionBatch{Version: "resolution-batch-v1", ID: newID(), SourceRun: source, Created: time.Now().UTC(), State: "preview", Items: []ResolutionBatchItem{}}
	if m.closed {
		return ResolutionBatch{}, fmt.Errorf("同步服务已关闭")
	}
	if len(ids) < 1 || len(ids) > 20 {
		return ResolutionBatch{}, fmt.Errorf("每批须明确选择1至20项")
	}
	seen := map[string]bool{}
	scopes := []string{}
	var total int64
	for _, id := range ids {
		if seen[id] {
			return ResolutionBatch{}, fmt.Errorf("重复决策")
		}
		seen[id] = true
		d, err := m.Resolution(ctx, id)
		if err != nil {
			return ResolutionBatch{}, err
		}
		if d.SourceRun != source {
			return ResolutionBatch{}, fmt.Errorf("批次必须来自同一冲突运行")
		}
		if len(batch.Items) == 0 {
			current, e := m.config(d.Config.ID)
			if e != nil || encoded(current) != encoded(d.Config) {
				return ResolutionBatch{}, fmt.Errorf("同步配置已变化")
			}
			batch.Config = d.Config
		} else if encoded(batch.Config) != encoded(d.Config) {
			return ResolutionBatch{}, fmt.Errorf("批次配置不一致")
		}
		var admitted int
		if err = m.db.QueryRowContext(ctx, `SELECT count(*) FROM sync_resolution_executions WHERE decision_id=?`, id).Scan(&admitted); err != nil {
			return ResolutionBatch{}, err
		}
		if admitted > 0 {
			return ResolutionBatch{}, fmt.Errorf("决策已有执行批次，请读取原记录")
		}
		steps := len(d.Effects)
		if d.State == "review_required" {
			plan, e := m.subtreeExecutionSteps(ctx, d)
			if e != nil {
				return ResolutionBatch{}, e
			}
			steps = len(plan)
		} else if d.State != "preview" || steps == 0 {
			return ResolutionBatch{}, fmt.Errorf("决策没有可执行固定计划")
		}
		targets := []string{d.Conflict.Step.Path}
		if d.LocalCopy != "" {
			targets = append(targets, d.LocalCopy)
		}
		for _, target := range targets {
			fold := strings.ToLower(target)
			for _, other := range scopes {
				if under(fold, other) || under(other, fold) {
					return ResolutionBatch{}, fmt.Errorf("决策范围或副本目标重叠")
				}
			}
			scopes = append(scopes, fold)
		}
		if int64(steps) > math.MaxInt64-total {
			return ResolutionBatch{}, fmt.Errorf("步骤数量溢出")
		}
		total += int64(steps)
		batch.Items = append(batch.Items, ResolutionBatchItem{DecisionID: d.ID, Ordinal: d.Conflict.Ordinal, Root: d.Conflict.Step.Path, Choice: d.Choice, CopyPath: d.LocalCopy, SnapshotSHA256: d.SnapshotSHA256, SnapshotRecorded: d.SnapshotRecorded, StepCount: strconv.Itoa(steps)})
	}
	batch.StepCount = strconv.FormatInt(total, 10)
	if _, err := m.db.ExecContext(ctx, `INSERT INTO sync_resolution_batches VALUES(?,?,?)`, batch.ID, source, encoded(batch)); err != nil {
		return ResolutionBatch{}, err
	}
	return batch, nil
}
func (m *Manager) ResolutionBatch(ctx context.Context, id string) (ResolutionBatch, error) {
	var result ResolutionBatch
	var body string
	err := m.db.QueryRowContext(ctx, `SELECT body FROM sync_resolution_batches WHERE id=?`, id).Scan(&body)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal([]byte(body), &result)
	return result, err
}
func (m *Manager) ResolutionBatches(ctx context.Context, source string, offset int) ([]ResolutionBatch, bool, error) {
	if offset < 0 || offset > 1000000 {
		return nil, false, fmt.Errorf("批次分页无效")
	}
	rows, err := m.db.QueryContext(ctx, `SELECT body FROM sync_resolution_batches WHERE source_run=? ORDER BY rowid DESC LIMIT 26 OFFSET ?`, source, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	result := []ResolutionBatch{}
	for rows.Next() {
		var body string
		var item ResolutionBatch
		if err = rows.Scan(&body); err != nil {
			return nil, false, err
		}
		if err = json.Unmarshal([]byte(body), &item); err != nil {
			return nil, false, err
		}
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
