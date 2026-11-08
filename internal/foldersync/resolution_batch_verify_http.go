package foldersync

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
)

func (m *Manager) HandleResolutionBatchVerification(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := r.PathValue("batch")
	if r.Method == http.MethodGet {
		value, err := m.LatestResolutionBatchVerification(r.Context(), id)
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "批次复核暂不可读", 500)
			return
		}
		reply(w, value)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var empty struct{}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&empty) != nil || decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "请明确提交空对象以启动完整批次复核", 400)
		return
	}
	result, err := m.StartResolutionBatchVerification(id)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	reply(w, result)
}
