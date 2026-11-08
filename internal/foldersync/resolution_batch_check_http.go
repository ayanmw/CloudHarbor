package foldersync

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
)

func (m *Manager) HandleResolutionBatchCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if id := r.PathValue("check"); id != "" {
		if r.Method != http.MethodGet {
			w.WriteHeader(405)
			return
		}
		result, err := m.ResolutionBatchCheck(r.Context(), id)
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "批次核对暂不可读", 500)
			return
		}
		reply(w, result)
		return
	}
	batch := r.PathValue("batch")
	if r.Method == http.MethodPost {
		var empty struct{}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&empty) != nil || decoder.Decode(new(any)) != io.EOF {
			http.Error(w, "核对请求只接受空对象", 400)
			return
		}
		result, err := m.CheckResolutionBatch(r.Context(), batch)
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		reply(w, result)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	offset := 0
	var err error
	q := r.URL.Query()
	if value := q.Get("offset"); value != "" {
		offset, err = strconv.Atoi(value)
	}
	if err != nil || offset < 0 || len(q["offset"]) > 1 {
		http.Error(w, "核对分页无效", 400)
		return
	}
	results, more, err := m.ResolutionBatchChecks(r.Context(), batch, offset)
	if err != nil {
		http.Error(w, "核对历史暂不可读", 500)
		return
	}
	reply(w, map[string]any{"batch_id": batch, "offset": offset, "has_more": more, "checks": results})
}
