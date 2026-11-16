package intakeapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func (s *Server) handleEmptyDirectoryPage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	for key, values := range query {
		if (key != "identity_format" && key != "offset" && key != "limit" && key != "cache_generation") || len(values) != 1 {
			http.Error(w, "空目录分页参数无效", http.StatusBadRequest)
			return
		}
	}
	if query.Get("identity_format") != "string" {
		http.Error(w, "空目录分页需要精确身份格式", http.StatusBadRequest)
		return
	}
	offset, limit, err := pagination(r, 50)
	if err != nil || limit > 100 || offset > 1000000 {
		http.Error(w, "空目录分页无效", http.StatusBadRequest)
		return
	}
	expected, err := optionalGeneration(r)
	if err != nil || (offset > 0 && expected == nil) {
		http.Error(w, "缓存代次参数无效", http.StatusBadRequest)
		return
	}
	backend, ok := s.backend.(interface {
		PreviewEmptyCloudDirectoriesPage(context.Context, int64, int, *int64) (taskstore.CloudEmptyDirectoryPage, error)
	})
	if !ok {
		http.Error(w, "当前后端不支持空目录分页", http.StatusNotImplemented)
		return
	}
	page, err := backend.PreviewEmptyCloudDirectoriesPage(r.Context(), int64(offset), limit, expected)
	if errors.Is(err, taskstore.ErrCloudInventoryGenerationChanged) {
		jsonResponseStatus(w, http.StatusConflict, map[string]string{"code": "cache_generation_changed", "cache_generation": strconv.FormatInt(page.Generation, 10)})
		return
	}
	if err != nil {
		readFailure(w, err)
		return
	}
	var next *string
	if int64(offset)+int64(len(page.Entries)) < page.Total {
		value := strconv.FormatInt(int64(offset)+int64(len(page.Entries)), 10)
		next = &value
	}
	jsonResponse(w, map[string]any{"directories": cachedCloudEntriesResponse(page.Entries, true), "cache_generation": strconv.FormatInt(page.Generation, 10), "coverage": page.Coverage, "scan_run_id": page.RunID, "observed_at_utc": page.UpdatedAtUTC, "total": strconv.FormatInt(page.Total, 10), "offset": strconv.Itoa(offset), "limit": limit, "next_offset": next})
}
