package intakeapi

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type duplicateScanRunDTO struct {
	StartSequence  int64     `json:"start_sequence"`
	RunID          string    `json:"run_id"`
	Status         string    `json:"status"`
	Phase          string    `json:"phase"`
	NextFsID       int64     `json:"next_fs_id"`
	TotalFiles     int64     `json:"total_files"`
	ProcessedFiles int64     `json:"processed_files"`
	TotalBytes     int64     `json:"total_bytes"`
	ProcessedBytes int64     `json:"processed_bytes"`
	StartedAtUTC   time.Time `json:"started_at_utc"`
	UpdatedAtUTC   time.Time `json:"updated_at_utc"`
	LastError      string    `json:"last_error,omitempty"`
	LatestSequence int64     `json:"latest_sequence"`
}

type duplicateScanRunFilterDTO struct {
	Status            string `json:"status"`
	StartedFromUTC    string `json:"started_from_utc"`
	StartedThroughUTC string `json:"started_through_utc"`
}

func duplicateScanRunDTOFrom(run taskstore.CloudDuplicateScanRun) duplicateScanRunDTO {
	return duplicateScanRunDTO{StartSequence: run.StartSequence, RunID: run.RunID, Status: run.Status, Phase: run.Phase, NextFsID: run.NextFsID, TotalFiles: run.TotalFiles, ProcessedFiles: run.ProcessedFiles, TotalBytes: run.TotalBytes, ProcessedBytes: run.ProcessedBytes, StartedAtUTC: run.StartedAtUTC, UpdatedAtUTC: run.UpdatedAtUTC, LastError: run.LastError, LatestSequence: run.LatestSequence}
}

func (s *Server) HandleDuplicateScanRuns(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	query := r.URL.Query()
	allowed := map[string]bool{"before_start_sequence": true, "limit": true, "status": true, "started_from_utc": true, "started_through_utc": true}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			http.Error(w, "重复扫描历史参数无效。", http.StatusBadRequest)
			return
		}
	}
	before, limit := int64(0), 20
	var err error
	if value := query.Get("before_start_sequence"); value != "" {
		before, err = strconv.ParseInt(value, 10, 64)
		if err != nil || before <= 0 {
			http.Error(w, "重复扫描历史游标无效。", http.StatusBadRequest)
			return
		}
	}
	if value := query.Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			http.Error(w, "重复扫描历史页大小无效。", http.StatusBadRequest)
			return
		}
	}
	filter := duplicateScanRunFilter(query.Get("status"), query.Get("started_from_utc"), query.Get("started_through_utc"))
	if !validDuplicateScanRunFilter(filter) {
		http.Error(w, "重复扫描历史筛选条件无效。", http.StatusBadRequest)
		return
	}
	runs, nextBefore, more, err := s.backend.CloudDuplicateScanRuns(r.Context(), before, filter, limit)
	if err != nil {
		http.Error(w, "重复扫描运行历史读取失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	items := make([]duplicateScanRunDTO, 0, len(runs))
	for _, run := range runs {
		items = append(items, duplicateScanRunDTOFrom(run))
	}
	var next *int64
	if more {
		next = &nextBefore
	}
	jsonResponse(w, map[string]any{"runs": items, "next_before_start_sequence": next, "has_more": more})
}

func (s *Server) HandleDuplicateScanRunSummaryExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !method(w, r, http.MethodGet) {
		return
	}
	query := r.URL.Query()
	allowed := map[string]bool{"status": true, "started_from_utc": true, "started_through_utc": true}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			http.Error(w, "重复扫描摘要导出参数无效。", http.StatusBadRequest)
			return
		}
	}
	filter := duplicateScanRunFilter(query.Get("status"), query.Get("started_from_utc"), query.Get("started_through_utc"))
	if !validDuplicateScanRunFilter(filter) {
		http.Error(w, "重复扫描摘要导出筛选条件无效。", http.StatusBadRequest)
		return
	}
	runs, err := s.backend.CloudDuplicateScanRunsExport(r.Context(), filter)
	if errors.Is(err, taskstore.ErrCloudDuplicateScanSummaryExportTooLarge) {
		http.Error(w, "匹配的重复扫描运行超过导出上限；请缩小状态或日期范围。", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "重复扫描摘要导出失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	items := make([]duplicateScanRunDTO, 0, len(runs))
	for _, run := range runs {
		items = append(items, duplicateScanRunDTOFrom(run))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="duplicate-scan-runs.json"`)
	_ = json.NewEncoder(w).Encode(map[string]any{"exported_at_utc": time.Now().UTC(), "filter": duplicateScanRunFilterDTO{Status: filter.Status, StartedFromUTC: filter.StartedFromUTC, StartedThroughUTC: filter.StartedThroughUTC}, "runs": items, "total_runs": len(items)})
}

func (s *Server) HandleDuplicateScanRunEvents(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	runID := r.PathValue("id")
	if !validDuplicateScanRunID(runID) {
		http.NotFound(w, r)
		return
	}
	query := r.URL.Query()
	for key, values := range query {
		if (key != "before_sequence" && key != "limit") || len(values) != 1 {
			http.Error(w, "重复扫描事件分页参数无效。", http.StatusBadRequest)
			return
		}
	}
	before, limit := int64(0), 20
	var err error
	if value := query.Get("before_sequence"); value != "" {
		before, err = strconv.ParseInt(value, 10, 64)
		if err != nil || before <= 0 {
			http.Error(w, "重复扫描事件游标无效。", http.StatusBadRequest)
			return
		}
	}
	if value := query.Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 50 {
			http.Error(w, "重复扫描事件页大小无效。", http.StatusBadRequest)
			return
		}
	}
	events, nextBefore, more, err := s.backend.CloudDuplicateScanRunEventPage(r.Context(), runID, before, limit)
	if errors.Is(err, taskstore.ErrCloudDuplicateScanRunNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "重复扫描事件读取失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	if err = maskDuplicateScanEvents(events); err != nil {
		http.Error(w, "重复扫描事件证据读取失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	var next *int64
	if more {
		next = &nextBefore
	}
	jsonResponse(w, map[string]any{"run_id": runID, "events": events, "next_before_sequence": next, "has_more": more})
}

func (s *Server) HandleDuplicateScanEvidenceExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !method(w, r, http.MethodGet) {
		return
	}
	runID := r.PathValue("id")
	if !validDuplicateScanRunID(runID) {
		http.NotFound(w, r)
		return
	}
	bundle, err := s.backend.CloudDuplicateScanEvidenceExport(r.Context(), runID)
	if errors.Is(err, taskstore.ErrCloudDuplicateScanRunNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, taskstore.ErrCloudDuplicateScanEvidenceExportTooLarge) {
		http.Error(w, "此轮重复扫描事件超过导出上限；请在页面分页查看。", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "重复扫描证据导出失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	if err = maskDuplicateScanEvents(bundle.Events); err != nil {
		http.Error(w, "重复扫描证据脱敏失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	items := make([]CloudDuplicateScanEventExportDTO, 0, len(bundle.Events))
	for _, event := range bundle.Events {
		items = append(items, CloudDuplicateScanEventExportDTO{Sequence: event.Sequence, RunID: event.RunID, Status: event.Status, Phase: event.Phase, RecordedAtUTC: event.RecordedAtUTC, Evidence: event.Evidence})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="duplicate-scan-`+runID+`.json"`)
	_ = json.NewEncoder(w).Encode(map[string]any{"exported_at_utc": time.Now().UTC(), "run": duplicateScanRunDTOFrom(bundle.Run), "events": items, "total_events": len(items)})
}

type CloudDuplicateScanEventExportDTO struct {
	Sequence      int64           `json:"sequence"`
	RunID         string          `json:"run_id"`
	Status        string          `json:"status"`
	Phase         string          `json:"phase"`
	RecordedAtUTC time.Time       `json:"recorded_at_utc"`
	Evidence      json.RawMessage `json:"evidence,omitempty"`
}

func duplicateScanRunFilter(status, from, through string) taskstore.CloudDuplicateScanRunFilter {
	return taskstore.CloudDuplicateScanRunFilter{Status: status, StartedFromUTC: from, StartedThroughUTC: through}
}

func validDuplicateScanRunFilter(filter taskstore.CloudDuplicateScanRunFilter) bool {
	if filter.Status != "" && filter.Status != "running" && filter.Status != "paused" && filter.Status != "failed" && filter.Status != "complete" {
		return false
	}
	validDate := func(value string) bool {
		if value == "" {
			return true
		}
		parsed, err := time.Parse("2006-01-02", value)
		return err == nil && parsed.Format("2006-01-02") == value
	}
	return validDate(filter.StartedFromUTC) && validDate(filter.StartedThroughUTC) && (filter.StartedFromUTC == "" || filter.StartedThroughUTC == "" || filter.StartedFromUTC <= filter.StartedThroughUTC)
}

func validDuplicateScanRunID(runID string) bool {
	if len(runID) != 32 {
		return false
	}
	_, err := hex.DecodeString(runID)
	return err == nil
}

func maskDuplicateScanEvents(events []taskstore.CloudDuplicateScanEvent) error {
	for i := range events {
		evidence, err := MaskSensitiveEvidence(events[i].Evidence)
		if err != nil {
			return err
		}
		events[i].Evidence = evidence
	}
	return nil
}
