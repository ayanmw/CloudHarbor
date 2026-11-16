package library

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
)

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func status(err error) int {
	switch {
	case errors.Is(err, ErrUnsafe):
		return http.StatusBadRequest
	case errors.Is(err, ErrConflict), errors.Is(err, ErrBusy), errors.Is(err, ErrUnconfigured):
		return http.StatusConflict
	case errors.Is(err, ErrChanged):
		return http.StatusPreconditionFailed
	case errors.Is(err, os.ErrNotExist):
		return http.StatusNotFound
	default:
		return http.StatusServiceUnavailable
	}
}
func singleQuery(q url.Values, keys ...string) error {
	allowed := map[string]bool{}
	for _, k := range keys {
		allowed[k] = true
	}
	for k, v := range q {
		if !allowed[k] || len(v) != 1 {
			return ErrUnsafe
		}
	}
	return nil
}
func (m *Manager) Handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	switch r.URL.Path {
	case "/api/library/config":
		switch r.Method {
		case http.MethodGet:
			c, err := m.Config()
			if err != nil {
				http.Error(w, "文件库配置暂不可用", 503)
				return
			}
			reply(w, 200, c)
		case http.MethodPost:
			var request struct {
				Root             string `json:"root"`
				ExpectedRevision string `json:"expected_revision"`
			}
			d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
			d.DisallowUnknownFields()
			if d.Decode(&request) != nil || d.Decode(&struct{}{}) != io.EOF {
				http.Error(w, "文件库配置格式无效", 400)
				return
			}
			c, err := m.Configure(request.Root, request.ExpectedRevision)
			if err != nil {
				http.Error(w, err.Error(), status(err))
				return
			}
			reply(w, 200, c)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "Method not allowed", 405)
		}
	case "/api/library/entries":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method not allowed", 405)
			return
		}
		q := r.URL.Query()
		if singleQuery(q, "path", "query", "offset", "limit") != nil {
			http.Error(w, "列表查询无效", 400)
			return
		}
		offset, limit := 0, 50
		var err error
		if q.Has("offset") {
			offset, err = strconv.Atoi(q.Get("offset"))
			if err != nil {
				http.Error(w, "offset无效", 400)
				return
			}
		}
		if q.Has("limit") {
			limit, err = strconv.Atoi(q.Get("limit"))
			if err != nil {
				http.Error(w, "limit无效", 400)
				return
			}
		}
		l, err := m.List(r.Context(), q.Get("path"), q.Get("query"), offset, limit)
		if err != nil {
			if l.Error == "" {
				l.Error = err.Error()
				l.Stale = true
			}
			reply(w, status(err), l)
			return
		}
		reply(w, 200, l)
	case "/api/library/content":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", 405)
			return
		}
		q := r.URL.Query()
		if singleQuery(q, "path", "revision") != nil || q.Get("path") == "" || q.Get("revision") == "" {
			http.Error(w, "下载路径或观察版本无效", 400)
			return
		}
		m.serveContent(w, r, q.Get("path"), q.Get("revision"))
	default:
		http.NotFound(w, r)
	}
}

type contextFile struct {
	context context.Context
	file    *os.File
}

func (f contextFile) Read(b []byte) (int, error) {
	if e := f.context.Err(); e != nil {
		return 0, e
	}
	return f.file.Read(b)
}
func (f contextFile) Seek(o int64, w int) (int64, error) {
	if e := f.context.Err(); e != nil {
		return 0, e
	}
	return f.file.Seek(o, w)
}

type transferWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *transferWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}
func (w *transferWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	n, e := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, e
}
func (m *Manager) serveContent(w http.ResponseWriter, r *http.Request, p, revision string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		http.Error(w, "文件库已关闭", 503)
		return
	}
	if err := validRelative(p); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	c, err := m.config()
	if err != nil {
		http.Error(w, "文件库配置暂不可用", 503)
		return
	}
	root, err := m.openRoot(c)
	if err != nil {
		http.Error(w, err.Error(), status(err))
		return
	}
	defer root.Close()
	f, info, err := openChecked(root, p)
	if err != nil {
		http.Error(w, "无法读取该文件，请刷新目录", status(err))
		return
	}
	defer f.Close()
	if info.IsDir() {
		http.Error(w, "目录不支持打包下载", 400)
		return
	}
	id, err := identity(f)
	if err != nil {
		http.Error(w, "无法核对文件身份", 503)
		return
	}
	if observationRevision(c, p, id, info) != revision {
		http.Error(w, "文件已变化，请刷新目录后重新下载", 412)
		return
	}
	var random [12]byte
	if _, err = rand.Read(random[:]); err != nil {
		http.Error(w, "无法建立下载记录", 503)
		return
	}
	requestID := "library-" + hex.EncodeToString(random[:])
	w.Header().Set("X-Request-ID", requestID)
	// Cancellation during hashing used to leave only the preparation log. Record
	// a terminal response for every admitted request, including preparation errors.
	tw := &transferWriter{ResponseWriter: w}
	w = tw
	defer func() {
		log.Printf("[Library] at_utc=%s request_id=%q path=%q event=download_response status=%d bytes=%d context_error=%v", utcNow(), requestID, p, tw.status, tw.bytes, r.Context().Err())
	}()
	log.Printf("[Library] at_utc=%s request_id=%q path=%q event=download_prepare", utcNow(), requestID, p)
	body := contextFile{context: r.Context(), file: f}
	digest := sha256.New()
	if _, err = io.Copy(digest, body); err != nil {
		http.Error(w, "文件内容校验失败，请稍后重试", 503)
		return
	}
	after, err := f.Stat()
	if err != nil || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		http.Error(w, ErrChanged.Error(), 412)
		return
	}
	if err = components(root, p); err != nil {
		http.Error(w, ErrChanged.Error(), 412)
		return
	}
	current, err := root.Stat(p)
	if err != nil || !os.SameFile(after, current) {
		http.Error(w, ErrChanged.Error(), 412)
		return
	}
	check, err := m.openRoot(c)
	if err != nil {
		http.Error(w, ErrChanged.Error(), 412)
		return
	}
	check.Close()
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		http.Error(w, "无法读取文件内容", 503)
		return
	}
	w.Header().Set("ETag", `"sha256-`+hex.EncodeToString(digest.Sum(nil))+`"`)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": path.Base(p)}))
	http.ServeContent(w, r, path.Base(p), info.ModTime(), body)
}
