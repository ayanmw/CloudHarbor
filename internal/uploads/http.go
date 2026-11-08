package uploads

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

func write(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(value)
}
func (m *Manager) Handle(w http.ResponseWriter, r *http.Request) {
	batch, item := r.PathValue("batch"), r.PathValue("item")
	if item != "" {
		if r.Method != http.MethodPut {
			w.WriteHeader(405)
			return
		}
		if err := m.Receive(r.Context(), batch, item, r.Body); err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		write(w, map[string]bool{"received": true})
		return
	}
	if batch != "" {
		switch r.Method {
		case http.MethodGet:
			b, err := m.Get(batch)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					http.NotFound(w, r)
				} else {
					http.Error(w, "上传批次不可读", 500)
				}
				return
			}
			write(w, b)
		case http.MethodPost:
			if err := m.Retry(batch); err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
			write(w, map[string]bool{"retry_requested": true})
		case http.MethodDelete:
			if err := m.Cancel(batch); err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
			write(w, map[string]bool{"cancelled": true})
		default:
			w.WriteHeader(405)
		}
		return
	}
	switch r.Method {
	case http.MethodGet:
		rows, err := m.List()
		if err != nil {
			http.Error(w, "上传批次不可读", 500)
			return
		}
		write(w, map[string]any{"batches": rows, "limit": 50})
	case http.MethodPost:
		var input struct {
			Target      string   `json:"target"`
			Directories []string `json:"directories"`
			Files       []Input  `json:"files"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4*1024*1024))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
			http.Error(w, "上传清单无效", 400)
			return
		}
		if len(input.Directories) > 0 {
			m.mu.Lock()
			available := m.runDirectory != nil
			m.mu.Unlock()
			if !available {
				http.Error(w, "当前服务未配置目录上传执行器", 409)
				return
			}
		}
		b, err := m.createManifest(input.Target, input.Files, input.Directories)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		write(w, b)
	default:
		w.WriteHeader(405)
	}
}
