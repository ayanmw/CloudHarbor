package intakeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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
	ListCachedCloud(context.Context, string, int, int) ([]taskstore.CloudInventoryEntry, taskstore.CloudInventoryDirectory, int, error)
	GetCachedCloudDirectorySize(context.Context, string) (taskstore.CloudDirectorySize, error)
	CloudInventoryScanStatus() (taskstore.CloudScanState, error)
	StartCloudInventoryScan(context.Context) (taskstore.CloudScanState, error)
	PauseCloudInventoryScan()
	CloudDuplicateScanStatus() (taskstore.CloudDuplicateScanState, error)
	StartCloudDuplicateScan(context.Context) (taskstore.CloudDuplicateScanState, error)
	PauseCloudDuplicateScan()
	CloudDuplicatePreview() ([]taskstore.CloudDuplicateGroup, error)
	PreviewEmptyCloudDirectories(context.Context) ([]taskstore.CloudInventoryEntry, error)
	MutateCloud(context.Context, downloader.CloudMutationRequest) (downloader.CloudMutationResult, error)
}

func (s *Server) HandleEmptyCloudDirectories(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	items, err := s.backend.PreviewEmptyCloudDirectories(r.Context())
	if err != nil {
		failure(w, err)
		return
	}
	state, err := s.backend.CloudInventoryScanStatus()
	if err != nil {
		failure(w, err)
		return
	}
	jsonResponse(w, map[string]any{"directories": items, "scan_run_id": state.RunID, "observed_at_utc": state.UpdatedAtUTC, "status": state.Status})
}

func (s *Server) HandleDuplicateScan(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		state, err := s.backend.CloudDuplicateScanStatus()
		if err != nil {
			failure(w, err)
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
func (s *Server) HandleDuplicatePreview(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, http.MethodGet) {
		return
	}
	groups, err := s.backend.CloudDuplicatePreview()
	if err != nil {
		failure(w, err)
		return
	}
	jsonResponse(w, map[string]any{"groups": groups})
}

type Server struct {
	backend Backend
	scan    func()
}

func New(backend Backend, scan func()) *Server { return &Server{backend: backend, scan: scan} }
func jsonResponse(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
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
		failure(w, err)
		return
	}
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = "/"
	}
	result, err := s.backend.ListCloud(r.Context(), dir, offset, limit)
	if err != nil {
		readFailure(w, err)
		return
	}
	jsonResponse(w, result)
}

func (s *Server) HandleInventory(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	offset, limit, err := pagination(r, 100)
	if err != nil {
		failure(w, err)
		return
	}
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		dir = "/"
	}
	items, metadata, total, err := s.backend.ListCachedCloud(r.Context(), dir, offset, limit)
	if err != nil {
		failure(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"directory": dir, "entries": items, "metadata": metadata, "total": total, "offset": offset, "next_offset": func() *int {
		if offset+len(items) < total {
			n := offset + len(items)
			return &n
		}
		return nil
	}()})
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
		failure(w, err)
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
			failure(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state)
	case http.MethodPost:
		state, err := s.backend.StartCloudInventoryScan(r.Context())
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
	offset, limit, err := pagination(r, 20)
	if err != nil {
		readFailure(w, err)
		return
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
	total := len(records)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	result := map[string]any{"records": records[offset:end], "total": total, "offset": offset}
	if end < total {
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
