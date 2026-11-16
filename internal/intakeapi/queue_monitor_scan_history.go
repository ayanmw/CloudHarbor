package intakeapi

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

type queueMonitorRunDTO struct {
	Sequence           int64      `json:"sequence"`
	RunID              string     `json:"run_id"`
	Status             string     `json:"status"`
	MonitorPath        string     `json:"monitor_path"`
	StartedAtUTC       time.Time  `json:"started_at_utc"`
	FinishedAtUTC      *time.Time `json:"finished_at_utc,omitempty"`
	CloudObservedAtUTC *time.Time `json:"cloud_observed_at_utc,omitempty"`
	RecordedAtUTC      time.Time  `json:"recorded_at_utc"`
	ErrorCode          string     `json:"error_code,omitempty"`
}

type queueMonitorEventDTO struct {
	Sequence           int64      `json:"sequence"`
	RunID              string     `json:"run_id"`
	Kind               string     `json:"kind"`
	RecordedAtUTC      time.Time  `json:"recorded_at_utc"`
	CloudObservedAtUTC *time.Time `json:"cloud_observed_at_utc,omitempty"`
	ErrorCode          string     `json:"error_code,omitempty"`
}

type queueMonitorStringRunDTO struct {
	queueMonitorRunDTO
	Sequence string `json:"sequence"`
}
type queueMonitorStringEventDTO struct {
	queueMonitorEventDTO
	Sequence string `json:"sequence"`
}

func monitorStringCursor(next int64, more bool) *string {
	if !more {
		return nil
	}
	value := strconv.FormatInt(next, 10)
	return &value
}

func monitorRunDTO(r taskstore.QueueMonitorScanRun) queueMonitorRunDTO {
	return queueMonitorRunDTO{Sequence: r.Sequence, RunID: r.RunID, Status: r.Status, MonitorPath: r.MonitorPath, StartedAtUTC: r.StartedAtUTC, FinishedAtUTC: r.FinishedAtUTC, CloudObservedAtUTC: r.CloudObservedAtUTC, RecordedAtUTC: r.RecordedAtUTC, ErrorCode: r.ErrorCode}
}
func monitorEventDTO(e taskstore.QueueMonitorScanEvent) queueMonitorEventDTO {
	return queueMonitorEventDTO{Sequence: e.Sequence, RunID: e.RunID, Kind: e.Kind, RecordedAtUTC: e.RecordedAtUTC, CloudObservedAtUTC: e.CloudObservedAtUTC, ErrorCode: e.ErrorCode}
}

func monitorPaging(w http.ResponseWriter, r *http.Request, events bool) (int64, int, bool) {
	q := r.URL.Query()
	allowed := map[string]bool{"before_sequence": true, "limit": true, "sequence_format": true}
	if events {
		allowed["run_id"] = true
	} else {
		allowed["status"], allowed["started_from_utc"], allowed["started_through_utc"] = true, true, true
	}
	for k, v := range q {
		if !allowed[k] || len(v) != 1 {
			http.Error(w, "自动扫描历史查询参数无效。", 400)
			return 0, 0, false
		}
	}
	if format := q.Get("sequence_format"); format != "" && format != "string" {
		http.Error(w, "扫描历史序列格式无效。", 400)
		return 0, 0, false
	}
	before := int64(0)
	if raw := q.Get("before_sequence"); raw != "" {
		n, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || n <= 0 {
			http.Error(w, "自动扫描历史游标无效。", 400)
			return 0, 0, false
		}
		before = n
	}
	limit := 50
	if raw := q.Get("limit"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n < 1 || n > 100 {
			http.Error(w, "自动扫描历史每页数量无效。", 400)
			return 0, 0, false
		}
		limit = n
	}
	if !events {
		status := q.Get("status")
		if status != "" && status != "running" && status != "success" && status != "failed" && status != "interrupted" {
			http.Error(w, "自动扫描历史状态无效。", 400)
			return 0, 0, false
		}
		for _, key := range []string{"started_from_utc", "started_through_utc"} {
			v := q.Get(key)
			if v != "" {
				t, e := time.Parse("2006-01-02", v)
				if e != nil || t.Format("2006-01-02") != v {
					http.Error(w, "UTC 日期格式应为 YYYY-MM-DD。", 400)
					return 0, 0, false
				}
			}
		}
		if q.Get("started_from_utc") != "" && q.Get("started_through_utc") != "" && q.Get("started_from_utc") > q.Get("started_through_utc") {
			http.Error(w, "UTC 日期范围无效。", 400)
			return 0, 0, false
		}
	}
	return before, limit, true
}

func (s *Server) HandleQueueMonitorScanRuns(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	before, limit, ok := monitorPaging(w, r, false)
	if !ok {
		return
	}
	q := r.URL.Query()
	filter := taskstore.QueueMonitorScanFilter{Status: q.Get("status"), StartedFromUTC: q.Get("started_from_utc"), StartedThroughUTC: q.Get("started_through_utc")}
	runs, next, more, err := s.backend.QueueMonitorScanHistoryRuns(r.Context(), before, filter, limit)
	if err != nil {
		readFailure(w, err)
		return
	}
	if q.Get("sequence_format") == "string" {
		out := make([]queueMonitorStringRunDTO, 0, len(runs))
		for _, run := range runs {
			out = append(out, queueMonitorStringRunDTO{queueMonitorRunDTO: monitorRunDTO(run), Sequence: strconv.FormatInt(run.Sequence, 10)})
		}
		jsonResponse(w, map[string]any{"runs": out, "next_before_sequence": monitorStringCursor(next, more), "has_more": more})
		return
	}
	out := make([]queueMonitorRunDTO, 0, len(runs))
	for _, run := range runs {
		out = append(out, monitorRunDTO(run))
	}
	jsonResponse(w, map[string]any{"runs": out, "next_before_sequence": next, "has_more": more})
}

func (s *Server) HandleQueueMonitorScanEvents(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	before, limit, ok := monitorPaging(w, r, true)
	if !ok {
		return
	}
	runID := r.URL.Query().Get("run_id")
	if len(runID) != 32 || strings.ToLower(runID) != runID {
		http.Error(w, "扫描运行 ID 无效。", 400)
		return
	}
	if _, err := hex.DecodeString(runID); err != nil {
		http.Error(w, "扫描运行 ID 无效。", 400)
		return
	}
	events, next, more, err := s.backend.QueueMonitorScanHistoryEvents(r.Context(), runID, before, limit)
	if err != nil {
		readFailure(w, err)
		return
	}
	if r.URL.Query().Get("sequence_format") == "string" {
		out := make([]queueMonitorStringEventDTO, 0, len(events))
		for _, event := range events {
			out = append(out, queueMonitorStringEventDTO{queueMonitorEventDTO: monitorEventDTO(event), Sequence: strconv.FormatInt(event.Sequence, 10)})
		}
		jsonResponse(w, map[string]any{"events": out, "next_before_sequence": monitorStringCursor(next, more), "has_more": more})
		return
	}
	out := make([]queueMonitorEventDTO, 0, len(events))
	for _, event := range events {
		out = append(out, monitorEventDTO(event))
	}
	jsonResponse(w, map[string]any{"events": out, "next_before_sequence": next, "has_more": more})
}

func (s *Server) HandleQueueMonitorScanExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !method(w, r, http.MethodGet) {
		return
	}
	runID := r.PathValue("id")
	if len(runID) != 32 || strings.ToLower(runID) != runID {
		http.NotFound(w, r)
		return
	}
	if _, err := hex.DecodeString(runID); err != nil {
		http.NotFound(w, r)
		return
	}
	bundle, err := s.backend.QueueMonitorScanHistoryExport(r.Context(), runID)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, taskstore.ErrCloudScanEvidenceTooLarge) {
		http.Error(w, "此轮自动扫描证据超过导出上限。", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "自动扫描证据导出失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	events := make([]queueMonitorEventDTO, 0, len(bundle.Events))
	for _, event := range bundle.Events {
		events = append(events, monitorEventDTO(event))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="queue-monitor-scan-`+runID+`.json"`)
	_ = json.NewEncoder(w).Encode(map[string]any{"exported_at_utc": time.Now().UTC(), "run": monitorRunDTO(bundle.Run), "events": events, "total_events": len(events)})
}
