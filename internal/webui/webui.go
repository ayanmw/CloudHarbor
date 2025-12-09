package webui

import (
	_ "embed"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"web_browser_baidu_onekey_upload/internal/downloader"
)

//go:embed index.html
var indexPage []byte

//go:embed index_ws.html
var indexWS []byte

//go:embed index_poll.html
var indexPoll []byte

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
