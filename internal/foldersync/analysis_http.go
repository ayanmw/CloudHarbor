package foldersync

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"github.com/ayanmw/CloudHarbor/internal/directoryanalysis"
)

func analysisOffset(r *http.Request, key string) (int, error) {
	s := r.URL.Query().Get(key)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("分析分页无效")
	}
	return n, nil
}
func (m *Manager) HandleAnalysis(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.PathValue("run") == "" && r.PathValue("report") == "" {
		var input struct {
			Left  string `json:"left_root"`
			Right string `json:"right_root"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
			http.Error(w, "比较目录请求无效", 400)
			return
		}
		header, err := m.AnalyzeCachedDirectories(r.Context(), input.Left, input.Right)
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		reply(w, header)
		return
	}
	if run := r.PathValue("run"); run != "" {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		if r.ContentLength != 0 {
			var empty struct{}
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&empty) != nil || decoder.Decode(new(any)) != io.EOF {
				http.Error(w, "分析请求无效", 400)
				return
			}
		}
		header, err := m.AnalyzeRun(r.Context(), run)
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "该批次尚无完整遍历快照", 409)
			} else {
				http.Error(w, err.Error(), 409)
			}
			return
		}
		reply(w, header)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	id := r.PathValue("report")
	if id == "" {
		reports, more, next, err := m.Analyses(r.URL.Query().Get("before"))
		if err != nil {
			http.Error(w, "分析历史读取失败", 500)
			return
		}
		reply(w, map[string]any{"reports": reports, "limit": 50, "has_more": more, "next_before": next})
		return
	}
	offset, err := analysisOffset(r, "offset")
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	page, err := m.AnalysisPage(id, r.URL.Query().Get("kind"), offset)
	if err != nil {
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
		} else {
			http.Error(w, "分析报告读取失败", 400)
		}
		return
	}
	if text := r.PathValue("group"); text != "" {
		group, err := strconv.Atoi(text)
		if err != nil || group < 1 {
			http.Error(w, "重复组无效", 400)
			return
		}
		members, more, err := m.AnalysisMembers(id, group, offset)
		if err != nil {
			http.Error(w, "重复组副本读取失败", 500)
			return
		}
		reply(w, map[string]any{"report_id": id, "group": group, "offset": offset, "members": members, "has_more": more})
		return
	}
	if r.URL.Query().Get("export") == "1" {
		rows, err := m.allAnalysisRows(id)
		if err != nil {
			http.Error(w, "分析明细导出失败", 500)
			return
		}
		groups := []AnalysisGroup{}
		for start := 0; ; start += 10 {
			batch, more, err := m.AnalysisGroups(id, start)
			if err != nil {
				http.Error(w, "重复组导出失败", 500)
				return
			}
			for i := range batch {
				batch[i].Members = nil
				for at := 0; ; at += 20 {
					members, more, err := m.AnalysisMembers(id, batch[i].Ordinal, at)
					if err != nil {
						http.Error(w, "重复副本导出失败", 500)
						return
					}
					batch[i].Members = append(batch[i].Members, members...)
					if !more {
						break
					}
				}
				batch[i].MembersMore = false
			}
			groups = append(groups, batch...)
			if !more {
				break
			}
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="directory-analysis-%s.json"`, id))
		reply(w, map[string]any{"analysis": page.Analysis, "rows": rows, "groups": groups})
		return
	}
	groupOffset, err := analysisOffset(r, "group_offset")
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	groups, more, err := m.AnalysisGroups(id, groupOffset)
	if err != nil {
		http.Error(w, "重复组读取失败", 500)
		return
	}
	reply(w, map[string]any{"analysis": page.Analysis, "rows": page.Rows, "offset": page.Offset, "kind": page.Kind, "matched": page.Matched, "has_more": page.More, "groups": groups, "group_offset": groupOffset, "groups_have_more": more})
}

func (m *Manager) allAnalysisRows(id string) ([]directoryanalysis.Row, error) {
	rows, err := m.db.Query(`SELECT body FROM directory_analysis_rows WHERE report_id=? ORDER BY ordinal`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []directoryanalysis.Row{}
	for rows.Next() {
		var body string
		var row directoryanalysis.Row
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(body), &row); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
