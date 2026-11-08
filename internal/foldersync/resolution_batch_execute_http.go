package foldersync

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
)

func (m *Manager) HandleResolutionBatchExecution(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("batch")
	if r.Method == http.MethodGet {
		value, err := m.ResolutionBatchExecution(r.Context(), id)
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "批次执行暂不可读", 500)
			return
		}
		reply(w, value)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var input struct {
		CheckID string `json:"check_id"`
		Confirm bool   `json:"confirm"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || !input.Confirm || input.CheckID == "" {
		http.Error(w, "请确认批次并提供完整核对ID", 400)
		return
	}
	result, err := m.StartResolutionBatch(id, input.CheckID)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	reply(w, result)
}
