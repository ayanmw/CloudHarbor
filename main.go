package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/downloader"
	"web_browser_baidu_onekey_upload/internal/oauth"
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

	log.Printf("配置加载完成: QueueDir=%s, LocalDownDir=%s, ScanInterval=%ds",
		cfg.QueueDir, cfg.LocalDownDir, cfg.ScanInterval)

	// 创建百度网盘客户端
	client := baiduapi.NewClient(cfg.AccessToken, cfg.BaiduAppKey)

	// 创建下载器
	dl = downloader.NewDownloader(cfg, client)

	// 创建 WebUI 服务器
	ui = webui.NewServer(dl)

	// HTTP 路由 - 将 WebSocket 版本设为默认页面
	http.HandleFunc("/", ui.HandlePageWS)
	http.HandleFunc("/ws", ui.HandleWS)
	http.HandleFunc("/poll", ui.HandlePagePoll)
	http.HandleFunc("/oauth", ui.HandlePageOAuth) // 添加 OAuth 页面路由
	http.HandleFunc("/res/", ui.HandleRes)        // 使用单一处理器处理所有 /res/ 路径下的资源
	http.HandleFunc("/api/health", handleHealth)
	http.HandleFunc("/api/status", handleStatus)
	http.HandleFunc("/api/scan", handleScan)

	// OAuth 相关路由
	http.HandleFunc("/api/oauth/device-code", handleGetDeviceCode)
	http.HandleFunc("/api/oauth/token", handleGetToken)
	http.HandleFunc("/api/oauth/refresh", handleRefreshToken)

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

// handleGetDeviceCode 获取设备码
func handleGetDeviceCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if cfg.BaiduAppKey == "" {
		http.Error(w, "BAIDU_APP_KEY 未配置", http.StatusBadRequest)
		return
	}

	deviceResp, err := oauth.GetDeviceCode(cfg.BaiduAppKey)
	if err != nil {
		http.Error(w, "获取设备码失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(deviceResp)
}

// handleGetToken 轮询获取 Token
func handleGetToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if cfg.BaiduAppKey == "" || cfg.BaiduSecretKey == "" {
		http.Error(w, "BAIDU_APP_KEY 或 BAIDU_SECRET_KEY 未配置", http.StatusBadRequest)
		return
	}

	// 解析请求参数
	var req struct {
		DeviceCode string `json:"device_code"`
		Interval   int    `json:"interval"`
		ExpiresIn  int    `json:"expires_in"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "解析请求参数失败: "+err.Error(), http.StatusBadRequest)
		return
	}

	tokenResp, err := oauth.PollForToken(cfg.BaiduAppKey, cfg.BaiduSecretKey, req.DeviceCode, req.Interval, req.ExpiresIn)
	if err != nil {
		http.Error(w, "获取 Token 失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tokenResp)
}

// handleRefreshToken 刷新 Token
func handleRefreshToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if cfg.BaiduAppKey == "" || cfg.BaiduSecretKey == "" || cfg.RefreshToken == "" {
		http.Error(w, "缺少必要的配置参数", http.StatusBadRequest)
		return
	}

	tokenResp, err := oauth.RefreshToken(cfg.BaiduAppKey, cfg.BaiduSecretKey, cfg.RefreshToken)
	if err != nil {
		http.Error(w, "刷新 Token 失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 更新配置中的 Token
	cfg.AccessToken = tokenResp.AccessToken
	cfg.RefreshToken = tokenResp.RefreshToken

	// 更新百度网盘客户端
	dl.UpdateClient(baiduapi.NewClient(cfg.AccessToken, cfg.BaiduAppKey))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tokenResp)
}
