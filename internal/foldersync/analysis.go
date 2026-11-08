package foldersync

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
	"web_browser_baidu_onekey_upload/internal/directoryanalysis"
)

type Analysis struct {
	SourceKind      string                   `json:"source_kind,omitempty"`
	CacheGeneration string                   `json:"cache_generation,omitempty"`
	ID              string                   `json:"id"`
	SourceRun       string                   `json:"source_run"`
	SourceConfig    string                   `json:"source_config"`
	SourceScope     string                   `json:"source_scope"`
	SourceMode      string                   `json:"source_mode"`
	SourceSequence  string                   `json:"source_event_sequence"`
	SourceRecorded  time.Time                `json:"source_recorded_at_utc"`
	Created         time.Time                `json:"created_at_utc"`
	LeftRoot        string                   `json:"left_root"`
	RightRoot       string                   `json:"right_root"`
	Historical      bool                     `json:"historical"`
	Summary         directoryanalysis.Report `json:"summary"`
	RowCount        string                   `json:"row_count"`
	GroupCount      string                   `json:"group_count"`
}
type AnalysisPage struct {
	Analysis Analysis                `json:"analysis"`
	Rows     []directoryanalysis.Row `json:"rows"`
	Offset   int                     `json:"offset"`
	More     bool                    `json:"has_more"`
	Matched  string                  `json:"matched"`
	Kind     string                  `json:"kind"`
}
type AnalysisGroup struct {
	Ordinal             int                        `json:"ordinal"`
	Key                 string                     `json:"key"`
	SHA256              string                     `json:"sha256"`
	Size                string                     `json:"size"`
	RedundantBytesLeft  string                     `json:"redundant_bytes_left"`
	RedundantBytesRight string                     `json:"redundant_bytes_right"`
	MemberCount         string                     `json:"member_count"`
	Members             []directoryanalysis.Member `json:"members"`
	MembersMore         bool                       `json:"members_have_more"`
	CapacityBasis       string                     `json:"capacity_basis"`
}

func (m *Manager) initAnalyses() error {
	_, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS directory_analysis_reports(id TEXT PRIMARY KEY,run_id TEXT NOT NULL,created TEXT NOT NULL,body TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS directory_analysis_rows(report_id TEXT NOT NULL,ordinal INTEGER NOT NULL,kind TEXT NOT NULL,body TEXT NOT NULL,PRIMARY KEY(report_id,ordinal));
CREATE INDEX IF NOT EXISTS directory_analysis_row_kind ON directory_analysis_rows(report_id,kind,ordinal);
CREATE TABLE IF NOT EXISTS directory_analysis_groups(report_id TEXT NOT NULL,ordinal INTEGER NOT NULL,body TEXT NOT NULL,PRIMARY KEY(report_id,ordinal));
CREATE TABLE IF NOT EXISTS directory_analysis_members(report_id TEXT NOT NULL,group_ordinal INTEGER NOT NULL,ordinal INTEGER NOT NULL,body TEXT NOT NULL,PRIMARY KEY(report_id,group_ordinal,ordinal));
CREATE TRIGGER IF NOT EXISTS directory_analysis_no_update BEFORE UPDATE ON directory_analysis_reports BEGIN SELECT RAISE(ABORT,'analysis is immutable'); END;
CREATE TRIGGER IF NOT EXISTS directory_analysis_no_delete BEFORE DELETE ON directory_analysis_reports BEGIN SELECT RAISE(ABORT,'analysis is immutable'); END;`)
	if err != nil {
		return err
	}
	for _, table := range []string{"directory_analysis_rows", "directory_analysis_groups", "directory_analysis_members"} {
		for _, operation := range []string{"UPDATE", "DELETE"} {
			if _, err = m.db.Exec(`CREATE TRIGGER IF NOT EXISTS ` + table + `_no_` + operation + ` BEFORE ` + operation + ` ON ` + table + ` BEGIN SELECT RAISE(ABORT,'analysis is immutable'); END;`); err != nil {
				return err
			}
		}
	}
	return err
}

func analysisInput(entries []Entry, label, root string) directoryanalysis.Input {
	input := directoryanalysis.Input{Label: label, Root: root, Complete: true, Entries: []directoryanalysis.Entry{}}
	for _, e := range entries {
		var observed *time.Time
		if !e.Observed.IsZero() {
			copy := e.Observed
			observed = &copy
		}
		input.Entries = append(input.Entries, directoryanalysis.Entry{Path: e.Path, ID: e.ID, Directory: e.Directory, Size: strconv.FormatInt(e.Size, 10), SHA256: e.SHA256, Observed: observed})
	}
	return input
}

// AnalyzeRun only reads a completed traversal observation. It never scans a
// directory, advances a synchronization baseline, or executes a planned step.
func (m *Manager) AnalyzeRun(ctx context.Context, run string) (Analysis, error) {
	var snapshot Snapshot
	var config Config
	var body, recorded, configBody string
	var sequence int64
	err := m.db.QueryRowContext(ctx, `SELECT sequence,recorded,evidence FROM sync_events WHERE run_id=? AND kind='snapshot_before' ORDER BY sequence LIMIT 1`, run).Scan(&sequence, &recorded, &body)
	if err != nil {
		return Analysis{}, err
	}
	var envelope struct {
		Local  json.RawMessage `json:"local"`
		Remote json.RawMessage `json:"remote"`
	}
	if json.Unmarshal([]byte(body), &envelope) != nil || len(envelope.Local) == 0 || len(envelope.Remote) == 0 || envelope.Local[0] != '[' || envelope.Remote[0] != '[' {
		return Analysis{}, fmt.Errorf("缺少完整的两端遍历快照，不能将缺失证据当作空目录")
	}
	if err = json.Unmarshal([]byte(body), &snapshot); err != nil {
		return Analysis{}, fmt.Errorf("目录快照不可读")
	}
	if err = m.db.QueryRowContext(ctx, `SELECT evidence FROM sync_events WHERE run_id=? AND kind='run_started' ORDER BY sequence LIMIT 1`, run).Scan(&configBody); err != nil {
		return Analysis{}, err
	}
	if err = json.Unmarshal([]byte(configBody), &config); err != nil {
		return Analysis{}, fmt.Errorf("快照范围不可读")
	}
	if config.ID == "" || config.Account == "" || config.LocalRoot == "" || config.RemoteRoot == "" || (config.Mode != "pull" && config.Mode != "both") {
		return Analysis{}, fmt.Errorf("来源配置缺少身份、范围或方向证据")
	}
	at, err := time.Parse(time.RFC3339Nano, recorded)
	if err != nil {
		return Analysis{}, err
	}
	report, err := directoryanalysis.Compare(analysisInput(snapshot.Local, "本地", config.LocalRoot), analysisInput(snapshot.Remote, "网盘", config.RemoteRoot))
	if err != nil {
		return Analysis{}, err
	}
	if err = ctx.Err(); err != nil {
		return Analysis{}, err
	}
	header := Analysis{ID: newID(), SourceRun: run, SourceConfig: config.ID, SourceScope: config.Account, SourceMode: config.Mode, SourceSequence: strconv.FormatInt(sequence, 10), SourceRecorded: at, Created: time.Now().UTC(), LeftRoot: config.LocalRoot, RightRoot: config.RemoteRoot, Historical: true, Summary: report, RowCount: strconv.Itoa(len(report.Rows)), GroupCount: strconv.Itoa(len(report.Groups))}
	return m.saveAnalysis(ctx, header, report)
}

func (m *Manager) saveAnalysis(ctx context.Context, header Analysis, report directoryanalysis.Report) (Analysis, error) {
	header.Summary.Rows = nil
	header.Summary.Groups = nil
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return header, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO directory_analysis_reports VALUES(?,?,?,?)`, header.ID, header.SourceRun, header.Created.Format(time.RFC3339Nano), encoded(header)); err != nil {
		return header, err
	}
	for i, row := range report.Rows {
		if _, err = tx.ExecContext(ctx, `INSERT INTO directory_analysis_rows VALUES(?,?,?,?)`, header.ID, i+1, row.Kind, encoded(row)); err != nil {
			return header, err
		}
	}
	for i, group := range report.Groups {
		view := AnalysisGroup{Ordinal: i + 1, Key: group.Key, SHA256: group.SHA256, Size: group.Size, RedundantBytesLeft: group.RedundantBytesLeft, RedundantBytesRight: group.RedundantBytesRight, MemberCount: strconv.Itoa(len(group.Members)), Members: []directoryanalysis.Member{}, CapacityBasis: group.CapacityBasis}
		if _, err = tx.ExecContext(ctx, `INSERT INTO directory_analysis_groups VALUES(?,?,?)`, header.ID, i+1, encoded(view)); err != nil {
			return header, err
		}
		for j, member := range group.Members {
			if _, err = tx.ExecContext(ctx, `INSERT INTO directory_analysis_members VALUES(?,?,?,?)`, header.ID, i+1, j+1, encoded(member)); err != nil {
				return header, err
			}
		}
	}
	return header, tx.Commit()
}

func (m *Manager) Analyses(before string) ([]Analysis, bool, string, error) {
	query := `SELECT body FROM directory_analysis_reports`
	args := []any{}
	if before != "" {
		var rowID int64
		if err := m.db.QueryRow(`SELECT rowid FROM directory_analysis_reports WHERE id=?`, before).Scan(&rowID); err != nil {
			return nil, false, "", err
		}
		query += ` WHERE rowid<?`
		args = append(args, rowID)
	}
	query += ` ORDER BY rowid DESC LIMIT 51`
	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, false, "", err
	}
	defer rows.Close()
	result := []Analysis{}
	for rows.Next() {
		var body string
		var header Analysis
		if err = rows.Scan(&body); err != nil {
			return nil, false, "", err
		}
		if err = json.Unmarshal([]byte(body), &header); err != nil {
			return nil, false, "", err
		}
		result = append(result, header)
	}
	if err = rows.Err(); err != nil {
		return nil, false, "", err
	}
	more := len(result) > 50
	next := ""
	if more {
		result = result[:50]
		next = result[49].ID
	}
	return result, more, next, nil
}

func validAnalysisKind(kind string) bool {
	switch kind {
	case "", "same", "same_directory", "content_conflict", "type_conflict", "unverified", "only_left", "only_right", "same_content_elsewhere", "left_not_fully_observed", "right_not_fully_observed":
		return true
	}
	return false
}
func (m *Manager) AnalysisPage(id, kind string, offset int) (AnalysisPage, error) {
	page := AnalysisPage{Rows: []directoryanalysis.Row{}, Offset: offset, Kind: kind}
	if offset < 0 || !validAnalysisKind(kind) {
		return page, fmt.Errorf("比较报告筛选无效")
	}
	var body string
	if err := m.db.QueryRow(`SELECT body FROM directory_analysis_reports WHERE id=?`, id).Scan(&body); err != nil {
		return page, err
	}
	if err := json.Unmarshal([]byte(body), &page.Analysis); err != nil {
		return page, err
	}
	query := `report_id=?`
	args := []any{id}
	if kind != "" {
		query += ` AND kind=?`
		args = append(args, kind)
	}
	var count int
	if err := m.db.QueryRow(`SELECT count(*) FROM directory_analysis_rows WHERE `+query, args...).Scan(&count); err != nil {
		return page, err
	}
	page.Matched = strconv.Itoa(count)
	rows, err := m.db.Query(`SELECT body FROM directory_analysis_rows WHERE `+query+` ORDER BY ordinal LIMIT 101 OFFSET ?`, append(args, offset)...)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var row directoryanalysis.Row
		var body string
		if err = rows.Scan(&body); err != nil {
			return page, err
		}
		if err = json.Unmarshal([]byte(body), &row); err != nil {
			return page, err
		}
		page.Rows = append(page.Rows, row)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	page.More = len(page.Rows) > 100
	if page.More {
		page.Rows = page.Rows[:100]
	}
	return page, nil
}

func (m *Manager) AnalysisGroups(id string, offset int) ([]AnalysisGroup, bool, error) {
	rows, err := m.db.Query(`SELECT body FROM directory_analysis_groups WHERE report_id=? ORDER BY ordinal LIMIT 11 OFFSET ?`, id, offset)
	if err != nil {
		return nil, false, err
	}
	result := []AnalysisGroup{}
	for rows.Next() {
		var g AnalysisGroup
		var body string
		if err = rows.Scan(&body); err != nil {
			rows.Close()
			return nil, false, err
		}
		if err = json.Unmarshal([]byte(body), &g); err != nil {
			rows.Close()
			return nil, false, err
		}
		result = append(result, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, false, err
	}
	more := len(result) > 10
	if more {
		result = result[:10]
	}
	for i := range result {
		members, more, err := m.AnalysisMembers(id, result[i].Ordinal, 0)
		if err != nil {
			return nil, false, err
		}
		result[i].Members = members
		result[i].MembersMore = more
	}
	return result, more, nil
}
func (m *Manager) AnalysisMembers(id string, group, offset int) ([]directoryanalysis.Member, bool, error) {
	if group < 1 || offset < 0 {
		return nil, false, fmt.Errorf("重复组分页无效")
	}
	rows, err := m.db.Query(`SELECT body FROM directory_analysis_members WHERE report_id=? AND group_ordinal=? ORDER BY ordinal LIMIT 21 OFFSET ?`, id, group, offset)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	result := []directoryanalysis.Member{}
	for rows.Next() {
		var member directoryanalysis.Member
		var body string
		if err = rows.Scan(&body); err != nil {
			return nil, false, err
		}
		if err = json.Unmarshal([]byte(body), &member); err != nil {
			return nil, false, err
		}
		result = append(result, member)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(result) > 20
	if more {
		result = result[:20]
	}
	return result, more, nil
}
