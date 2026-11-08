package foldersync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

func (m *Manager) initSubtreePlans() error {
	_, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS sync_resolution_subtree_steps(decision_id TEXT NOT NULL,ordinal INTEGER NOT NULL,body TEXT NOT NULL,PRIMARY KEY(decision_id,ordinal));
 CREATE TRIGGER IF NOT EXISTS sync_subtree_step_no_update BEFORE UPDATE ON sync_resolution_subtree_steps BEGIN SELECT RAISE(ABORT,'subtree step is immutable'); END;
 CREATE TRIGGER IF NOT EXISTS sync_subtree_step_no_delete BEFORE DELETE ON sync_resolution_subtree_steps BEGIN SELECT RAISE(ABORT,'subtree step is immutable'); END;`)
	return err
}

type SubtreeStepsPage struct {
	DecisionID string `json:"decision_id"`
	Version    string `json:"version"`
	Root       string `json:"root"`
	Total      string `json:"total"`
	Offset     int    `json:"offset"`
	Limit      int    `json:"limit"`
	More       bool   `json:"has_more"`
	Steps      []Step `json:"steps"`
}

func (m *Manager) SubtreeStepsPage(ctx context.Context, id string, offset int) (SubtreeStepsPage, error) {
	page := SubtreeStepsPage{DecisionID: id, Offset: offset, Limit: 100, Steps: []Step{}}
	if offset < 0 {
		return page, fmt.Errorf("有序步骤分页无效")
	}
	header, err := m.ResolutionSummary(ctx, id)
	if err != nil {
		return page, err
	}
	if header.SubtreeReview == nil || header.SubtreeReview.PlanVersion != "subtree-steps-v1" {
		return page, sql.ErrNoRows
	}
	page.Version = header.SubtreeReview.PlanVersion
	page.Root = header.SubtreeReview.Root
	page.Total = header.SubtreeReview.PlanStepCount
	total, err := strconv.Atoi(page.Total)
	if err != nil || total < 0 {
		return page, fmt.Errorf("步骤总数证据无效")
	}
	rows, err := m.db.QueryContext(ctx, `SELECT body FROM sync_resolution_subtree_steps WHERE decision_id=? ORDER BY ordinal LIMIT 101 OFFSET ?`, id, offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var body string
		var step Step
		if err = rows.Scan(&body); err != nil {
			return page, err
		}
		if err = json.Unmarshal([]byte(body), &step); err != nil {
			return page, err
		}
		page.Steps = append(page.Steps, step)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	page.More = len(page.Steps) > 100
	if page.More {
		page.Steps = page.Steps[:100]
	}
	expected := total - offset
	if expected < 0 {
		expected = 0
	}
	if expected > 100 {
		expected = 100
	}
	if len(page.Steps) != expected {
		return page, fmt.Errorf("固定步骤缺少证据，不能返回不完整计划")
	}
	return page, nil
}
func (m *Manager) HandleSubtreeSteps(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	q := r.URL.Query()
	offset := 0
	var err error
	if text := q.Get("offset"); text != "" {
		offset, err = strconv.Atoi(text)
	}
	if err != nil || offset < 0 || len(q["offset"]) > 1 {
		http.Error(w, "有序步骤分页无效", 400)
		return
	}
	id := r.PathValue("decision")
	page, err := m.SubtreeStepsPage(r.Context(), id, offset)
	if err != nil {
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
		} else {
			http.Error(w, "固定步骤读取失败", 500)
		}
		return
	}
	if q.Get("export") == "1" {
		first, err := m.SubtreeStepsPage(r.Context(), id, 0)
		if err != nil {
			http.Error(w, "完整步骤导出失败", 500)
			return
		}
		all := first.Steps
		for first.More {
			first, err = m.SubtreeStepsPage(r.Context(), id, len(all))
			if err != nil {
				http.Error(w, "完整步骤导出失败", 500)
				return
			}
			all = append(all, first.Steps...)
		}
		header, err := m.ResolutionSummary(r.Context(), id)
		if err != nil {
			http.Error(w, "步骤来源读取失败", 500)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="subtree-plan-`+id+`.json"`)
		reply(w, map[string]any{"decision": header, "version": page.Version, "step_count": page.Total, "steps": all})
		return
	}
	reply(w, page)
}
