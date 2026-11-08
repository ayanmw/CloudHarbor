package foldersync

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}
func (m *Manager) Handle(w http.ResponseWriter, r *http.Request) {
	config, run := r.PathValue("config"), r.PathValue("run")
	if run != "" {
		if r.Method != http.MethodGet {
			w.WriteHeader(405)
			return
		}
		var body string
		if err := m.db.QueryRow(`SELECT body FROM sync_runs WHERE id=?`, run).Scan(&body); err != nil {
			http.NotFound(w, r)
			return
		}
		var result Run
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			http.Error(w, "同步记录损坏", 500)
			return
		}
		steps, err := m.Steps(run)
		if err != nil {
			http.Error(w, "步骤记录读取失败", 500)
			return
		}
		var events []Event
		truncated := false
		if r.URL.Query().Get("export") == "1" {
			events, err = m.Events(run)
		} else {
			events, truncated, err = m.displayEvents(run)
		}
		if err != nil {
			http.Error(w, "同步证据读取失败", 500)
			return
		}
		if r.URL.Query().Get("export") == "1" {
			w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="sync-%s.json"`, run))
		}
		offset := 0
		if text := r.URL.Query().Get("change_offset"); text != "" {
			offset, err = strconv.Atoi(text)
			if err != nil || offset < 0 {
				http.Error(w, "变化分页无效", 400)
				return
			}
		}
		changes, total, err := m.Changes(run, offset)
		if err != nil {
			http.Error(w, "变化记录读取失败", 500)
			return
		}
		reply(w, map[string]any{"run": result, "steps": steps, "events": events, "events_truncated": truncated, "changes": changes, "changes_total": total, "change_offset": offset})
		return
	}
	if config != "" {
		switch r.Method {
		case http.MethodGet:
			runs, more, next, err := m.runPage(config, r.URL.Query().Get("before"))
			if err != nil {
				http.Error(w, "同步运行记录读取失败", 500)
				return
			}
			reply(w, map[string]any{"runs": runs, "has_more": more, "next_before": next})
		case http.MethodPost:
			run, err := m.Start(config)
			if err != nil {
				http.Error(w, err.Error(), 409)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusAccepted)
			reply(w, run)
		default:
			w.WriteHeader(405)
		}
		return
	}
	switch r.Method {
	case http.MethodGet:
		configs, err := m.Configs()
		if err != nil {
			http.Error(w, "同步配置读取失败", 500)
			return
		}
		m.mu.Lock()
		active := m.active
		m.mu.Unlock()
		reply(w, map[string]any{"configs": configs, "active": active})
	case http.MethodPost:
		var input struct {
			Name       string `json:"name"`
			Mode       string `json:"mode"`
			LocalRoot  string `json:"local_root"`
			RemoteRoot string `json:"remote_root"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
			http.Error(w, "同步配置请求无效", 400)
			return
		}
		config, err := m.Create(r.Context(), Config{Name: input.Name, Mode: input.Mode, LocalRoot: input.LocalRoot, RemoteRoot: input.RemoteRoot})
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		reply(w, config)
	default:
		w.WriteHeader(405)
	}
}

func (m *Manager) HandleLocalDirectories(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	p := r.URL.Query().Get("path")
	if !filepath.IsAbs(p) {
		http.Error(w, "请输入服务所在主机的绝对目录", 400)
		return
	}
	p = filepath.Clean(p)
	if _, err := rootIdentity(p); err != nil {
		http.Error(w, "目录不可访问或包含链接", 400)
		return
	}
	rows, err := os.ReadDir(p)
	if err != nil {
		http.Error(w, "本地目录读取失败", 400)
		return
	}
	result := []map[string]string{}
	for _, row := range rows {
		if !row.IsDir() || row.Name() == privateDirectory {
			continue
		}
		info, e := row.Info()
		if e != nil || info.Mode()&os.ModeSymlink != 0 || isReparse(info) {
			continue
		}
		target := filepath.Join(p, row.Name())
		forbidden := false
		for _, excluded := range m.exclusions() {
			if overlap(target, excluded) {
				forbidden = true
			}
		}
		if !forbidden {
			result = append(result, map[string]string{"name": row.Name(), "path": target})
		}
	}
	reply(w, map[string]any{"path": p, "parent": filepath.Dir(p), "directories": result})
}
