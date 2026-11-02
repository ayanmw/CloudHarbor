package webui

import (
	"context"
	"embed"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"web_browser_baidu_onekey_upload/internal/downloader"
	"web_browser_baidu_onekey_upload/internal/management"
)

//go:embed index_ws.html
var indexWS []byte

//go:embed index_poll.html
var indexPoll []byte

//go:embed index_oauth.html
var indexOAuth []byte

//go:embed index_intake.html
var indexIntake []byte

//go:embed index_login.html
var indexLogin []byte

//go:embed index_cloud.html
var indexCloud []byte

//go:embed index_operations.html
var indexOperations []byte

//go:embed index_monitor_history.html
var indexMonitorHistory []byte

//go:embed index_task_events.html
var indexTaskEvents []byte

//go:embed res/*
var resFiles embed.FS

type Server struct {
	ctx      context.Context
	dl       *downloader.Downloader
	upgrader websocket.Upgrader
}

type wsMessage struct {
	Type         string `json:"type"`
	DownloadMode string `json:"download_mode,omitempty"`
	Offset       int    `json:"offset,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	SortBy       string `json:"sort_by,omitempty"`
	Status       string `json:"status,omitempty"`
	Query        string `json:"q,omitempty"`
	RequestID    int64  `json:"request_id,omitempty"`
}

func NewServer(dl *downloader.Downloader) *Server {
	return &Server{
		ctx: context.Background(),
		dl:  dl,
		upgrader: websocket.Upgrader{
			CheckOrigin: management.CheckOrigin,
		},
	}
}

func (s *Server) WithContext(ctx context.Context) *Server { s.ctx = ctx; return s }

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

// HandlePageOAuth 返回 OAuth 授权页面
func (s *Server) HandlePageOAuth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	writeWorkspacePage(w, indexOAuth)
}

func (s *Server) HandlePageIntake(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	writeWorkspacePage(w, indexIntake)
}

func (s *Server) HandlePageLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	writeWorkspacePage(w, indexLogin)
}

func (s *Server) HandlePageCloud(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	writeWorkspacePage(w, indexCloud)
}

func (s *Server) HandlePageOperations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	writeWorkspacePage(w, indexOperations)
}

func (s *Server) HandlePageMonitorHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	writeWorkspacePage(w, indexMonitorHistory)
}

func (s *Server) HandlePageTaskEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	writeWorkspacePage(w, indexTaskEvents)
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

// HandleWS WebSocket 实时状态通道
func (s *Server) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WebUI] WebSocket 升级失败: %v", err)
		return
	}
	defer conn.Close()

	done := make(chan struct{})
	pageRequests := make(chan wsMessage, 1)
	offset, limit, sortBy, status, query, requestID := 0, 100, "name", "", "", int64(0)

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
			} else if msg.Type == "status_page" && msg.Offset >= 0 && msg.Limit >= 1 && msg.Limit <= 200 && (msg.SortBy == "name" || msg.SortBy == "progress" || msg.SortBy == "size") {
				msg.Status, msg.Query, err = downloader.NormalizeTaskFilters(msg.Status, msg.Query)
				if err != nil {
					continue
				}
				select {
				case pageRequests <- msg:
				default:
					select {
					case <-pageRequests:
					default:
					}
					pageRequests <- msg
				}
			}
		}
	}()

	// 定时推送状态
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-done:
			return
		case request := <-pageRequests:
			offset, limit, sortBy, status, query, requestID = request.Offset, request.Limit, request.SortBy, request.Status, request.Query, request.RequestID
			if err := s.sendStatus(conn, offset, limit, sortBy, status, query, requestID); err != nil {
				log.Printf("[WebUI] 发送状态失败: %v", err)
				return
			}
		case <-ticker.C:
			if err := s.sendStatus(conn, offset, limit, sortBy, status, query, requestID); err != nil {
				log.Printf("[WebUI] 发送状态失败: %v", err)
				return
			}
		}
	}
}

func (s *Server) sendStatus(conn *websocket.Conn, offset, limit int, sortBy, status, query string, requestID int64) error {
	page, err := s.dl.GetTaskPageFiltered(offset, limit, sortBy, status, query)
	if err != nil {
		return err
	}
	tasks := page.Tasks

	// 与 /status 类似的结构
	type TaskStatus struct {
		FsID           int64                            `json:"fs_id"`
		TaskID         string                           `json:"task_id"`
		CanCancel      bool                             `json:"can_cancel"`
		CanPause       bool                             `json:"can_pause"`
		CanResume      bool                             `json:"can_resume"`
		CanRetry       bool                             `json:"can_retry"`
		Path           string                           `json:"path"`
		ServerFilename string                           `json:"server_filename"`
		Size           int64                            `json:"size"`
		Status         string                           `json:"status"`
		LocalPath      string                           `json:"local_path,omitempty"`
		Error          string                           `json:"error,omitempty"`
		StartTime      string                           `json:"start_time,omitempty"`
		FinishTime     string                           `json:"finish_time,omitempty"`
		Progress       int64                            `json:"progress"`
		Speed          int64                            `json:"speed,omitempty"` // 添加速度信息
		Attempts       int                              `json:"attempts"`
		NextAttempt    string                           `json:"next_attempt,omitempty"`
		LocalStorage   *downloader.LocalStorageEvidence `json:"local_storage,omitempty"`
		Root           bool                             `json:"is_root"`
		DownloadMode   string                           `json:"download_mode,omitempty"`
	}

	statuses := make([]TaskStatus, 0, len(tasks))
	for _, t := range tasks {
		ts := TaskStatus{
			FsID:   t.FileInfo.FsID,
			TaskID: t.TaskID, CanCancel: t.CanCancel, CanPause: t.CanPause, CanResume: t.CanResume, CanRetry: t.CanRetry,
			Path:           t.FileInfo.Path,
			ServerFilename: t.FileInfo.ServerFilename,
			Size:           t.FileInfo.Size,
			Status:         t.Status,
			LocalPath:      t.LocalPath,
			Error:          t.Error,
			Progress:       t.Progress,
			Speed:          t.Speed, // 添加速度信息
			Attempts:       t.Attempts,
			LocalStorage:   t.LocalStorage,
			Root:           t.Root, DownloadMode: string(t.DownloadMode),
		}

		if !t.StartTime.IsZero() {
			ts.StartTime = t.StartTime.Format(time.RFC3339)
		}
		if !t.NextAttempt.IsZero() {
			ts.NextAttempt = t.NextAttempt.Format(time.RFC3339)
		}
		if !t.FinishTime.IsZero() {
			ts.FinishTime = t.FinishTime.Format(time.RFC3339)
		}

		statuses = append(statuses, ts)
	}

	payload := map[string]interface{}{
		"type": "status", "tasks": statuses, "request_id": requestID,
		"total_tasks": page.Total, "queue_total_tasks": page.QueueTotal, "status_counts": page.Counts,
		"queue_pause": s.dl.QueuePauseStatus(), "queue_healthy": s.dl.QueueOperational(),
		"offset": page.Offset, "limit": page.Limit, "has_more": page.HasMore,
		"next_offset": func() *int {
			if !page.HasMore {
				return nil
			}
			next := page.Offset + page.Limit
			return &next
		}(),
		"settings": map[string]interface{}{
			"download_mode": string(s.dl.GetDownloadMode()),
		},
	}

	return conn.WriteJSON(payload)
}
