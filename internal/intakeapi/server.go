package intakeapi

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/downloader"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type Backend interface {
	ListCloud(context.Context, string, int, int) (downloader.CloudListing, error)
	PreviewIntake(context.Context, []string) (downloader.Intake, error)
	CommitIntake(context.Context, string) (downloader.Intake, error)
	RecheckIntake(context.Context, string) (downloader.Intake, error)
	GetIntakes() []downloader.Intake
	ListCachedCloud(context.Context, string, int, int, *int64) ([]taskstore.CloudInventoryEntry, taskstore.CloudInventoryDirectory, int, error)
	ListIncompleteCloudInventoryDirectories(context.Context, string, string, string, int, *int64) (taskstore.CloudInventoryIncompleteDirectoryPage, error)
	SearchCachedCloud(context.Context, string, int, int, *int64) ([]taskstore.CloudInventoryEntry, int, taskstore.CloudInventorySearchCoverage, error)
	GetCachedCloudDirectorySize(context.Context, string) (taskstore.CloudDirectorySize, error)
	CloudInventoryScanStatus() (taskstore.CloudScanState, error)
	StartCloudInventoryScan(context.Context, int64) (taskstore.CloudScanState, error)
	StartCloudInventoryScanWithLimit(context.Context, int64, string) (taskstore.CloudScanState, error)
	CloudScanHistoryRuns(context.Context, int64, taskstore.CloudScanRunFilter, int) ([]taskstore.CloudScanRun, int64, bool, error)
	CloudScanHistoryEvents(context.Context, string, int64, int) ([]taskstore.CloudScanEvent, int64, bool, error)
	CloudScanHistoryExport(context.Context, string) (taskstore.CloudScanEvidenceBundle, error)
	QueueMonitorScanHistoryRuns(context.Context, int64, taskstore.QueueMonitorScanFilter, int) ([]taskstore.QueueMonitorScanRun, int64, bool, error)
	QueueMonitorScanHistoryEvents(context.Context, string, int64, int) ([]taskstore.QueueMonitorScanEvent, int64, bool, error)
	QueueMonitorScanHistoryExport(context.Context, string) (taskstore.QueueMonitorScanBundle, error)
	PauseCloudInventoryScan()
	CloudInventoryRefreshStatus() (taskstore.CloudInventoryRefreshState, error)
	CloudInventoryRefreshEventPage(context.Context, string, string, string, int64, int) ([]taskstore.CloudInventoryRefreshEvent, int64, bool, error)
	CloudInventoryRefreshChangePage(context.Context, string, int64, int64, int, string, string) (taskstore.CloudInventoryRefreshChangePage, error)
	CloudInventoryRefreshChangesExport(context.Context, string, int64, string, string) (taskstore.CloudInventoryRefreshChangeExport, error)
	CloudInventoryRefreshRuns(context.Context, int64, int) ([]taskstore.CloudInventoryRefreshRun, int64, bool, error)
	CloudInventoryRefreshRunsFiltered(context.Context, int64, taskstore.CloudInventoryRefreshRunFilter, int) ([]taskstore.CloudInventoryRefreshRun, int64, bool, error)
	CloudInventoryRefreshEvidenceExport(context.Context, string) (taskstore.CloudInventoryRefreshEvidenceBundle, error)
	CloudInventoryRefreshRunSummariesExport(context.Context, taskstore.CloudInventoryRefreshRunFilter) ([]taskstore.CloudInventoryRefreshRun, error)
	StartCloudInventoryRefresh(context.Context, string, int64) (taskstore.CloudInventoryRefreshState, error)
	StartCloudInventoryRefreshWithLimit(context.Context, string, int64, string) (taskstore.CloudInventoryRefreshState, error)
	PauseCloudInventoryRefresh()
	CloudDuplicateScanStatus() (taskstore.CloudDuplicateScanState, error)
	CloudDuplicateScanEstimate() (taskstore.CloudDuplicateHashCandidateEstimate, error)
	CloudDuplicateScanEventPage(context.Context, int64, int) ([]taskstore.CloudDuplicateScanEvent, int64, bool, error)
	CloudDuplicateScanRuns(context.Context, int64, taskstore.CloudDuplicateScanRunFilter, int) ([]taskstore.CloudDuplicateScanRun, int64, bool, error)
	CloudDuplicateScanRunsExport(context.Context, taskstore.CloudDuplicateScanRunFilter) ([]taskstore.CloudDuplicateScanRun, error)
	CloudDuplicateScanRunEventPage(context.Context, string, int64, int) ([]taskstore.CloudDuplicateScanEvent, int64, bool, error)
	CloudDuplicateScanEvidenceExport(context.Context, string) (taskstore.CloudDuplicateScanEvidenceBundle, error)
	StartCloudDuplicateScan(context.Context) (taskstore.CloudDuplicateScanState, error)
	PauseCloudDuplicateScan()
	CloudDuplicatePreview(context.Context, int, int) ([]taskstore.CloudDuplicateGroup, int, int, error)
	PreviewEmptyCloudDirectories(context.Context) ([]taskstore.CloudInventoryEntry, error)
	MutateCloud(context.Context, downloader.CloudMutationRequest) (downloader.CloudMutationResult, error)
	CloudOperationPage(context.Context, taskstore.RemoteOperationFilter, int, int) ([]taskstore.RemoteOperationSummary, int, error)
	CloudOperationCursorPage(context.Context, taskstore.RemoteOperationFilter, *taskstore.RemoteOperationCursor, int) ([]taskstore.RemoteOperationSummary, int, *taskstore.RemoteOperationCursor, error)
	CloudOperationExport(context.Context, taskstore.RemoteOperationFilter) ([]taskstore.RemoteOperationSummary, error)
	CloudOperationEventPage(context.Context, string, taskstore.RemoteOperationEventFilter, int64, int) ([]taskstore.RemoteOperationEvent, int64, bool, error)
	CloudOperationEvidenceExport(context.Context, string) (taskstore.RemoteOperationEvidenceBundle, error)
	CloudOperationEvidenceExportFiltered(context.Context, string, taskstore.RemoteOperationEventFilter) (taskstore.RemoteOperationEvidenceBundle, error)
	ExportLocalDirectoryMigrationEvidence(context.Context) (taskstore.LocalDirectoryMigrationEvidenceBundle, error)
	ListLocalDirectoryMigrations(context.Context, taskstore.LocalDirectoryMigrationFilter, int, int) ([]taskstore.LocalDirectoryMigration, int, error)
	ExportLocalDirectoryMigrationEvidenceByID(context.Context, string) (taskstore.LocalDirectoryMigrationEvidenceBundle, error)
	ExportLocalDirectoryMigrationSummaries(context.Context, taskstore.LocalDirectoryMigrationFilter) ([]taskstore.LocalDirectoryMigration, error)
	LocalDirectoryMigrationEventPage(context.Context, string, int64, int) ([]taskstore.LocalDirectoryMigrationEvidenceEvent, int64, bool, error)
}

func (s *Server) HandleLocalDirectoryMigrations(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	query := r.URL.Query()
	if len(query["offset"]) > 1 || len(query["limit"]) > 1 || len(query["status"]) > 1 || len(query["q"]) > 1 {
		http.Error(w, "分页参数无效。", http.StatusBadRequest)
		return
	}
	offset, limit := 0, 20
	var err error
	if value := query.Get("offset"); value != "" {
		offset, err = strconv.Atoi(value)
		if err != nil || offset < 0 {
			http.Error(w, "分页参数无效。", http.StatusBadRequest)
			return
		}
	}
	if value := query.Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > taskstore.LocalDirectoryMigrationPageLimit {
			http.Error(w, "分页参数无效。", http.StatusBadRequest)
			return
		}
	}
	filter, err := localDirectoryMigrationFilterFromRequest(r)
	if err != nil {
		http.Error(w, "筛选条件无效。", http.StatusBadRequest)
		return
	}
	entries, total, err := s.backend.ListLocalDirectoryMigrations(r.Context(), filter, offset, limit)
	if err != nil {
		http.Error(w, "读取本地目录迁移历史失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	var nextOffset *int
	if offset <= total && len(entries) <= total-offset && offset+len(entries) < total {
		next := offset + len(entries)
		nextOffset = &next
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonResponse(w, map[string]any{"entries": entries, "total": total, "offset": offset, "limit": limit, "next_offset": nextOffset})
}

func localDirectoryMigrationFilterFromRequest(r *http.Request) (taskstore.LocalDirectoryMigrationFilter, error) {
	query := r.URL.Query()
	if len(query["status"]) > 1 || len(query["q"]) > 1 || len(query["started_from_utc"]) > 1 || len(query["started_through_utc"]) > 1 {
		return taskstore.LocalDirectoryMigrationFilter{}, fmt.Errorf("duplicate migration filter")
	}
	filter := taskstore.LocalDirectoryMigrationFilter{Status: query.Get("status"), Query: strings.TrimSpace(query.Get("q")), StartedFromUTC: query.Get("started_from_utc"), StartedThroughUTC: query.Get("started_through_utc")}
	if (filter.Status != "" && filter.Status != "running" && filter.Status != "failed" && filter.Status != "completed") || !utf8.ValidString(filter.Query) || len(filter.Query) > 256 {
		return taskstore.LocalDirectoryMigrationFilter{}, fmt.Errorf("invalid migration filter")
	}
	for _, date := range []string{filter.StartedFromUTC, filter.StartedThroughUTC} {
		if date == "" {
			continue
		}
		parsed, err := time.Parse("2006-01-02", date)
		if err != nil || parsed.Format("2006-01-02") != date {
			return taskstore.LocalDirectoryMigrationFilter{}, fmt.Errorf("invalid migration UTC date")
		}
	}
	if filter.StartedFromUTC != "" && filter.StartedThroughUTC != "" && filter.StartedFromUTC > filter.StartedThroughUTC {
		return taskstore.LocalDirectoryMigrationFilter{}, fmt.Errorf("invalid migration UTC date range")
	}
	return filter, nil
}

func (s *Server) HandleLocalDirectoryMigrationSummaryExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !method(w, r, http.MethodGet) {
		return
	}
	filter, err := localDirectoryMigrationFilterFromRequest(r)
	if err != nil {
		http.Error(w, "筛选条件无效。", http.StatusBadRequest)
		return
	}
	items, err := s.backend.ExportLocalDirectoryMigrationSummaries(r.Context(), filter)
	if errors.Is(err, taskstore.ErrLocalMigrationSummaryExportTooLarge) {
		http.Error(w, "筛选结果超过 5000 条，未生成部分导出文件。", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "导出迁移历史摘要失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="local-migration-history.json"`)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"exported_at_utc": time.Now().UTC(),
		"filter":          filter,
		"total":           len(items),
		"migrations":      items,
	})
}

func (s *Server) HandleLocalDirectoryMigrationEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !method(w, r, http.MethodGet) {
		return
	}
	id := r.PathValue("id")
	decoded, decodeErr := hex.DecodeString(id)
	if decodeErr != nil || len(id) != 32 || len(decoded) != 16 {
		http.Error(w, "迁移记录编号无效。", http.StatusBadRequest)
		return
	}
	query := r.URL.Query()
	if len(query["before_sequence"]) > 1 || len(query["limit"]) > 1 {
		http.Error(w, "分页参数无效。", http.StatusBadRequest)
		return
	}
	before, limit := int64(0), 20
	var err error
	if value := query.Get("before_sequence"); value != "" {
		before, err = strconv.ParseInt(value, 10, 64)
		if err != nil || before < 0 {
			http.Error(w, "分页参数无效。", http.StatusBadRequest)
			return
		}
	}
	if value := query.Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > taskstore.LocalDirectoryMigrationEventPageLimit {
			http.Error(w, "分页参数无效。", http.StatusBadRequest)
			return
		}
	}
	events, nextBefore, hasMore, err := s.backend.LocalDirectoryMigrationEventPage(r.Context(), id, before, limit)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "本地目录迁移记录不存在。", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "读取本地目录迁移事件失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	for i := range events {
		evidence, maskErr := MaskSensitiveEvidence(events[i].Evidence)
		if maskErr != nil {
			http.Error(w, "读取本地目录迁移事件失败，请稍后重试。", http.StatusServiceUnavailable)
			return
		}
		events[i].Evidence = evidence
	}
	jsonResponse(w, map[string]any{"migration_id": id, "events": events, "next_before_sequence": nextBefore, "has_more": hasMore})
}

func (s *Server) HandleLocalDirectoryMigrationEvidenceExport(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	bundle, err := s.backend.ExportLocalDirectoryMigrationEvidence(r.Context())
	s.writeLocalDirectoryMigrationEvidence(w, bundle, err)
}

func (s *Server) HandleLocalDirectoryMigrationEvidenceExportByID(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	id := r.PathValue("id")
	decoded, decodeErr := hex.DecodeString(id)
	if decodeErr != nil || len(id) != 32 || len(decoded) != 16 {
		http.Error(w, "迁移记录编号无效。", http.StatusBadRequest)
		return
	}
	bundle, err := s.backend.ExportLocalDirectoryMigrationEvidenceByID(r.Context(), id)
	s.writeLocalDirectoryMigrationEvidence(w, bundle, err)
}

func (s *Server) writeLocalDirectoryMigrationEvidence(w http.ResponseWriter, bundle taskstore.LocalDirectoryMigrationEvidenceBundle, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "尚无本地目录迁移记录。", http.StatusNotFound)
		return
	}
	if errors.Is(err, taskstore.ErrLocalMigrationEvidenceTooLarge) {
		http.Error(w, "迁移事件超过 100 条，完整证据无法导出。", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "读取本地目录迁移证据失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	for i := range bundle.Events {
		evidence, maskErr := MaskSensitiveEvidence(bundle.Events[i].Evidence)
		if maskErr != nil {
			http.Error(w, "读取本地目录迁移证据失败，请稍后重试。", http.StatusServiceUnavailable)
			return
		}
		bundle.Events[i].Evidence = evidence
	}
	now := time.Now().UTC()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="local-migration-`+bundle.Migration.ID+`.json"`)
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"exported_at_utc": now,
		"migration":       bundle.Migration,
		"events":          bundle.Events,
	})
}

func (s *Server) HandleEmptyCloudDirectories(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	items, err := s.backend.PreviewEmptyCloudDirectories(r.Context())
	if err != nil {
		readFailure(w, err)
		return
	}
	state, err := s.backend.CloudInventoryScanStatus()
	if err != nil {
		readFailure(w, err)
		return
	}
	jsonResponse(w, map[string]any{"directories": items, "scan_run_id": state.RunID, "observed_at_utc": state.UpdatedAtUTC, "status": state.Status})
}

func (s *Server) HandleDuplicateScan(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("estimate") == "1" {
			estimate, err := s.backend.CloudDuplicateScanEstimate()
			if err != nil {
				readFailure(w, err)
				return
			}
			jsonResponse(w, estimate)
			return
		}
		state, err := s.backend.CloudDuplicateScanStatus()
		if err != nil {
			readFailure(w, err)
			return
		}
		jsonResponse(w, state)
	case http.MethodPost:
		state, err := s.backend.StartCloudDuplicateScan(r.Context())
		if err != nil {
			failure(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		jsonResponse(w, state)
	case http.MethodDelete:
		s.backend.PauseCloudDuplicateScan()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) HandleDuplicateScanEvents(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	query := r.URL.Query()
	if len(query["before_sequence"]) > 1 || len(query["limit"]) > 1 || query.Has("offset") {
		http.Error(w, "invalid duplicate scan event page", http.StatusBadRequest)
		return
	}
	before := int64(0)
	var err error
	if raw := query.Get("before_sequence"); raw != "" {
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before < 0 {
			http.Error(w, "invalid duplicate scan event page", http.StatusBadRequest)
			return
		}
	}
	limit := 20
	if raw := query.Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 50 {
			http.Error(w, "invalid duplicate scan event page", http.StatusBadRequest)
			return
		}
	}
	events, nextBefore, more, err := s.backend.CloudDuplicateScanEventPage(r.Context(), before, limit)
	if err != nil {
		readFailure(w, err)
		return
	}
	if events == nil {
		events = []taskstore.CloudDuplicateScanEvent{}
	}
	for i := range events {
		evidence, maskErr := MaskSensitiveEvidence(events[i].Evidence)
		if maskErr != nil {
			http.Error(w, "重复扫描事件读取失败，请稍后重试。", http.StatusServiceUnavailable)
			return
		}
		events[i].Evidence = evidence
	}
	var next *int64
	if more {
		next = &nextBefore
	}
	jsonResponse(w, map[string]any{"events": events, "next_before_sequence": next})
}

func (s *Server) HandleDuplicatePreview(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	query := r.URL.Query()
	if len(query["offset"]) > 1 || len(query["limit"]) > 1 {
		http.Error(w, "invalid duplicate preview page", http.StatusBadRequest)
		return
	}
	offset, limit, err := pagination(r, 25)
	if err != nil || limit > 50 {
		http.Error(w, "invalid duplicate preview page", http.StatusBadRequest)
		return
	}
	groups, total, deleteCount, err := s.backend.CloudDuplicatePreview(r.Context(), offset, limit)
	if err != nil {
		readFailure(w, err)
		return
	}
	var nextOffset *int
	if offset+len(groups) < total {
		next := offset + len(groups)
		nextOffset = &next
	}
	jsonResponse(w, map[string]any{"groups": groups, "total": total, "delete_count": deleteCount, "offset": offset, "limit": limit, "next_offset": nextOffset})
}

type Server struct {
	backend Backend
	scan    func()
}

func New(backend Backend, scan func()) *Server { return &Server{backend: backend, scan: scan} }
func jsonResponse(w http.ResponseWriter, value any) {
	jsonResponseStatus(w, http.StatusOK, value)
}
func jsonResponseStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func method(w http.ResponseWriter, r *http.Request, want string) bool {
	if r.Method != want {
		w.Header().Set("Allow", want)
		http.Error(w, "Method not allowed", 405)
		return false
	}
	return true
}
func pagination(r *http.Request, defaultLimit int) (int, int, error) {
	offset, limit := 0, defaultLimit
	var err error
	if value := r.URL.Query().Get("offset"); value != "" {
		offset, err = strconv.Atoi(value)
		if err != nil {
			return 0, 0, downloader.ErrIntakeInvalid
		}
	}
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil {
			return 0, 0, downloader.ErrIntakeInvalid
		}
	}
	if offset < 0 || offset > 1000000 || limit < 1 || limit > 100 {
		return 0, 0, downloader.ErrIntakeInvalid
	}
	return offset, limit, nil
}

func optionalGeneration(r *http.Request) (*int64, error) {
	values, ok := r.URL.Query()["cache_generation"]
	if !ok {
		return nil, nil
	}
	if len(values) != 1 || values[0] == "" {
		return nil, downloader.ErrIntakeInvalid
	}
	generation, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || generation < 0 {
		return nil, downloader.ErrIntakeInvalid
	}
	return &generation, nil
}

type intakeRecordCursor struct {
	Created string `json:"created"`
	ID      string `json:"id"`
}

func encodeIntakeRecordCursor(record downloader.Intake) string {
	payload, _ := json.Marshal(intakeRecordCursor{Created: record.Created.UTC().Format(time.RFC3339Nano), ID: record.ID})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeIntakeRecordCursor(value string) (time.Time, string, error) {
	if value == "" || len(value) > 512 {
		return time.Time{}, "", downloader.ErrIntakeInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || base64.RawURLEncoding.EncodeToString(payload) != value {
		return time.Time{}, "", downloader.ErrIntakeInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	var cursor intakeRecordCursor
	if err = decoder.Decode(&cursor); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return time.Time{}, "", downloader.ErrIntakeInvalid
	}
	created, err := time.Parse(time.RFC3339Nano, cursor.Created)
	if err != nil || created.IsZero() || len(cursor.ID) != 32 || strings.ToLower(cursor.ID) != cursor.ID {
		return time.Time{}, "", downloader.ErrIntakeInvalid
	}
	if _, err = hex.DecodeString(cursor.ID); err != nil {
		return time.Time{}, "", downloader.ErrIntakeInvalid
	}
	return created, cursor.ID, nil
}
func decode(w http.ResponseWriter, r *http.Request, value any, allowEmpty bool) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(value)
	if allowEmpty && err == io.EOF {
		return true
	}
	if err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "invalid intake request", 400)
		return false
	}
	return true
}
func failure(w http.ResponseWriter, err error) {
	status, message := 502, "网盘请求失败，请查看账号状态或稍后重新核对；不要盲目重发移动。"
	switch {
	case errors.Is(err, downloader.ErrCloudInventoryIncomplete):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, downloader.ErrIntakeInvalid):
		status, message = 400, err.Error()
	case errors.Is(err, downloader.ErrIntakeNotFound):
		status, message = 404, err.Error()
	case errors.Is(err, downloader.ErrIntakeConflict), errors.Is(err, downloader.ErrIntakeStale), errors.Is(err, downloader.ErrIntakeUnconfirmed):
		status, message = 409, err.Error()
	case errors.Is(err, downloader.ErrAuthenticationWaiting):
		status, message = 409, "请先完成百度账号授权或续期。"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, message = 408, "请求已取消或超时；请查看入队记录，不要直接重发移动。"
	}
	http.Error(w, message, status)
}

// readFailure is for operations that only inspect cloud state. Unlike a move
// or delete failure, these requests are safe to repeat; expose only the
// structured provider status/code, never an arbitrary error string that may
// contain a request URL or credential.
func readFailure(w http.ResponseWriter, err error) {
	status, message := http.StatusBadGateway, "读取网盘信息失败；请检查账号授权、网络和目录路径，读取操作可安全重试。"
	switch {
	case errors.Is(err, downloader.ErrCloudInventoryIncomplete):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, downloader.ErrIntakeInvalid):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, downloader.ErrIntakeNotFound):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, downloader.ErrIntakeConflict), errors.Is(err, downloader.ErrIntakeStale), errors.Is(err, downloader.ErrIntakeUnconfirmed):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, downloader.ErrAuthenticationWaiting):
		status, message = http.StatusConflict, "请先完成百度账号授权或续期。"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, message = http.StatusRequestTimeout, "读取请求已取消或超时；只读查询可安全重试。"
	default:
		var providerError *baiduapi.APIError
		if errors.As(err, &providerError) {
			message = fmt.Sprintf("读取网盘信息失败（HTTP=%d，百度错误码=%d）；请核对账号、路径和权限，读取操作可安全重试。", providerError.Status, providerError.Code)
			if providerError.RetryAfter > 0 {
				seconds := int64((providerError.RetryAfter + time.Second - 1) / time.Second)
				w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
				message += fmt.Sprintf(" 百度建议等待约 %d 秒后再重试。", seconds)
			}
		} else {
			var dnsError *net.DNSError
			var networkError net.Error
			var operationError *net.OpError
			switch {
			case errors.As(err, &dnsError):
				message = "无法解析百度网盘域名，请检查 NAS DNS/网络；修复后只读查询可安全重试。"
			case errors.As(err, &networkError) && networkError.Timeout():
				message = "连接百度网盘超时，请检查 NAS 网络/代理；稍后只读查询可安全重试。"
			case errors.As(err, &operationError) && operationError.Op == "dial":
				message = "无法建立网盘连接，请检查 NAS 网络/代理；修复后只读查询可安全重试。"
			case errors.As(err, &operationError):
				message = "读取网盘响应时网络中断，请检查 NAS 网络；修复后只读查询可安全重试。"
			}
		}
	}
	http.Error(w, message, status)
}

func (s *Server) HandleCloud(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	offset, limit, err := pagination(r, 100)
	if err != nil {
		readFailure(w, err)
		return
	}
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = "/"
	}
	result, err := s.backend.ListCloud(r.Context(), dir, offset, limit)
	reanchored := false
	if offset > 0 && errors.Is(err, baiduapi.ErrDuplicatePageItem) {
		result, err = s.backend.ListCloud(r.Context(), dir, 0, limit)
		if err == nil {
			result.BoundaryChanged = true
			result.BoundaryChangeReason = "provider_duplicate_page_id"
			reanchored = true
		}
	}
	if err != nil {
		readFailure(w, err)
		return
	}
	if raw := r.URL.Query().Get("expected_boundary"); raw != "" {
		var expected downloader.CloudPageBoundary
		if offset == 0 || len(raw) > 4096 || json.Unmarshal([]byte(raw), &expected) != nil || len(expected.Fingerprint) != 64 {
			http.Error(w, "分页边界无效", http.StatusBadRequest)
			return
		}
		if _, err := hex.DecodeString(expected.Fingerprint); err != nil {
			http.Error(w, "分页边界无效", http.StatusBadRequest)
			return
		}
		if !reanchored {
			matches := false
			if len(result.Entries) > 0 {
				entry := result.Entries[0]
				entryBoundary := downloader.CloudPageBoundary{FsID: entry.FsID, Path: entry.Path, Name: entry.Name, Directory: entry.Directory, Size: entry.ProviderSize, ProviderMtime: entry.Mtime, ContentMD5: entry.MD5}
				matches = strings.EqualFold(expected.Fingerprint, entryBoundary.ComputeFingerprint())
			}
			if !matches {
				result, err = s.backend.ListCloud(r.Context(), dir, 0, limit)
				if err != nil {
					readFailure(w, err)
					return
				}
				result.BoundaryChanged = true
				result.BoundaryChangeReason = "page_boundary_mismatch"
			}
		}
	}
	jsonResponse(w, result)
}

func (s *Server) HandleInventory(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	offset, limit, err := pagination(r, 100)
	if err != nil {
		readFailure(w, err)
		return
	}
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = "/"
	}
	expected, err := optionalGeneration(r)
	if err != nil {
		http.Error(w, "缓存代次参数无效", http.StatusBadRequest)
		return
	}
	items, metadata, total, err := s.backend.ListCachedCloud(r.Context(), dir, offset, limit, expected)
	if err != nil {
		if errors.Is(err, taskstore.ErrCloudInventoryGenerationChanged) {
			jsonResponseStatus(w, http.StatusConflict, map[string]any{"code": "cache_generation_changed", "cache_generation": metadata.Generation})
			return
		}
		readFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"directory": dir, "entries": items, "metadata": metadata, "cache_generation": metadata.Generation, "total": total, "offset": offset, "next_offset": func() *int {
		if offset+len(items) < total {
			n := offset + len(items)
			return &n
		}
		return nil
	}()})
}

func (s *Server) HandleIncompleteCloudInventoryDirectories(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	query := r.URL.Query()
	allowed := map[string]bool{"before_path": true, "reason": true, "q": true, "limit": true, "cache_generation": true}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			http.Error(w, "参数无效。", http.StatusBadRequest)
			return
		}
	}
	if (query.Get("before_path") != "" && !taskstore.IsValidCloudInventoryPath(query.Get("before_path"))) || !taskstore.IsValidCloudInventoryIncompleteReason(query.Get("reason")) || utf8.RuneCountInString(strings.TrimSpace(query.Get("q"))) > 128 {
		http.Error(w, "筛选参数无效。", http.StatusBadRequest)
		return
	}
	limit := 25
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			http.Error(w, "分页参数无效。", http.StatusBadRequest)
			return
		}
		limit = parsed
	}
	expected, err := optionalGeneration(r)
	if err != nil {
		http.Error(w, "缓存代次参数无效。", http.StatusBadRequest)
		return
	}
	page, err := s.backend.ListIncompleteCloudInventoryDirectories(r.Context(), query.Get("before_path"), query.Get("reason"), query.Get("q"), limit, expected)
	if errors.Is(err, taskstore.ErrCloudInventoryGenerationChanged) {
		jsonResponseStatus(w, http.StatusConflict, map[string]any{"code": "cache_generation_changed", "cache_generation": page.Generation})
		return
	}
	if err != nil {
		readFailure(w, err)
		return
	}
	jsonResponse(w, page)
}

func (s *Server) HandleInventorySearch(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" || utf8.RuneCountInString(query) > 128 {
		http.Error(w, "搜索内容须为 1 至 128 个字符", http.StatusBadRequest)
		return
	}
	offset, limit, err := pagination(r, 50)
	if err != nil {
		http.Error(w, "分页参数无效", http.StatusBadRequest)
		return
	}
	expected, err := optionalGeneration(r)
	if err != nil {
		http.Error(w, "缓存代次参数无效", http.StatusBadRequest)
		return
	}
	items, total, coverage, err := s.backend.SearchCachedCloud(r.Context(), query, offset, limit, expected)
	if err != nil {
		if errors.Is(err, taskstore.ErrCloudInventoryGenerationChanged) {
			jsonResponseStatus(w, http.StatusConflict, map[string]any{"code": "cache_generation_changed", "cache_generation": coverage.Generation})
			return
		}
		http.Error(w, "读取本地缓存搜索结果失败", http.StatusInternalServerError)
		return
	}
	var nextOffset *int
	if offset+len(items) < total {
		next := offset + len(items)
		nextOffset = &next
	}
	jsonResponse(w, map[string]any{"query": query, "entries": items, "total": total, "offset": offset, "next_offset": nextOffset, "cache_generation": coverage.Generation, "coverage": coverage})
}

func (s *Server) HandleInventorySize(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = "/"
	}
	size, err := s.backend.GetCachedCloudDirectorySize(r.Context(), dir)
	if err != nil {
		readFailure(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(size)
}

func (s *Server) HandleInventoryScan(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		state, err := s.backend.CloudInventoryScanStatus()
		if err != nil {
			readFailure(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state)
	case http.MethodPost:
		var request struct {
			RequestBudget    int64  `json:"request_budget"`
			RequestLimitMode string `json:"request_limit_mode"`
		}
		if !decode(w, r, &request, true) {
			return
		}
		if err := downloader.ValidateInventoryRequestLimit(request.RequestBudget, request.RequestLimitMode); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		state, err := s.backend.StartCloudInventoryScanWithLimit(r.Context(), request.RequestBudget, request.RequestLimitMode)
		if err != nil {
			failure(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(state)
	case http.MethodDelete:
		s.backend.PauseCloudInventoryScan()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) HandleCloudScanHistoryRuns(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	before, limit, ok := scanHistoryPaging(w, r, false)
	if !ok {
		return
	}
	filter, err := cloudScanRunFilterFromRequest(r)
	if err != nil {
		http.Error(w, "全盘扫描历史筛选条件无效。", http.StatusBadRequest)
		return
	}
	runs, next, more, err := s.backend.CloudScanHistoryRuns(r.Context(), before, filter, limit)
	if err != nil {
		readFailure(w, err)
		return
	}
	jsonResponse(w, map[string]any{"runs": runs, "next_before_sequence": next, "has_more": more})
}

func cloudScanRunFilterFromRequest(r *http.Request) (taskstore.CloudScanRunFilter, error) {
	query := r.URL.Query()
	filter := taskstore.CloudScanRunFilter{Status: query.Get("status"), StartedFromUTC: query.Get("started_from_utc"), StartedThroughUTC: query.Get("started_through_utc")}
	if (filter.Status != "" && !validRefreshStatusFilter(filter.Status)) || !validRefreshDateFilter(filter.StartedFromUTC) || !validRefreshDateFilter(filter.StartedThroughUTC) || (filter.StartedFromUTC != "" && filter.StartedThroughUTC != "" && filter.StartedFromUTC > filter.StartedThroughUTC) {
		return taskstore.CloudScanRunFilter{}, fmt.Errorf("invalid run or date filter")
	}
	for key, target := range map[string]*string{"coverage_status": &filter.CoverageStatus, "coverage_reason": &filter.CoverageReason, "incomplete_directories_min": nil, "incomplete_directories_max": nil} {
		values, exists := query[key]
		if !exists {
			continue
		}
		if len(values) != 1 {
			return taskstore.CloudScanRunFilter{}, fmt.Errorf("duplicate coverage filter")
		}
		if target != nil {
			*target = values[0]
		}
	}
	for _, item := range []struct {
		key    string
		target **int64
	}{{"incomplete_directories_min", &filter.CoverageIncompleteMin}, {"incomplete_directories_max", &filter.CoverageIncompleteMax}} {
		values, exists := query[item.key]
		if !exists || values[0] == "" {
			continue
		}
		value, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil {
			return taskstore.CloudScanRunFilter{}, fmt.Errorf("invalid coverage count filter")
		}
		*item.target = &value
	}
	if err := taskstore.ValidateCloudScanRunCoverageFilter(filter); err != nil {
		return taskstore.CloudScanRunFilter{}, err
	}
	return filter, nil
}

func (s *Server) HandleCloudScanHistoryEvents(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	before, limit, ok := scanHistoryPaging(w, r, true)
	if !ok {
		return
	}
	runID := r.URL.Query().Get("run_id")
	if runID == "" || len(runID) > 128 {
		http.Error(w, "扫描运行 ID 无效。", http.StatusBadRequest)
		return
	}
	events, next, more, err := s.backend.CloudScanHistoryEvents(r.Context(), runID, before, limit)
	if err != nil {
		readFailure(w, err)
		return
	}
	jsonResponse(w, map[string]any{"events": events, "next_before_sequence": next, "has_more": more})
}

func (s *Server) HandleCloudScanHistoryExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !method(w, r, http.MethodGet) {
		return
	}
	runID := r.PathValue("id")
	if len(runID) != 32 {
		http.NotFound(w, r)
		return
	}
	if _, err := hex.DecodeString(runID); err != nil {
		http.NotFound(w, r)
		return
	}
	bundle, err := s.backend.CloudScanHistoryExport(r.Context(), runID)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, taskstore.ErrCloudScanEvidenceTooLarge) {
		http.Error(w, "此轮全盘扫描事件超过导出上限；请在页面分页查看。", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "全盘扫描证据导出失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="cloud-scan-`+runID+`.json"`)
	events := make([]cloudScanEventExportDTO, 0, len(bundle.Events))
	for _, event := range bundle.Events {
		events = append(events, cloudScanEventExportDTOFrom(event))
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"exported_at_utc": time.Now().UTC(), "run": cloudScanRunExportDTOFrom(bundle.Run), "events": events, "total_events": len(events)})
}

type cloudScanRunExportDTO struct {
	Sequence                      int64                                    `json:"sequence"`
	RunID                         string                                   `json:"run_id"`
	Status                        string                                   `json:"status"`
	Phase                         string                                   `json:"phase"`
	CurrentDirectory              string                                   `json:"current_directory"`
	PagesProcessed                int64                                    `json:"pages_processed"`
	ItemsProcessed                int64                                    `json:"items_processed"`
	RequestBudget                 int64                                    `json:"request_budget"`
	RequestsUsed                  int64                                    `json:"requests_used"`
	PendingCount                  int64                                    `json:"pending_count"`
	StartedAtUTC                  time.Time                                `json:"started_at_utc"`
	UpdatedAtUTC                  time.Time                                `json:"updated_at_utc"`
	LastError                     string                                   `json:"last_error,omitempty"`
	CoverageStatus                *string                                  `json:"coverage_status"`
	CoverageIncompleteDirectories *int64                                   `json:"coverage_incomplete_directories"`
	CoverageGeneration            *int64                                   `json:"coverage_generation"`
	CoverageObservedAtUTC         *time.Time                               `json:"coverage_observed_at_utc"`
	CoverageReasons               *taskstore.CloudInventoryCoverageReasons `json:"coverage_reasons,omitempty"`
}

func cloudScanRunExportDTOFrom(run taskstore.CloudScanRun) cloudScanRunExportDTO {
	return cloudScanRunExportDTO{Sequence: run.Sequence, RunID: run.RunID, Status: run.Status, Phase: run.Phase, CurrentDirectory: run.CurrentDirectory, PagesProcessed: run.PagesProcessed, ItemsProcessed: run.ItemsProcessed, RequestBudget: run.RequestBudget, RequestsUsed: run.RequestsUsed, PendingCount: run.PendingCount, StartedAtUTC: run.StartedAtUTC, UpdatedAtUTC: run.UpdatedAtUTC, LastError: run.LastError, CoverageStatus: run.CoverageStatus, CoverageIncompleteDirectories: run.CoverageIncompleteDirectories, CoverageGeneration: run.CoverageGeneration, CoverageObservedAtUTC: run.CoverageObservedAtUTC, CoverageReasons: run.CoverageReasons}
}

type cloudScanEventExportDTO struct {
	Sequence         int64     `json:"sequence"`
	RunID            string    `json:"run_id"`
	Kind             string    `json:"kind"`
	Status           string    `json:"status"`
	Phase            string    `json:"phase"`
	CurrentDirectory string    `json:"current_directory"`
	PagesProcessed   int64     `json:"pages_processed"`
	ItemsProcessed   int64     `json:"items_processed"`
	RequestBudget    int64     `json:"request_budget"`
	RequestsUsed     int64     `json:"requests_used"`
	PendingCount     int64     `json:"pending_count"`
	RecordedAtUTC    time.Time `json:"recorded_at_utc"`
	LastError        string    `json:"last_error,omitempty"`
}

func cloudScanEventExportDTOFrom(event taskstore.CloudScanEvent) cloudScanEventExportDTO {
	return cloudScanEventExportDTO{Sequence: event.Sequence, RunID: event.RunID, Kind: event.Kind, Status: event.Status, Phase: event.Phase, CurrentDirectory: event.CurrentDirectory, PagesProcessed: event.PagesProcessed, ItemsProcessed: event.ItemsProcessed, RequestBudget: event.RequestBudget, RequestsUsed: event.RequestsUsed, PendingCount: event.PendingCount, RecordedAtUTC: event.RecordedAtUTC, LastError: event.LastError}
}

func scanHistoryPaging(w http.ResponseWriter, r *http.Request, withRun bool) (int64, int, bool) {
	query := r.URL.Query()
	allowed := map[string]bool{"before_sequence": true, "limit": true}
	if withRun {
		allowed["run_id"] = true
	} else {
		allowed["status"], allowed["started_from_utc"], allowed["started_through_utc"] = true, true, true
		allowed["coverage_status"], allowed["coverage_reason"] = true, true
		allowed["incomplete_directories_min"], allowed["incomplete_directories_max"] = true, true
	}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			http.Error(w, "扫描历史分页参数无效。", http.StatusBadRequest)
			return 0, 0, false
		}
	}
	before, limit := int64(0), 20
	var err error
	if value := query.Get("before_sequence"); value != "" {
		before, err = strconv.ParseInt(value, 10, 64)
		if err != nil || before <= 0 {
			http.Error(w, "扫描历史游标无效。", http.StatusBadRequest)
			return 0, 0, false
		}
	}
	if value := query.Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			http.Error(w, "扫描历史分页大小无效。", http.StatusBadRequest)
			return 0, 0, false
		}
	}
	return before, limit, true
}

func (s *Server) HandleInventoryRefresh(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		state, err := s.backend.CloudInventoryRefreshStatus()
		if err != nil {
			readFailure(w, err)
			return
		}
		jsonResponse(w, state)
	case http.MethodPost:
		var request struct {
			Directory        string `json:"directory"`
			RequestBudget    int64  `json:"request_budget"`
			RequestLimitMode string `json:"request_limit_mode"`
		}
		if !decode(w, r, &request, false) {
			return
		}
		if err := downloader.ValidateInventoryRequestLimit(request.RequestBudget, request.RequestLimitMode); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		state, err := s.backend.StartCloudInventoryRefreshWithLimit(r.Context(), request.Directory, request.RequestBudget, request.RequestLimitMode)
		if err != nil {
			failure(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(state)
	case http.MethodDelete:
		s.backend.PauseCloudInventoryRefresh()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) HandleInventoryRefreshRuns(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	query := r.URL.Query()
	for key, values := range query {
		if (key != "before_sequence" && key != "limit" && key != "status" && key != "started_from_utc" && key != "started_through_utc") || len(values) != 1 {
			http.Error(w, "刷新运行分页参数无效。", http.StatusBadRequest)
			return
		}
	}
	before, limit := int64(0), 20
	var err error
	if value := query.Get("before_sequence"); value != "" {
		before, err = strconv.ParseInt(value, 10, 64)
		if err != nil || before <= 0 {
			http.Error(w, "刷新运行游标无效。", http.StatusBadRequest)
			return
		}
	}
	if value := query.Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			http.Error(w, "刷新运行分页大小无效。", http.StatusBadRequest)
			return
		}
	}
	filter := taskstore.CloudInventoryRefreshRunFilter{Status: query.Get("status"), StartedFromUTC: query.Get("started_from_utc"), StartedThroughUTC: query.Get("started_through_utc")}
	if (filter.Status != "" && !validRefreshStatusFilter(filter.Status)) || !validRefreshDateFilter(filter.StartedFromUTC) || !validRefreshDateFilter(filter.StartedThroughUTC) || (filter.StartedFromUTC != "" && filter.StartedThroughUTC != "" && filter.StartedFromUTC > filter.StartedThroughUTC) {
		http.Error(w, "刷新运行筛选条件无效。", http.StatusBadRequest)
		return
	}
	runs, next, hasMore, err := s.backend.CloudInventoryRefreshRunsFiltered(r.Context(), before, filter, limit)
	if err != nil {
		readFailure(w, err)
		return
	}
	jsonResponse(w, map[string]any{"runs": runs, "next_before_sequence": next, "has_more": hasMore})
}

func validRefreshStatusFilter(status string) bool {
	return status == "running" || status == "paused" || status == "complete" || status == "failed"
}

func validRefreshDateFilter(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value && time.UnixMicro(parsed.UnixMicro()).Equal(parsed.UTC())
}

func (s *Server) HandleInventoryRefreshEvents(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	query := r.URL.Query()
	for key, values := range query {
		if (key != "run_id" && key != "kind" && key != "restart_reason" && key != "before_sequence" && key != "limit") || len(values) != 1 {
			http.Error(w, "刷新历史分页参数无效。", http.StatusBadRequest)
			return
		}
	}
	runID := query.Get("run_id")
	if runID == "" || len(runID) > 128 {
		http.Error(w, "刷新运行 ID 无效。", http.StatusBadRequest)
		return
	}
	kind := query.Get("kind")
	if kind != "" && !taskstore.IsCloudInventoryRefreshEventKind(kind) {
		http.Error(w, "刷新事件类型无效。", http.StatusBadRequest)
		return
	}
	restartReason := query.Get("restart_reason")
	if restartReason != "" && !taskstore.IsCloudInventoryRefreshRestartReason(restartReason) {
		http.Error(w, "分页重核原因无效。", http.StatusBadRequest)
		return
	}
	before, limit := int64(0), 20
	var err error
	if value := query.Get("before_sequence"); value != "" {
		before, err = strconv.ParseInt(value, 10, 64)
		if err != nil || before <= 0 {
			http.Error(w, "刷新历史游标无效。", http.StatusBadRequest)
			return
		}
	}
	if value := query.Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			http.Error(w, "刷新历史分页大小无效。", http.StatusBadRequest)
			return
		}
	}
	events, next, hasMore, err := s.backend.CloudInventoryRefreshEventPage(r.Context(), runID, kind, restartReason, before, limit)
	if err != nil {
		readFailure(w, err)
		return
	}
	jsonResponse(w, map[string]any{"run_id": runID, "events": events, "next_before_sequence": next, "has_more": hasMore})
}

func (s *Server) HandleInventoryRefreshChanges(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	query := r.URL.Query()
	for key, values := range query {
		if (key != "run_id" && key != "event_sequence" && key != "before_sequence" && key != "limit" && key != "kind" && key != "q") || len(values) != 1 {
			http.Error(w, "刷新变更明细分页参数无效。", http.StatusBadRequest)
			return
		}
	}
	runID := query.Get("run_id")
	if runID == "" || len(runID) > 128 {
		http.Error(w, "刷新运行 ID 无效。", http.StatusBadRequest)
		return
	}
	eventSequence, err := strconv.ParseInt(query.Get("event_sequence"), 10, 64)
	if err != nil || eventSequence <= 0 {
		http.Error(w, "刷新事件序号无效。", http.StatusBadRequest)
		return
	}
	before, limit := int64(0), 50
	if value := query.Get("before_sequence"); value != "" {
		before, err = strconv.ParseInt(value, 10, 64)
		if err != nil || before <= 0 {
			http.Error(w, "刷新变更明细游标无效。", http.StatusBadRequest)
			return
		}
	}
	if value := query.Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 50 {
			http.Error(w, "刷新变更明细分页大小无效。", http.StatusBadRequest)
			return
		}
	}
	kind, search := query.Get("kind"), strings.TrimSpace(query.Get("q"))
	if (kind != "" && !taskstore.IsCloudInventoryRefreshChangeKind(kind)) || utf8.RuneCountInString(search) > 128 {
		http.Error(w, "刷新变更明细筛选条件无效。", http.StatusBadRequest)
		return
	}
	page, err := s.backend.CloudInventoryRefreshChangePage(r.Context(), runID, eventSequence, before, limit, kind, search)
	if errors.Is(err, taskstore.ErrCloudInventoryRefreshNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		readFailure(w, err)
		return
	}
	jsonResponse(w, map[string]any{"run_id": runID, "event_sequence": eventSequence, "kind": kind, "query": search, "changes": page.Changes, "counts": page.Counts, "total_count": page.Counts.Total, "matched_count": page.MatchedCount, "next_before_sequence": page.NextBeforeSequence, "has_more": page.HasMore})
}

func (s *Server) HandleInventoryRefreshChangesExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !method(w, r, http.MethodGet) {
		return
	}
	query := r.URL.Query()
	for key, values := range query {
		if (key != "run_id" && key != "event_sequence" && key != "kind" && key != "q") || len(values) != 1 {
			http.Error(w, "刷新变更明细导出参数无效。", http.StatusBadRequest)
			return
		}
	}
	runID := query.Get("run_id")
	if runID == "" || len(runID) > 128 {
		http.Error(w, "刷新运行 ID 无效。", http.StatusBadRequest)
		return
	}
	eventSequence, err := strconv.ParseInt(query.Get("event_sequence"), 10, 64)
	if err != nil || eventSequence <= 0 {
		http.Error(w, "刷新事件序号无效。", http.StatusBadRequest)
		return
	}
	kind, search := query.Get("kind"), strings.TrimSpace(query.Get("q"))
	if (kind != "" && !taskstore.IsCloudInventoryRefreshChangeKind(kind)) || utf8.RuneCountInString(search) > 128 {
		http.Error(w, "刷新变更明细筛选条件无效。", http.StatusBadRequest)
		return
	}
	bundle, err := s.backend.CloudInventoryRefreshChangesExport(r.Context(), runID, eventSequence, kind, search)
	if errors.Is(err, taskstore.ErrCloudInventoryRefreshNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, taskstore.ErrCloudInventoryRefreshChangesExportTooLarge) {
		http.Error(w, "筛选后的目录变更明细超过 5,000 条，未生成部分文件。", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "目录变更明细导出失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="cloud-refresh-changes.json"`)
	_ = json.NewEncoder(w).Encode(map[string]any{"exported_at_utc": time.Now().UTC(), "run_id": bundle.RunID, "event_sequence": bundle.EventSequence, "recorded_at_utc": bundle.RecordedAtUTC, "cloud_observed_at_utc": bundle.CloudObservedAtUTC, "kind": bundle.Kind, "query": bundle.Query, "counts": bundle.Counts, "matched_count": bundle.MatchedCount, "changes": bundle.Changes})
}

func (s *Server) HandleInventoryRefreshEvidenceExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !method(w, r, http.MethodGet) {
		return
	}
	runID := r.PathValue("id")
	if len(runID) != 32 {
		http.NotFound(w, r)
		return
	}
	if _, err := hex.DecodeString(runID); err != nil {
		http.NotFound(w, r)
		return
	}
	bundle, err := s.backend.CloudInventoryRefreshEvidenceExport(r.Context(), runID)
	if errors.Is(err, taskstore.ErrCloudInventoryRefreshNotFound) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, taskstore.ErrCloudInventoryRefreshEvidenceExportTooLarge) {
		http.Error(w, "此轮刷新事件超过 100 条，完整证据无法导出；请在页面分页查看。", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "刷新运行证据导出失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="cloud-refresh-`+runID+`.json"`)
	_ = json.NewEncoder(w).Encode(map[string]any{"exported_at_utc": time.Now().UTC(), "run": bundle.Run, "events": bundle.Events, "total_events": len(bundle.Events)})
}

func (s *Server) HandleInventoryRefreshRunSummaryExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !method(w, r, http.MethodGet) {
		return
	}
	query := r.URL.Query()
	for key, values := range query {
		if (key != "status" && key != "started_from_utc" && key != "started_through_utc") || len(values) != 1 {
			http.Error(w, "刷新运行导出筛选参数无效。", http.StatusBadRequest)
			return
		}
	}
	filter := taskstore.CloudInventoryRefreshRunFilter{Status: query.Get("status"), StartedFromUTC: query.Get("started_from_utc"), StartedThroughUTC: query.Get("started_through_utc")}
	if (filter.Status != "" && !validRefreshStatusFilter(filter.Status)) || !validRefreshDateFilter(filter.StartedFromUTC) || !validRefreshDateFilter(filter.StartedThroughUTC) || (filter.StartedFromUTC != "" && filter.StartedThroughUTC != "" && filter.StartedFromUTC > filter.StartedThroughUTC) {
		http.Error(w, "刷新运行导出筛选条件无效。", http.StatusBadRequest)
		return
	}
	items, err := s.backend.CloudInventoryRefreshRunSummariesExport(r.Context(), filter)
	if errors.Is(err, taskstore.ErrCloudInventoryRefreshRunSummaryExportTooLarge) {
		http.Error(w, "筛选结果超过 5,000 条，请缩小状态或日期范围后再导出。", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		readFailure(w, err)
		return
	}
	now := time.Now().UTC()
	filterValue := map[string]string{}
	if filter.Status != "" {
		filterValue["status"] = filter.Status
	}
	if filter.StartedFromUTC != "" {
		filterValue["started_from_utc"] = filter.StartedFromUTC
	}
	if filter.StartedThroughUTC != "" {
		filterValue["started_through_utc"] = filter.StartedThroughUTC
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="cloud-refresh-runs-`+now.Format("20060102T150405Z")+`.json"`)
	_ = json.NewEncoder(w).Encode(map[string]any{"exported_at_utc": now, "filter": filterValue, "runs": items, "total_runs": len(items)})
}

func (s *Server) HandleCloudMutation(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodPost) {
		return
	}
	var request downloader.CloudMutationRequest
	if !decode(w, r, &request, false) {
		return
	}
	result, err := s.backend.MutateCloud(r.Context(), request)
	if err != nil {
		if result.OperationID != "" {
			if result.Status == "confirmed" {
				result.Warning = "远端操作已确认，但后续本地队列核对或缓存更新未完成；请检查监控路径并刷新。"
				jsonResponse(w, result)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"operation": result, "message": "操作结果请按状态重新核对；未知结果不会自动重发。"})
			return
		}
		failure(w, err)
		return
	}
	jsonResponse(w, result)
}
func (s *Server) HandlePreview(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	var request struct {
		Paths []string `json:"paths"`
	}
	if !decode(w, r, &request, false) {
		return
	}
	result, err := s.backend.PreviewIntake(r.Context(), request.Paths)
	if err != nil {
		readFailure(w, err)
		return
	}
	jsonResponse(w, result)
}
func (s *Server) HandleRecords(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	query := r.URL.Query()
	cursorValues, hasCursor := query["cursor"]
	cursorMode := hasCursor || !query.Has("offset")
	if hasCursor && (len(cursorValues) != 1 || query.Has("offset")) {
		readFailure(w, downloader.ErrIntakeInvalid)
		return
	}
	offset, limit, err := pagination(r, 20)
	if err != nil {
		readFailure(w, err)
		return
	}
	var cursorCreated time.Time
	var cursorID string
	if hasCursor {
		cursorCreated, cursorID, err = decodeIntakeRecordCursor(cursorValues[0])
		if err != nil {
			readFailure(w, err)
			return
		}
	}
	records := s.backend.GetIntakes()
	if r.URL.Query().Get("unresolved") == "1" {
		filtered := make([]downloader.Intake, 0)
		for _, record := range records {
			for _, item := range record.Items {
				if item.State == "unknown" || item.State == "sending" {
					filtered = append(filtered, record)
					break
				}
			}
		}
		records = filtered
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Created.Equal(records[j].Created) {
			return records[i].ID > records[j].ID
		}
		return records[i].Created.After(records[j].Created)
	})
	total := len(records)
	if hasCursor {
		remaining := make([]downloader.Intake, 0, len(records))
		for _, record := range records {
			if record.Created.Before(cursorCreated) || (record.Created.Equal(cursorCreated) && record.ID < cursorID) {
				remaining = append(remaining, record)
			}
		}
		records = remaining
		offset = 0
	} else if cursorMode {
		offset = 0
	} else if offset > total {
		offset = total
	}
	end := offset + limit
	if cursorMode {
		if end > len(records) {
			end = len(records)
		}
	} else if end > total {
		end = total
	}
	page := records[offset:end]
	result := map[string]any{"records": page, "total": total, "offset": offset, "limit": limit}
	if cursorMode {
		var nextCursor any
		if len(records) > end && len(page) > 0 {
			nextCursor = encodeIntakeRecordCursor(page[len(page)-1])
			if !hasCursor {
				result["next_offset"] = limit
			}
		}
		result["next_cursor"] = nextCursor
	} else if end < total {
		result["next_offset"] = end
	}
	jsonResponse(w, result)
}
func (s *Server) HandleAction(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/intakes/"), "/")
	if len(parts) != 2 || (parts[1] != "commit" && parts[1] != "recheck") {
		http.NotFound(w, r)
		return
	}
	if !decode(w, r, &struct{}{}, true) {
		return
	}
	var result downloader.Intake
	var err error
	if parts[1] == "commit" {
		result, err = s.backend.CommitIntake(r.Context(), parts[0])
	} else {
		result, err = s.backend.RecheckIntake(r.Context(), parts[0])
	}
	if err != nil {
		if parts[1] == "recheck" {
			readFailure(w, err)
		} else {
			failure(w, err)
		}
		return
	}
	if s.scan != nil {
		for _, item := range result.Items {
			if item.State == "moved" {
				s.scan()
				break
			}
		}
	}
	jsonResponse(w, result)
}
