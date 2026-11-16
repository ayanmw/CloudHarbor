package library

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
)

func (m *Manager) handleOperations(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var req WriteRequest
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
		d.DisallowUnknownFields()
		if d.Decode(&req) != nil || d.Decode(&struct{}{}) != io.EOF {
			http.Error(w, "文件操作格式无效", 400)
			return
		}
		op, e := m.Write(r.Context(), req, nil)
		writeOperationReply(w, req.ID, op, e)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", 405)
		return
	}
	q := r.URL.Query()
	if singleQuery(q, "id", "offset", "limit") != nil {
		http.Error(w, "操作查询无效", 400)
		return
	}
	if q.Has("id") {
		if len(q) != 1 || !operationIDPattern.MatchString(q.Get("id")) {
			http.Error(w, "操作ID无效", 400)
			return
		}
		op, e := m.Operation(q.Get("id"))
		if e != nil {
			http.Error(w, e.Error(), status(e))
			return
		}
		reply(w, 200, op)
		return
	}
	offset, limit := 0, 50
	var e error
	if q.Has("offset") {
		offset, e = strconv.Atoi(q.Get("offset"))
		if e != nil {
			http.Error(w, "offset无效", 400)
			return
		}
	}
	if q.Has("limit") {
		limit, e = strconv.Atoi(q.Get("limit"))
		if e != nil {
			http.Error(w, "limit无效", 400)
			return
		}
	}
	items, e := m.Operations(offset, limit)
	if e != nil {
		http.Error(w, e.Error(), status(e))
		return
	}
	reply(w, 200, map[string]any{"items": items, "offset": offset, "limit": limit})
}
func (m *Manager) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", 405)
		return
	}
	q := r.URL.Query()
	if singleQuery(q, "id", "root_revision", "target", "parent_identity", "size") != nil {
		http.Error(w, "上传参数无效", 400)
		return
	}
	size, e := strconv.ParseInt(q.Get("size"), 10, 64)
	if e != nil || size < 0 {
		http.Error(w, "上传大小无效", 400)
		return
	}
	if size > MaxUploadBytes || r.ContentLength > MaxUploadBytes {
		http.Error(w, "单文件最大50GiB", 413)
		return
	}
	req := WriteRequest{ID: q.Get("id"), Kind: "upload", RootRevision: q.Get("root_revision"), Target: q.Get("target"), ParentID: q.Get("parent_identity"), Size: size}
	op, e := m.Write(r.Context(), req, http.MaxBytesReader(w, r.Body, size+1))
	writeOperationReply(w, req.ID, op, e)
}
func writeOperationReply(w http.ResponseWriter, id string, op Operation, e error) {
	if e != nil {
		if op.ID == "" {
			reply(w, status(e), map[string]any{"id": id, "admitted": false, "error": writeError(e)})
		} else {
			reply(w, status(e), op)
		}
		return
	}
	reply(w, 201, op)
}

func writeError(e error) string {
	switch {
	case errors.Is(e, os.ErrExist):
		return "目标已存在，同名项目不会覆盖。请选择其他名称或目标目录。"
	case errors.Is(e, os.ErrPermission):
		return "没有文件系统写入权限，请核对目标目录权限。"
	case errors.Is(e, os.ErrNotExist):
		return "文件或目录已不存在，请刷新列表后重新核对。"
	default:
		return e.Error()
	}
}
