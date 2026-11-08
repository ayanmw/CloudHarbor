package foldersync

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

func (m *Manager) HandleResolutionCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if id := r.PathValue("check"); id != "" {
		if r.Method != http.MethodGet {
			w.WriteHeader(405)
			return
		}
		value, err := m.ResolutionCheck(r.Context(), id)
		if err != nil {
			if err == sql.ErrNoRows {
				http.NotFound(w, r)
			} else {
				http.Error(w, "核对证据读取失败", 500)
			}
			return
		}
		reply(w, value)
		return
	}
	if r.Method == http.MethodGet {
		query := r.URL.Query()
		offset := 0
		var err error
		if text := query.Get("offset"); text != "" {
			offset, err = strconv.Atoi(text)
		}
		if err != nil || offset < 0 || offset > 1000000 || len(query["offset"]) > 1 {
			http.Error(w, "核对分页无效", 400)
			return
		}
		checks, more, err := m.ResolutionChecks(r.Context(), r.PathValue("decision"), offset)
		if err != nil {
			http.Error(w, "核对历史读取失败", 500)
			return
		}
		reply(w, map[string]any{"decision_id": r.PathValue("decision"), "offset": offset, "checks": checks, "has_more": more})
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var empty struct{}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if r.ContentLength != 0 && (decoder.Decode(&empty) != nil || decoder.Decode(new(any)) != io.EOF) {
		http.Error(w, "核对请求无效", 400)
		return
	}
	value, err := m.CheckResolution(r.Context(), r.PathValue("decision"))
	if err != nil {
		http.Error(w, "无法开始核对："+err.Error(), 409)
		return
	}
	reply(w, value)
}
