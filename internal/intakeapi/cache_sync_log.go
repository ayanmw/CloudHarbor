package intakeapi

import (
	"context"
	"encoding/hex"
	"net/http"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func (s *Server) HandleCacheSyncLog(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	run := r.URL.Query().Get("run_id")
	raw, err := hex.DecodeString(run)
	if err != nil || len(raw) != 16 || hex.EncodeToString(raw) != run {
		http.Error(w, "刷新运行ID无效", 400)
		return
	}
	backend, ok := s.backend.(interface {
		CacheSyncChanges(context.Context, string) (taskstore.CacheSyncView, error)
	})
	if !ok {
		http.Error(w, "同步日志查询不可用", 501)
		return
	}
	view, err := backend.CacheSyncChanges(r.Context(), run)
	if err != nil {
		readFailure(w, err)
		return
	}
	rows := []map[string]any{}
	for _, item := range view.Changes {
		rows = append(rows, map[string]any{"fs_id": item.ID, "action": item.Action, "old": refreshChangeItemExact(item.Old), "new": refreshChangeItemExact(item.New), "source_change_sequences": item.Evidence, "recorded_at_utc": item.Recorded, "cloud_observed_at_utc": item.Observed})
	}
	jsonResponse(w, map[string]any{"run_id": view.RunID, "status": view.Status, "root": view.Root, "changes": rows, "raw_count": view.RawCount, "derived_from_immutable_observations": view.Derived})
}
