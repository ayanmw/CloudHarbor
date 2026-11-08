package foldersync

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

func (m *Manager) HandleResolutionBatch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if id := r.PathValue("batch"); id != "" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		batch, err := m.ResolutionBatch(r.Context(), id)
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "批次记录暂不可读", 500)
			return
		}
		reply(w, batch)
		return
	}
	source := r.PathValue("run")
	if r.Method == http.MethodPost {
		var input struct {
			DecisionIDs []string `json:"decision_ids"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
			http.Error(w, "请提供明确选择的决策ID", 400)
			return
		}
		batch, err := m.CreateResolutionBatch(r.Context(), source, input.DecisionIDs)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		reply(w, batch)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	offset := 0
	var err error
	q := r.URL.Query()
	if value := q.Get("offset"); value != "" {
		offset, err = strconv.Atoi(value)
	}
	if err != nil || offset < 0 || len(q["offset"]) > 1 {
		http.Error(w, "批次分页无效", 400)
		return
	}
	batches, more, err := m.ResolutionBatches(r.Context(), source, offset)
	if err != nil {
		http.Error(w, "批次列表暂不可读", 500)
		return
	}
	reply(w, map[string]any{"source_run": source, "offset": offset, "has_more": more, "batches": batches})
}
