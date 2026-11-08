package foldersync

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
)

func (m *Manager) HandleResolutionExecution(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		result, err := m.ResolutionExecution(r.Context(), r.PathValue("decision"))
		if err != nil {
			if err == sql.ErrNoRows {
				http.NotFound(w, r)
			} else {
				http.Error(w, "执行记录读取失败", 500)
			}
			return
		}
		reply(w, result)
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
		http.Error(w, "请确认固定决策并提供已通过的核对ID", 400)
		return
	}
	result, err := m.StartResolution(r.PathValue("decision"), input.CheckID)
	if err != nil {
		http.Error(w, "决策未准入："+err.Error(), 409)
		return
	}
	reply(w, result)
}
