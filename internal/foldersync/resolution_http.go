package foldersync

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

func (m *Manager) HandleResolution(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if id := r.PathValue("decision"); id != "" {
		if r.Method != http.MethodGet {
			w.WriteHeader(405)
			return
		}
		var value Resolution
		var err error
		if r.URL.Query().Get("export") == "1" {
			value, err = m.Resolution(r.Context(), id)
		} else {
			value, err = m.ResolutionSummary(r.Context(), id)
		}
		if err != nil {
			if err == sql.ErrNoRows {
				http.NotFound(w, r)
			} else {
				http.Error(w, "决策证据读取失败", 500)
			}
			return
		}
		if r.URL.Query().Get("export") == "1" {
			w.Header().Set("Content-Disposition", `attachment; filename="sync-resolution-`+id+`.json"`)
		}
		reply(w, value)
		return
	}
	if r.Method == http.MethodGet {
		query := r.URL.Query()
		ordinal, err := strconv.Atoi(query.Get("ordinal"))
		offset := 0
		if text := query.Get("offset"); text != "" {
			offset, err = strconv.Atoi(text)
		}
		if err != nil || ordinal < 1 || offset < 0 || offset > 1000000 || len(query["ordinal"]) != 1 || len(query["offset"]) > 1 {
			http.Error(w, "决策分页无效", 400)
			return
		}
		decisions, more, err := m.Resolutions(r.Context(), r.PathValue("run"), ordinal, offset)
		if err != nil {
			http.Error(w, "决策历史读取失败", 500)
			return
		}
		reply(w, map[string]any{"source_run": r.PathValue("run"), "ordinal": ordinal, "offset": offset, "decisions": decisions, "has_more": more})
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var body struct {
		Ordinal int    `json:"ordinal"`
		Choice  string `json:"choice"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "冲突决策请求无效", 400)
		return
	}
	decision, err := m.CreateResolution(r.Context(), r.PathValue("run"), body.Ordinal, body.Choice)
	if err != nil {
		http.Error(w, "无法保存决策预览："+err.Error(), 409)
		return
	}
	reply(w, resolutionSummary(decision))
}
