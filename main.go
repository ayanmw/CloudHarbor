package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/downloader"
	"web_browser_baidu_onekey_upload/internal/webui"
)

var (
	cfg          *config.Config
	dl           *downloader.Downloader
	ui           *webui.Server
	lastScanTime time.Time
)

func main() {
	log.Println("baidu-nas-downloader service starting...")

	// 加载配置
	cfg = config.Load()

	// 检查必要配置
	if cfg.AccessToken == "" {
		log.Fatal("BAIDU_ACCESS_TOKEN 环境变量未设置，请先配置 Access Token")
	}

	log.Printf("配置加载完成: QueueDir=%s, LocalDownDir=%s, ScanInterval=%ds",
		cfg.QueueDir, cfg.LocalDownDir, cfg.ScanInterval)

	// 创建百度网盘客户端
	client := baiduapi.NewClient(cfg.AccessToken, cfg.BaiduAppKey)

	// 创建下载器
	dl = downloader.NewDownloader(cfg, client)

	// 创建 WebUI 服务器
	ui = webui.NewServer(dl)

	// HTTP 路由
	http.HandleFunc("/", ui.HandlePageWS)
	http.HandleFunc("/poll", ui.HandlePagePoll)
	http.HandleFunc("/ws", ui.HandleWS)
	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/status", handleStatus)
	http.HandleFunc("/scan", handleScan)

	// 启动后台扫描任务
	go func() {
		for {
			log.Printf("[Worker] 开始扫描队列目录...")
			lastScanTime = time.Now()
			if err := dl.ScanAndDownload(); err != nil {
				log.Printf("[Worker] 扫描失败: %v", err)
			}
			log.Printf("[Worker] 扫描完成，下次扫描时间: %s", time.Now().Add(time.Duration(cfg.ScanInterval)*time.Second).Format(time.RFC3339))
			time.Sleep(time.Duration(cfg.ScanInterval) * time.Second)
		}
	}()

	log.Printf("HTTP server listening on :%s\n", cfg.ServerPort)
	if err := http.ListenAndServe(":"+cfg.ServerPort, nil); err != nil {
		log.Fatal(err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	tasks := dl.GetTasks()
	downloadingCount := 0
	doneCount := 0
	failedCount := 0

	for _, t := range tasks {
		switch t.Status {
		case "downloading":
			downloadingCount++
		case "done":
			doneCount++
		case "failed":
			failedCount++
		}
	}

	response := map[string]interface{}{
		"status":         "ok",
		"last_scan_time": lastScanTime.Format(time.RFC3339),
		"next_scan_time": lastScanTime.Add(time.Duration(cfg.ScanInterval) * time.Second).Format(time.RFC3339),
		"total_tasks":    len(tasks),
		"downloading":    downloadingCount,
		"done":           doneCount,
		"failed":         failedCount,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	tasks := dl.GetTasks()

	// 转换为适合输出的格式
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
		}

		if !t.StartTime.IsZero() {
			ts.StartTime = t.StartTime.Format(time.RFC3339)
		}
		if !t.FinishTime.IsZero() {
			ts.FinishTime = t.FinishTime.Format(time.RFC3339)
		}

		statuses = append(statuses, ts)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"tasks": statuses,
	})
}

func handleScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	go func() {
		log.Printf("[API] 手动触发扫描")
		lastScanTime = time.Now()
		if err := dl.ScanAndDownload(); err != nil {
			log.Printf("[API] 扫描失败: %v", err)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "scan triggered",
	})
}
