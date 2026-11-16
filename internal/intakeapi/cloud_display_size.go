package intakeapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/downloader"
)

type displaySizeListingBackend interface {
	ListCloudDisplaySize(context.Context, string, int, int, bool, string) (downloader.CloudListing, error)
}

func displaySizeSnapshotConflict(w http.ResponseWriter) {
	jsonResponseStatus(w, http.StatusConflict, map[string]any{"code": "display_size_snapshot_changed", "message": "大小排序快照已失效，请重新读取第一页；不会重发网盘写入。"})
}
func (s *Server) HandleCloudDisplaySize(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !method(w, r, "GET") {
		return
	}
	q := r.URL.Query()
	allowed := map[string]bool{"dir": true, "offset": true, "limit": true, "order": true, "desc": true, "snapshot": true, "identity_format": true, "expected_boundary": true, "mixed": true}
	for name, values := range q {
		if !allowed[name] || len(values) != 1 {
			http.Error(w, "大小排序参数无效。", 400)
			return
		}
	}
	if (q.Has("order") && q.Get("order") != "size" && q.Get("order") != "name" && q.Get("order") != "time") || (q.Has("desc") && q.Get("desc") != "0" && q.Get("desc") != "1") || (q.Get("identity_format") != "" && q.Get("identity_format") != "string") {
		http.Error(w, "大小排序参数无效。", 400)
		return
	}
	offset, limit, err := pagination(r, 100)
	if err != nil {
		readFailure(w, err)
		return
	}
	token := q.Get("snapshot")
	if q.Has("snapshot") {
		raw, e := hex.DecodeString(token)
		if e != nil || len(raw) != 16 || hex.EncodeToString(raw) != token {
			http.Error(w, "排序快照标识无效。", 400)
			return
		}
	}
	mixed := true
	if q.Has("mixed") {
		if q.Get("mixed") != "0" && q.Get("mixed") != "1" {
			http.Error(w, "混合排序参数无效。", 400)
			return
		}
		mixed = q.Get("mixed") == "1"
	}
	order := baiduapi.DirectoryOrder{Field: q.Get("order"), Descending: q.Get("desc") == "1"}
	if order.Field == "" {
		order.Field = "size"
	}
	var expected *downloader.CloudPageBoundary
	if q.Has("expected_boundary") {
		raw := q.Get("expected_boundary")
		var boundary downloader.CloudPageBoundary
		if offset == 0 || len(raw) > 4096 || json.Unmarshal([]byte(raw), &boundary) != nil || len(boundary.Fingerprint) != 64 {
			http.Error(w, "分页边界无效。", 400)
			return
		}
		if _, e := hex.DecodeString(boundary.Fingerprint); e != nil {
			http.Error(w, "分页边界无效。", 400)
			return
		}
		expected = &boundary
	}
	if offset > 0 && token == "" {
		displaySizeSnapshotConflict(w)
		return
	}
	b, ok := s.backend.(displaySizeListingBackend)
	if !ok {
		http.Error(w, "当前服务不支持显示大小排序。", 503)
		return
	}
	dir := q.Get("dir")
	if dir == "" {
		dir = "/"
	}
	descending := q.Get("desc") == "1"
	var result downloader.CloudListing
	if ordered, ok := s.backend.(interface {
		ListCloudDisplayOrdered(context.Context, string, int, int, baiduapi.DirectoryOrder, bool, string) (downloader.CloudListing, error)
	}); ok {
		result, err = ordered.ListCloudDisplayOrdered(r.Context(), dir, offset, limit, order, mixed, token)
	} else if order.Field == "size" && mixed {
		result, err = b.ListCloudDisplaySize(r.Context(), dir, offset, limit, descending, token)
	} else {
		http.Error(w, "当前服务不支持混合排序设置。", 503)
		return
	}
	if errors.Is(err, downloader.ErrCloudDisplaySizeSnapshotChanged) {
		displaySizeSnapshotConflict(w)
		return
	}
	if err != nil {
		readFailure(w, err)
		return
	}
	if expected != nil {
		matches := false
		if len(result.Entries) > 0 {
			entry := result.Entries[0]
			boundary := downloader.CloudPageBoundary{FsID: entry.FsID, Path: entry.Path, Name: entry.Name, Directory: entry.Directory, Size: entry.ProviderSize, ProviderMtime: entry.Mtime, ContentMD5: entry.MD5}
			matches = strings.EqualFold(expected.Fingerprint, boundary.FingerprintForListing(dir, order))
		}
		if !matches {
			displaySizeSnapshotConflict(w)
			return
		}
	}
	jsonResponse(w, cloudListingResponse(result, q.Get("identity_format") == "string"))
}
