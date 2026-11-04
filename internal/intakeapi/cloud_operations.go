package intakeapi

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"web_browser_baidu_onekey_upload/internal/taskstore"
)

const remoteOperationPageLimit = 50

func (s *Server) HandleCloudOperations(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	stringIdentities, err := operationStringIdentity(r.URL.Query())
	if err != nil {
		http.Error(w, "invalid operation identity format", http.StatusBadRequest)
		return
	}
	filter, err := parseRemoteOperationFilter(r.URL.Query())
	if err != nil {
		http.Error(w, "invalid remote operation filters", http.StatusBadRequest)
		return
	}
	query := r.URL.Query()
	if len(query["offset"]) > 1 || len(query["limit"]) > 1 || len(query["before_created_at_utc"]) > 1 || len(query["before_operation_id"]) > 1 {
		http.Error(w, "invalid remote operation page", http.StatusBadRequest)
		return
	}
	if !query.Has("offset") || query.Has("before_created_at_utc") || query.Has("before_operation_id") {
		if query.Has("offset") {
			http.Error(w, "invalid remote operation page", http.StatusBadRequest)
			return
		}
		before, cursorErr := parseRemoteOperationCursor(query)
		if cursorErr != nil {
			http.Error(w, "invalid remote operation cursor", http.StatusBadRequest)
			return
		}
		_, limit, pageErr := pagination(r, 20)
		if pageErr != nil || limit > remoteOperationPageLimit {
			http.Error(w, "invalid remote operation page", http.StatusBadRequest)
			return
		}
		items, total, next, pageErr := s.backend.CloudOperationCursorPage(r.Context(), filter, before, limit)
		if pageErr != nil {
			http.Error(w, "操作历史读取失败，请稍后重试。", http.StatusServiceUnavailable)
			return
		}
		jsonResponse(w, map[string]any{"operations": operationSummaryResponse(items, stringIdentities), "total": total, "limit": limit, "next_cursor": next})
		return
	}
	offset, limit, err := pagination(r, 20)
	if err != nil || limit > remoteOperationPageLimit {
		http.Error(w, "invalid remote operation page", http.StatusBadRequest)
		return
	}
	items, total, err := s.backend.CloudOperationPage(r.Context(), filter, offset, limit)
	if err != nil {
		http.Error(w, "操作历史读取失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	var nextOffset *int
	if offset+len(items) < total {
		next := offset + len(items)
		nextOffset = &next
	}
	jsonResponse(w, map[string]any{
		"operations":  operationSummaryResponse(items, stringIdentities),
		"total":       total,
		"offset":      offset,
		"limit":       limit,
		"next_offset": nextOffset,
	})
}

func parseRemoteOperationCursor(query url.Values) (*taskstore.RemoteOperationCursor, error) {
	createdValue, id := query.Get("before_created_at_utc"), query.Get("before_operation_id")
	if createdValue == "" && id == "" && !query.Has("before_created_at_utc") && !query.Has("before_operation_id") {
		return nil, nil
	}
	if createdValue == "" || len(id) != 32 {
		return nil, errors.New("incomplete remote operation cursor")
	}
	created, err := parseRemoteOperationTime(createdValue)
	if err != nil {
		return nil, err
	}
	if _, err = hex.DecodeString(id); err != nil {
		return nil, err
	}
	return &taskstore.RemoteOperationCursor{CreatedAtUTC: *created, ID: strings.ToLower(id)}, nil
}

func (s *Server) HandleCloudOperationExport(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	filter, err := parseRemoteOperationFilter(r.URL.Query())
	if err != nil {
		http.Error(w, "invalid remote operation filters", http.StatusBadRequest)
		return
	}
	operations, err := s.backend.CloudOperationExport(r.Context(), filter)
	if errors.Is(err, taskstore.ErrRemoteOperationExportTooLarge) {
		http.Error(w, "导出超过 5,000 条，请缩小状态、关键字或时间范围", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "操作历史导出失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	now := time.Now().UTC()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="remote-operations-`+now.Format("20060102T150405Z")+`.json"`)
	w.Header().Set("Cache-Control", "no-store")
	filterValue := map[string]any{"status": filter.Status, "query": filter.Query}
	if filter.Kind != "" {
		filterValue["kind"] = filter.Kind
	}
	if filter.FromUTC != nil {
		filterValue["from_utc"] = filter.FromUTC.Format(time.RFC3339Nano)
	}
	if filter.ToUTC != nil {
		filterValue["to_utc"] = filter.ToUTC.Format(time.RFC3339Nano)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"exported_at_utc": now,
		"filter":          filterValue,
		"total":           len(operations),
		"operations":      operations,
	})
}

func parseRemoteOperationFilter(query url.Values) (taskstore.RemoteOperationFilter, error) {
	status := query.Get("status")
	if status != "" {
		switch status {
		case "prepared", "confirmed", "unknown", "failed":
		default:
			return taskstore.RemoteOperationFilter{}, errors.New("invalid remote operation status")
		}
	}
	kind := query.Get("kind")
	if kind != "" {
		switch kind {
		case "mkdir", "move", "rename", "delete":
		default:
			return taskstore.RemoteOperationFilter{}, errors.New("invalid remote operation kind")
		}
	}
	filter := taskstore.RemoteOperationFilter{Status: status, Kind: kind, Query: strings.TrimSpace(query.Get("q"))}
	if utf8.RuneCountInString(filter.Query) > 128 {
		return taskstore.RemoteOperationFilter{}, errors.New("remote operation search query is too long")
	}
	from, err := parseRemoteOperationTime(query.Get("from"))
	if err != nil {
		return taskstore.RemoteOperationFilter{}, err
	}
	filter.FromUTC = from
	filter.ToUTC, err = parseRemoteOperationTime(query.Get("to"))
	if err != nil || (filter.FromUTC != nil && filter.ToUTC != nil && filter.FromUTC.After(*filter.ToUTC)) {
		return taskstore.RemoteOperationFilter{}, errors.New("invalid remote operation time range")
	}
	return filter, nil
}

func parseRemoteOperationTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	utc := parsed.UTC()
	if !time.Unix(0, parsed.UnixNano()).Equal(utc) {
		return nil, strconv.ErrRange
	}
	return &utc, nil
}

func (s *Server) HandleCloudOperationEvents(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	stringIdentities, identityErr := operationStringIdentity(r.URL.Query())
	if identityErr != nil {
		http.Error(w, "invalid operation identity format", http.StatusBadRequest)
		return
	}
	query := r.URL.Query()
	if len(query["offset"]) > 1 || len(query["before_sequence"]) > 1 || len(query["limit"]) > 1 {
		http.Error(w, "invalid remote operation event filters", http.StatusBadRequest)
		return
	}
	if query.Has("offset") {
		http.Error(w, "events use before_sequence pagination", http.StatusBadRequest)
		return
	}
	before := int64(0)
	var err error
	if raw := query.Get("before_sequence"); raw != "" {
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before < 0 {
			http.Error(w, "invalid before_sequence", http.StatusBadRequest)
			return
		}
	}
	filter, err := parseRemoteOperationEventFilter(query)
	if err != nil {
		http.Error(w, "invalid remote operation event filters", http.StatusBadRequest)
		return
	}
	_, limit, err := pagination(r, 20)
	if err != nil || limit > remoteOperationPageLimit {
		http.Error(w, "invalid remote operation event page", http.StatusBadRequest)
		return
	}
	events, nextBefore, more, err := s.backend.CloudOperationEventPage(r.Context(), r.PathValue("id"), filter, before, limit)
	if errors.Is(err, taskstore.ErrRemoteOperationNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "操作事件读取失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	for i := range events {
		evidence, maskErr := MaskSensitiveEvidence(events[i].Evidence)
		if maskErr != nil {
			http.Error(w, "操作事件读取失败，请稍后重试。", http.StatusServiceUnavailable)
			return
		}
		events[i].Evidence = evidence
	}
	var next *int64
	if more {
		next = &nextBefore
	}
	if stringIdentities {
		var cursor *string
		if more {
			value := strconv.FormatInt(nextBefore, 10)
			cursor = &value
		}
		jsonResponse(w, map[string]any{"events": operationEventStringResponse(events), "next_before_sequence": cursor})
		return
	}
	jsonResponse(w, map[string]any{"events": events, "next_before_sequence": next})
}

func parseRemoteOperationEventFilter(query url.Values) (taskstore.RemoteOperationEventFilter, error) {
	if len(query["status"]) > 1 || len(query["recorded_from_utc"]) > 1 || len(query["recorded_through_utc"]) > 1 {
		return taskstore.RemoteOperationEventFilter{}, errors.New("duplicate event filter")
	}
	filter := taskstore.RemoteOperationEventFilter{Status: query.Get("status")}
	if filter.Status != "" && filter.Status != "prepared" && filter.Status != "confirmed" && filter.Status != "unknown" && filter.Status != "failed" {
		return taskstore.RemoteOperationEventFilter{}, errors.New("invalid event status")
	}
	from, err := parseRemoteOperationEventDate(query.Get("recorded_from_utc"))
	if err != nil {
		return taskstore.RemoteOperationEventFilter{}, err
	}
	through, err := parseRemoteOperationEventDate(query.Get("recorded_through_utc"))
	if err != nil {
		return taskstore.RemoteOperationEventFilter{}, err
	}
	if from != nil {
		filter.RecordedFromUTC = from
	}
	if through != nil {
		endExclusive := through.AddDate(0, 0, 1)
		if !time.Unix(0, endExclusive.UnixNano()).Equal(endExclusive.UTC()) {
			return taskstore.RemoteOperationEventFilter{}, errors.New("event date outside supported range")
		}
		filter.RecordedBeforeUTC = &endExclusive
	}
	if filter.RecordedFromUTC != nil && filter.RecordedBeforeUTC != nil && !filter.RecordedFromUTC.Before(*filter.RecordedBeforeUTC) {
		return taskstore.RemoteOperationEventFilter{}, errors.New("invalid event date range")
	}
	return filter, nil
}

func parseRemoteOperationEventDate(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil || parsed.Format("2006-01-02") != value || !time.Unix(0, parsed.UnixNano()).Equal(parsed.UTC()) {
		return nil, errors.New("invalid UTC event date")
	}
	return &parsed, nil
}

func (s *Server) HandleCloudOperationFilteredEvidenceExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !method(w, r, http.MethodGet) {
		return
	}
	filter, err := parseRemoteOperationEventFilter(r.URL.Query())
	if err != nil {
		http.Error(w, "操作事件筛选条件无效。", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	if len(id) != 32 {
		http.NotFound(w, r)
		return
	}
	if _, err = hex.DecodeString(id); err != nil {
		http.NotFound(w, r)
		return
	}
	bundle, err := s.backend.CloudOperationEvidenceExportFiltered(r.Context(), id, filter)
	if errors.Is(err, taskstore.ErrRemoteOperationNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, taskstore.ErrRemoteOperationEvidenceExportTooLarge) {
		http.Error(w, "当前筛选命中超过 100 条事件，未导出部分证据；请缩小 UTC 日期或状态范围。", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "操作事件证据导出失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	for i := range bundle.Events {
		evidence, maskErr := MaskSensitiveEvidence(bundle.Events[i].Evidence)
		if maskErr != nil {
			http.Error(w, "操作事件证据导出失败，请稍后重试。", http.StatusServiceUnavailable)
			return
		}
		bundle.Events[i].Evidence = evidence
	}
	filterValue := map[string]any{"status": filter.Status}
	if filter.RecordedFromUTC != nil {
		filterValue["recorded_from_utc"] = filter.RecordedFromUTC.Format("2006-01-02")
	}
	if filter.RecordedBeforeUTC != nil {
		filterValue["recorded_through_utc"] = filter.RecordedBeforeUTC.AddDate(0, 0, -1).Format("2006-01-02")
	}
	now := time.Now().UTC()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="remote-operation-events-`+id+`.json"`)
	_ = json.NewEncoder(w).Encode(map[string]any{"exported_at_utc": now, "operation": bundle.Operation, "filter": filterValue, "events": bundle.Events, "total_events": len(bundle.Events)})
}

func (s *Server) HandleCloudOperationEvidenceExport(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	id := r.PathValue("id")
	if len(id) != 32 {
		http.NotFound(w, r)
		return
	}
	if _, err := hex.DecodeString(id); err != nil {
		http.NotFound(w, r)
		return
	}
	bundle, err := s.backend.CloudOperationEvidenceExport(r.Context(), id)
	if errors.Is(err, taskstore.ErrRemoteOperationNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, taskstore.ErrRemoteOperationEvidenceExportTooLarge) {
		http.Error(w, "操作事件超过 100 条，完整证据无法导出；请在页面逐页查看", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "操作证据导出失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	for i := range bundle.Events {
		evidence, maskErr := MaskSensitiveEvidence(bundle.Events[i].Evidence)
		if maskErr != nil {
			http.Error(w, "操作证据导出失败，请稍后重试。", http.StatusServiceUnavailable)
			return
		}
		bundle.Events[i].Evidence = evidence
	}
	now := time.Now().UTC()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="remote-operation-`+id+`.json"`)
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"exported_at_utc": now,
		"operation":       bundle.Operation,
		"events":          bundle.Events,
		"total_events":    len(bundle.Events),
	})
}

func MaskSensitiveEvidence(raw json.RawMessage) (json.RawMessage, error) {
	var value any
	// Keep numeric evidence exact: float64 decoding would alter fs_ids before
	// the browser or an exported evidence file ever receives them.
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("invalid trailing evidence JSON")
	}
	maskRemoteEvidenceValue(value)
	return json.Marshal(value)
}

func maskRemoteEvidenceValue(value any) {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			if sensitiveRemoteEvidenceKey(key) {
				node[key] = "[REDACTED]"
				continue
			}
			maskRemoteEvidenceValue(child)
		}
	case []any:
		for _, child := range node {
			maskRemoteEvidenceValue(child)
		}
	}
}

func sensitiveRemoteEvidenceKey(key string) bool {
	key = strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key))
	for _, marker := range []string{"token", "secret", "password", "authorization", "appkey", "signkey"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}
