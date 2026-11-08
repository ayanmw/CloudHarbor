package intakeapi

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"
	"web_browser_baidu_onekey_upload/internal/downloader"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type cleanupBackend interface {
	CreateCloudCleanupBatch(context.Context, string, []taskstore.CleanupBatchItem) (taskstore.CleanupBatchRecord, error)
	CloudCleanupBatches(context.Context, int, int) ([]taskstore.CleanupBatchSummary, int, error)
	CloudCleanupBatch(context.Context, string) (taskstore.CleanupBatchRecord, error)
	CloudCleanupItems(context.Context, string, int, int) ([]taskstore.CleanupItemRecord, int, error)
	CloudCleanupEvents(context.Context, string, int64, int) ([]taskstore.CleanupBatchEvent, bool, error)
	StopCloudCleanupBatch(context.Context, string) error
}

func (s *Server) cleanupBackend(w http.ResponseWriter) (cleanupBackend, bool) {
	b, ok := s.backend.(cleanupBackend)
	if !ok {
		http.Error(w, "当前服务不支持后台清理。", 503)
	}
	return b, ok
}

const (
	cleanupCreateFailureMessage = "清理请求未完成，请按批次 ID 查询状态；重试提交时必须使用相同 ID 和清单。"
	cleanupReadFailureMessage   = "清理记录读取失败，请稍后刷新；本次查询不会重新执行清理。"
	cleanupStopFailureMessage   = "停止请求未确认，请按批次 ID 查询当前状态；已发送项目仍需核对结果。"
)

// Use the failed action's message: reading or stopping must not suggest creating
// a cleanup batch again. Shared status mapping and redaction remain unchanged.
func cleanupHTTPFailure(w http.ResponseWriter, err error, message string) {
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, sql.ErrNoRows):
		status, message = 404, "找不到清理批次。"
	case errors.Is(err, taskstore.ErrCleanupBatchConflict), errors.Is(err, downloader.ErrIntakeConflict), errors.Is(err, downloader.ErrIntakeStale):
		status, message = 409, "批次清单、作用域或状态已变化，请重新读取批次。"
	case errors.Is(err, downloader.ErrAuthenticationWaiting):
		status, message = 409, "请先完成百度账号授权或续期。"
	}
	http.Error(w, message, status)
}
func cleanupPage(r *http.Request, max int) (int, int, bool) {
	offset, limit := 0, 25
	for name, target := range map[string]*int{"offset": &offset, "limit": &limit} {
		values, exists := r.URL.Query()[name]
		if !exists {
			continue
		}
		if len(values) != 1 {
			return 0, 0, false
		}
		value, err := strconv.Atoi(values[0])
		if err != nil || strconv.Itoa(value) != values[0] {
			return 0, 0, false
		}
		*target = value
	}
	return offset, limit, offset >= 0 && limit >= 1 && limit <= max
}
func cleanupID(r *http.Request) (string, bool) {
	id := r.PathValue("id")
	decoded, err := hex.DecodeString(id)
	return id, err == nil && len(decoded) == 16 && hex.EncodeToString(decoded) == id
}
func (s *Server) HandleCloudCleanupBatches(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" && r.Method != "POST" {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	b, ok := s.cleanupBackend(w)
	if !ok {
		return
	}
	if r.Method == "GET" {
		offset, limit, valid := cleanupPage(r, 50)
		if !valid {
			http.Error(w, "分页参数无效。", 400)
			return
		}
		items, total, err := b.CloudCleanupBatches(r.Context(), offset, limit)
		if err != nil {
			cleanupHTTPFailure(w, err, cleanupReadFailureMessage)
			return
		}
		jsonResponse(w, map[string]any{"batches": items, "total": total, "offset": offset, "limit": limit, "observed_at_utc": time.Now().UTC()})
		return
	}
	var wire struct {
		ID    string                       `json:"batch_id"`
		Items []taskstore.CleanupBatchItem `json:"items"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "清理请求格式无效或超过上限。", 400)
		return
	}
	if err := taskstore.ValidateCleanupBatchSelection(wire.ID, wire.Items); err != nil {
		http.Error(w, "清理清单无效，请重新预览。", 400)
		return
	}
	record, err := b.CreateCloudCleanupBatch(r.Context(), wire.ID, wire.Items)
	if err != nil {
		cleanupHTTPFailure(w, err, cleanupCreateFailureMessage)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(record)
}
func (s *Server) HandleCloudCleanupBatch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !method(w, r, "GET") {
		return
	}
	id, ok := cleanupID(r)
	if !ok {
		http.Error(w, "批次 ID 无效。", 400)
		return
	}
	b, ok := s.cleanupBackend(w)
	if !ok {
		return
	}
	record, err := b.CloudCleanupBatch(r.Context(), id)
	if err != nil {
		cleanupHTTPFailure(w, err, cleanupReadFailureMessage)
		return
	}
	jsonResponse(w, record)
}
func (s *Server) HandleCloudCleanupItems(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !method(w, r, "GET") {
		return
	}
	id, ok := cleanupID(r)
	offset, limit, valid := cleanupPage(r, 100)
	if !ok || !valid {
		http.Error(w, "批次或分页参数无效。", 400)
		return
	}
	b, ok := s.cleanupBackend(w)
	if !ok {
		return
	}
	items, total, err := b.CloudCleanupItems(r.Context(), id, offset, limit)
	if err != nil {
		cleanupHTTPFailure(w, err, cleanupReadFailureMessage)
		return
	}
	jsonResponse(w, map[string]any{"items": items, "total": total, "offset": offset, "limit": limit, "observed_at_utc": time.Now().UTC()})
}
func (s *Server) HandleCloudCleanupEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !method(w, r, "GET") {
		return
	}
	id, ok := cleanupID(r)
	_, limit, valid := cleanupPage(r, 100)
	after := int64(0)
	if values, exists := r.URL.Query()["after_sequence"]; exists {
		if len(values) != 1 {
			valid = false
		} else {
			parsed, err := strconv.ParseInt(values[0], 10, 64)
			if err != nil || parsed < 0 || strconv.FormatInt(parsed, 10) != values[0] {
				valid = false
			} else {
				after = parsed
			}
		}
	}
	if !ok || !valid {
		http.Error(w, "批次或事件游标无效。", 400)
		return
	}
	b, ok := s.cleanupBackend(w)
	if !ok {
		return
	}
	events, more, err := b.CloudCleanupEvents(r.Context(), id, after, limit)
	if err != nil {
		cleanupHTTPFailure(w, err, cleanupReadFailureMessage)
		return
	}
	next := after
	if len(events) > 0 {
		next = events[len(events)-1].Sequence
	}
	jsonResponse(w, map[string]any{"events": events, "has_more": more, "next_sequence": strconv.FormatInt(next, 10), "observed_at_utc": time.Now().UTC()})
}
func (s *Server) HandleCloudCleanupStop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !method(w, r, "POST") {
		return
	}
	id, ok := cleanupID(r)
	if !ok {
		http.Error(w, "批次 ID 无效。", 400)
		return
	}
	if !decode(w, r, &struct{}{}, true) {
		return
	}
	b, ok := s.cleanupBackend(w)
	if !ok {
		return
	}
	if err := b.StopCloudCleanupBatch(r.Context(), id); err != nil {
		cleanupHTTPFailure(w, err, cleanupStopFailureMessage)
		return
	}
	jsonResponse(w, map[string]any{"batch_id": id, "message": "停止请求已处理；已发送项目仍需记录实际结果。"})
}
