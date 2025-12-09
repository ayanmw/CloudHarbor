package webui

import (
	"embed"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"web_browser_baidu_onekey_upload/internal/downloader"
)

//go:embed index_ws.html
var indexWS []byte

//go:embed index_poll.html
var indexPoll []byte

//go:embed res/*
var resFiles embed.FS

type Server struct {
	dl       *downloader.Downloader
	upgrader websocket.Upgrader
}

func NewServer(dl *downloader.Downloader) *Server {
	return &Server{
		dl: dl,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// HandlePageWS 返回 WebSocket 版本页面（默认页面）
func (s *Server) HandlePageWS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexWS)
}

// HandlePagePoll 返回轮询版本页面
func (s *Server) HandlePagePoll(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(indexPoll)
}

// HandleRes 静态资源文件服务
func (s *Server) HandleRes(w http.ResponseWriter, r *http.Request) {
	// 移除前缀 /res/ 获取实际文件路径
	path := strings.TrimPrefix(r.URL.Path, "/res/")

	// 安全检查：防止路径遍历攻击
	if strings.Contains(path, "..") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	// 从嵌入的文件系统中读取文件
	content, err := resFiles.ReadFile("res/" + path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// 根据文件扩展名设置正确的 Content-Type
	switch filepath.Ext(path) {
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		w.Header().Set("Content-Type", "image/jpeg")
	case ".gif":
		w.Header().Set("Content-Type", "image/gif")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case ".ico":
		w.Header().Set("Content-Type", "image/x-icon")
	case ".woff":
		w.Header().Set("Content-Type", "font/woff")
	case ".woff2":
		w.Header().Set("Content-Type", "font/woff2")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	w.Write(content)
}

type wsMessage struct {
	Type         string `json:"type"`
	DownloadMode string `json:"download_mode,omitempty"`
}

// HandleWS WebSocket 实时状态通道
func (s *Server) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WebUI] WebSocket 升级失败: %v", err)
		return
	}
	defer conn.Close()

	done := make(chan struct{})

	// 读取前端消息（设置更新）
	go func() {
		defer close(done)
		for {
			var msg wsMessage
			if err := conn.ReadJSON(&msg); err != nil {
				log.Printf("[WebUI] 读取 WebSocket 消息失败: %v", err)
				return
			}

			if msg.Type == "update_settings" && msg.DownloadMode != "" {
				log.Printf("[WebUI] 更新下载模式为: %s", msg.DownloadMode)
				s.dl.SetDownloadMode(downloader.DownloadMode(msg.DownloadMode))
			}
		}
	}()

	// 定时推送状态
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if err := s.sendStatus(conn); err != nil {
				log.Printf("[WebUI] 发送状态失败: %v", err)
				return
			}
		}
	}
}

func (s *Server) sendStatus(conn *websocket.Conn) error {
	tasks := s.dl.GetTasks()

	// 与 /status 类似的结构
	type TaskStatus struct {
		FsID           int64  `json:"fs_id"`
		Path           string `json:"path"`
		ServerFilename string `json:"server_filename"`
		Size           int64  `json:"size"`
		Status         string `json:"status"`
		LocalPath      string `json:"local_path,omitempty"`
		Error          string `json:"error,omitempty"`
		StartTime      string `json:"start_time,omitempty"`
		FinishTime     string `json:"finish_time,omitempty"`
		Progress       int64  `json:"progress"`
		Speed          int64  `json:"speed,omitempty"` // 添加速度信息
	}

	statuses := make([]TaskStatus, 0, len(tasks))
	for _, t := range tasks {
		ts := TaskStatus{
			FsID:           t.FileInfo.FsID,
			Path:           t.FileInfo.Path,
			ServerFilename: t.FileInfo.ServerFilename,
			Size:           t.FileInfo.Size,
			Status:         t.Status,
			LocalPath:      t.LocalPath,
			Error:          t.Error,
			Progress:       t.Progress,
			Speed:          t.Speed, // 添加速度信息
		}

		if !t.StartTime.IsZero() {
			ts.StartTime = t.StartTime.Format(time.RFC3339)
		}
		if !t.FinishTime.IsZero() {
			ts.FinishTime = t.FinishTime.Format(time.RFC3339)
		}

		statuses = append(statuses, ts)
	}

	payload := map[string]interface{}{
		"type":  "status",
		"tasks": statuses,
		"settings": map[string]interface{}{
			"download_mode": string(s.dl.GetDownloadMode()),
		},
	}

	return conn.WriteJSON(payload)
}
