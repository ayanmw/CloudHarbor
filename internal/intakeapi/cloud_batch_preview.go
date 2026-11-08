package intakeapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/downloader"
)

// POST carries bounded exact identities; this handler performs only cloud reads.
func (s *Server) HandleCloudBatchPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !method(w, r, http.MethodPost) {
		return
	}
	var wire struct {
		RequestID            string                    `json:"request_id"`
		Phase                string                    `json:"phase"`
		Operation            string                    `json:"operation"`
		DestinationDirectory string                    `json:"destination_directory"`
		Expected             []cloudStringExpectedFile `json:"expected"`
		Offset               int                       `json:"offset"`
		ExpectedBoundary     string                    `json:"expected_boundary"`
	}
	// A target page can carry 100 path identities, unlike the single-item mutation.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		http.Error(w, "预览请求格式无效。", http.StatusBadRequest)
		return
	}
	request := downloader.CloudBatchPreviewRequest{RequestID: wire.RequestID, Phase: wire.Phase, Operation: wire.Operation, DestinationDirectory: wire.DestinationDirectory, Offset: wire.Offset, ExpectedBoundary: wire.ExpectedBoundary, Expected: []baiduapi.FileInfo{}}
	for _, item := range wire.Expected {
		value, err := parseCloudStringExpected(&item)
		if err != nil {
			http.Error(w, "预览身份数字格式无效。", http.StatusBadRequest)
			return
		}
		request.Expected = append(request.Expected, value)
	}
	if err := downloader.ValidateCloudBatchPreview(request); err != nil {
		readFailure(w, err)
		return
	}
	backend, ok := s.backend.(interface {
		PreviewCloudBatch(context.Context, downloader.CloudBatchPreviewRequest) (downloader.CloudBatchPreviewResult, error)
	})
	if !ok {
		http.Error(w, "当前服务不支持在线预览。", http.StatusServiceUnavailable)
		return
	}
	result, err := backend.PreviewCloudBatch(r.Context(), request)
	if err != nil {
		readFailure(w, err)
		return
	}
	jsonResponse(w, result)
}
