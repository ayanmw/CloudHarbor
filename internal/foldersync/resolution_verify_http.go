package foldersync

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
)

func (m *Manager) HandleResolutionVerification(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		result, err := m.LatestResolutionVerification(r.Context(), r.PathValue("decision"))
		if err != nil {
			if err == sql.ErrNoRows {
				http.NotFound(w, r)
			} else {
				http.Error(w, "完整复核记录读取失败", 500)
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
	var empty struct{}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if r.ContentLength != 0 && (decoder.Decode(&empty) != nil || decoder.Decode(new(any)) != io.EOF) {
		http.Error(w, "完整复核请求无效", 400)
		return
	}
	result, err := m.StartResolutionVerification(r.PathValue("decision"))
	if err != nil {
		http.Error(w, "无法准入完整复核："+err.Error(), 409)
		return
	}
	reply(w, result)
}
