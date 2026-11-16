package foldersync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/directoryanalysis"
)

type ResolutionEffect struct {
	Side   string `json:"side"`
	Action string `json:"action"`
	Path   string `json:"path"`
	From   string `json:"from,omitempty"`
	Backup bool   `json:"backup_required"`
}
type Resolution struct {
	SubtreeReview    *SubtreeReview     `json:"subtree_review,omitempty"`
	SnapshotSequence string             `json:"snapshot_event_sequence"`
	SnapshotRecorded time.Time          `json:"snapshot_recorded_at_utc"`
	SnapshotSHA256   string             `json:"snapshot_evidence_sha256"`
	ID               string             `json:"id"`
	SourceRun        string             `json:"source_run"`
	ConflictSequence string             `json:"conflict_event_sequence"`
	SourceRecorded   time.Time          `json:"source_recorded_at_utc"`
	Created          time.Time          `json:"created_at_utc"`
	Config           Config             `json:"source_config"`
	Conflict         StepRecord         `json:"source_conflict"`
	Choice           string             `json:"choice"`
	State            string             `json:"state"`
	Note             string             `json:"note"`
	LocalCopy        string             `json:"local_copy_path,omitempty"`
	Effects          []ResolutionEffect `json:"effects"`
}

func (m *Manager) initResolutions() error {
	_, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS sync_resolutions(id TEXT PRIMARY KEY,source_run TEXT NOT NULL,body TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS sync_resolution_run ON sync_resolutions(source_run);
 CREATE TRIGGER IF NOT EXISTS sync_resolution_no_update BEFORE UPDATE ON sync_resolutions BEGIN SELECT RAISE(ABORT,'resolution evidence is immutable'); END;
 CREATE TRIGGER IF NOT EXISTS sync_resolution_no_delete BEFORE DELETE ON sync_resolutions BEGIN SELECT RAISE(ABORT,'resolution evidence is immutable'); END;`)
	return err
}

// CreateResolution saves a historical decision preview only. It never checks
// current files, performs writes on either side, or advances the common baseline.
func (m *Manager) CreateResolution(ctx context.Context, run string, ordinal int, choice string) (Resolution, error) {
	empty := Resolution{}
	if ordinal < 1 {
		return empty, fmt.Errorf("冲突序号无效")
	}
	switch choice {
	case "keep_local", "keep_remote", "keep_both", "skip":
	default:
		return empty, fmt.Errorf("冲突处理方案无效")
	}
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	var conflictBody, configBody, snapshotBody, recorded string
	var sequence int64
	if err = tx.QueryRowContext(ctx, `SELECT sequence,recorded,evidence FROM sync_events WHERE run_id=? AND kind='step_conflict' AND json_extract(evidence,'$.ordinal')=? ORDER BY sequence LIMIT 1`, run, ordinal).Scan(&sequence, &recorded, &conflictBody); err != nil {
		return empty, err
	}
	var conflict StepRecord
	if json.Unmarshal([]byte(conflictBody), &conflict) != nil || conflict.Ordinal != ordinal || conflict.Status != "conflict" || conflict.Step.Action != "conflict" || !safeRelative(conflict.Step.Path) {
		return empty, fmt.Errorf("缺少有效原始冲突证据")
	}
	if err = tx.QueryRowContext(ctx, `SELECT evidence FROM sync_events WHERE run_id=? AND kind='run_started' ORDER BY sequence LIMIT 1`, run).Scan(&configBody); err != nil {
		return empty, err
	}
	var config Config
	if json.Unmarshal([]byte(configBody), &config) != nil || config.ID == "" || config.Account == "" || config.LocalRoot == "" || config.RemoteRoot == "" || (config.Mode != "pull" && config.Mode != "both") {
		return empty, fmt.Errorf("原始同步范围证据无效")
	}
	var snapshotSequence int64
	var snapshotRecorded string
	if err = tx.QueryRowContext(ctx, `SELECT sequence,recorded,evidence FROM sync_events WHERE run_id=? AND kind='snapshot_before' ORDER BY sequence LIMIT 1`, run).Scan(&snapshotSequence, &snapshotRecorded, &snapshotBody); err != nil {
		return empty, err
	}
	var snapshot Snapshot
	if json.Unmarshal([]byte(snapshotBody), &snapshot) != nil || snapshot.Local == nil || snapshot.Remote == nil {
		return empty, fmt.Errorf("原始遍历快照不完整")
	}
	if _, err = directoryanalysis.Compare(analysisInput(snapshot.Local, "本地", config.LocalRoot), analysisInput(snapshot.Remote, "网盘", config.RemoteRoot)); err != nil {
		return empty, err
	}
	snapshotAt, err := time.Parse(time.RFC3339Nano, snapshotRecorded)
	if err != nil || snapshotSequence >= sequence {
		return empty, fmt.Errorf("来源快照时间或事件顺序无效")
	}
	at, err := time.Parse(time.RFC3339Nano, recorded)
	if err != nil {
		return empty, err
	}
	r := Resolution{ID: newID(), SourceRun: run, ConflictSequence: strconv.FormatInt(sequence, 10), SourceRecorded: at, Created: time.Now().UTC(), Config: config, Conflict: conflict, Choice: choice, State: "preview", Note: "历史决策预览；尚未检查当前两端或执行，确认执行前必须重新核对。", Effects: []ResolutionEffect{}}
	digest := sha256.Sum256([]byte(snapshotBody))
	r.SnapshotSequence = strconv.FormatInt(snapshotSequence, 10)
	r.SnapshotRecorded = snapshotAt
	r.SnapshotSHA256 = hex.EncodeToString(digest[:])
	l, remote := conflict.Step.Local, conflict.Step.Remote
	for _, pair := range []struct {
		entry *Entry
		all   []Entry
	}{{l, snapshot.Local}, {remote, snapshot.Remote}} {
		original := index(pair.all)[conflict.Step.Path]
		if !same(pair.entry, original) || (pair.entry != nil && pair.entry.ID != original.ID) {
			return empty, fmt.Errorf("冲突对象与原始快照不一致")
		}
	}
	var orderedSubtreeSteps []Step
	var subtreePlanErr error
	if l == nil || remote == nil || l.Directory || remote.Directory {
		if choice == "keep_both" && l != nil && remote != nil {
			r.LocalCopy = path.Join(path.Dir(conflict.Step.Path), path.Base(conflict.Step.Path)+".conflict-local-"+r.ID[:12])
			for _, entry := range append(append([]Entry{}, snapshot.Local...), snapshot.Remote...) {
				if strings.EqualFold(entry.Path, r.LocalCopy) || strings.HasPrefix(strings.ToLower(entry.Path), strings.ToLower(r.LocalCopy)+"/") {
					return empty, fmt.Errorf("固定副本目录已占用，请重新预检")
				}
			}
		}
		r.SubtreeReview, err = BuildSubtreeReview(config.Mode, choice, conflict.Step.Path, r.LocalCopy, snapshot)
		if err != nil {
			return empty, err
		}
		orderedSubtreeSteps, subtreePlanErr = CompileSubtreePlan(config.Mode, choice, r.LocalCopy, r.SubtreeReview)
		if subtreePlanErr == nil {
			r.SubtreeReview.PlanVersion = "subtree-steps-v1"
			r.SubtreeReview.PlanStepCount = strconv.Itoa(len(orderedSubtreeSteps))
		}
	}
	switch {
	case choice == "skip":
		r.State = "deferred"
		r.Note = "暂不处理；保留原冲突和共同基线。"
	case choice == "keep_local" && config.Mode == "pull":
		r.State = "unresolved"
		r.Note = "网盘到本地模式不会上传本地变化，继续保留未解决冲突。"
	case l == nil || remote == nil || l.Directory || remote.Directory:
		r.State = "review_required"
		r.Note = "已展开目录、删除或类型冲突的子项影响；仍需生成有序备份及执行步骤，当前不执行操作。"
		if subtreePlanErr != nil {
			r.Note = "子树影响已保存；有序计划证据不足：" + subtreePlanErr.Error()
		} else {
			r.Note = "已保存固定有序步骤；需重新核对完整子树并明确确认后执行，完成后仍须整组复核。"
		}
	default:
		if l.SHA256 == "" || remote.SHA256 == "" {
			return empty, fmt.Errorf("缺少两端内容摘要")
		}
		if choice == "keep_remote" {
			r.Effects = append(r.Effects, ResolutionEffect{Side: "local", Action: "download", Path: conflict.Step.Path, Backup: true})
		}
		if choice == "keep_local" {
			r.Effects = append(r.Effects, ResolutionEffect{Side: "remote", Action: "upload", Path: conflict.Step.Path, Backup: true})
		}
		if choice == "keep_both" {
			r.LocalCopy = path.Join(path.Dir(conflict.Step.Path), path.Base(conflict.Step.Path)+".conflict-local-"+r.ID[:12])
			if !safeRelative(r.LocalCopy) {
				return empty, fmt.Errorf("冲突副本路径无效")
			}
			for _, e := range append(append([]Entry{}, snapshot.Local...), snapshot.Remote...) {
				if strings.EqualFold(e.Path, r.LocalCopy) {
					return empty, fmt.Errorf("固定副本路径已被占用，请重新预检")
				}
			}
			r.Effects = append(r.Effects, ResolutionEffect{Side: "local", Action: "move", From: conflict.Step.Path, Path: r.LocalCopy}, ResolutionEffect{Side: "local", Action: "download", Path: conflict.Step.Path})
			if config.Mode == "both" {
				r.Effects = append(r.Effects, ResolutionEffect{Side: "remote", Action: "upload", From: r.LocalCopy, Path: r.LocalCopy})
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sync_resolutions VALUES(?,?,?)`, r.ID, run, encoded(r)); err != nil {
		return empty, err
	}
	for i, step := range orderedSubtreeSteps {
		if _, err = tx.ExecContext(ctx, `INSERT INTO sync_resolution_subtree_steps VALUES(?,?,?)`, r.ID, i+1, encoded(step)); err != nil {
			return empty, err
		}
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return r, nil
}

func (m *Manager) Resolution(ctx context.Context, id string) (Resolution, error) {
	var r Resolution
	var body string
	err := m.db.QueryRowContext(ctx, `SELECT body FROM sync_resolutions WHERE id=?`, id).Scan(&body)
	if err != nil {
		return r, err
	}
	err = json.Unmarshal([]byte(body), &r)
	return r, err
}

func (m *Manager) Resolutions(ctx context.Context, run string, ordinal, offset int) ([]Resolution, bool, error) {
	if ordinal < 1 || offset < 0 || offset > 1000000 {
		return nil, false, fmt.Errorf("决策分页无效")
	}
	rows, err := m.db.QueryContext(ctx, `SELECT `+resolutionSummarySQL+` FROM sync_resolutions WHERE source_run=? AND json_extract(body,'$.source_conflict.ordinal')=? ORDER BY rowid DESC LIMIT 26 OFFSET ?`, run, ordinal, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	result := []Resolution{}
	for rows.Next() {
		var body string
		var decision Resolution
		if err = rows.Scan(&body); err != nil {
			return nil, false, err
		}
		if err = json.Unmarshal([]byte(body), &decision); err != nil {
			return nil, false, err
		}
		result = append(result, decision)
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
