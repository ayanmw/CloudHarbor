package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"web_browser_baidu_onekey_upload/internal/authorization"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/config"
	"web_browser_baidu_onekey_upload/internal/credentials"
	"web_browser_baidu_onekey_upload/internal/downloader"
	"web_browser_baidu_onekey_upload/internal/intakeapi"
	"web_browser_baidu_onekey_upload/internal/management"
	"web_browser_baidu_onekey_upload/internal/webui"
)

var (
	cfg            *config.Config
	dl             *downloader.Downloader
	ui             *webui.Server
	lastScanUnix   atomic.Int64
	auth           *credentials.Manager
	lastScanResult atomic.Pointer[scanResult]
	scanRequests   = make(chan struct{}, 1)
)

var version = "dev"
var revision = "unknown"

type scanResult struct {
	Success time.Time
	Error   string
}

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("baidu-nas-downloader " + version + " (" + revision + ")")
		return
	}
	log.Println("baidu-nas-downloader service starting...")

	// 加载配置
	cfg = config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}

	log.Printf("配置加载完成: QueueDir=%s, LocalDownDir=%s, ScanInterval=%ds",
		cfg.QueueDir, cfg.LocalDownDir, cfg.ScanInterval)

	// 创建百度网盘客户端
	client := baiduapi.NewClient(cfg.AccessToken, cfg.BaiduAppKey)

	// 创建下载器
	var err error
	dl, err = downloader.OpenDownloader(cfg, client)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dl.SetAuthentication(client, false, time.Time{})
	auth, err = credentials.Open(dl, credentials.Options{AppKey: cfg.BaiduAppKey, SecretKey: cfg.BaiduSecretKey, AccessToken: cfg.AccessToken, RefreshToken: cfg.RefreshToken}, nil, func(update credentials.Update) {
		dl.SetAuthentication(baiduapi.NewClient(update.AccessToken, cfg.BaiduAppKey), update.Ready, update.ExpiresAt)
		if update.Ready {
			select {
			case scanRequests <- struct{}{}:
			default:
			}
		}
	})
	if err != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		dl.Shutdown(shutdownCtx)
		cancel()
		log.Fatal(err)
	}
	dl.SetAuthenticationFailureHandler(auth.NotifyRejected)
	adminKey, err := dl.AdministratorKey(cfg.AdminPassword)
	if err != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		dl.Shutdown(shutdownCtx)
		cancel()
		log.Fatal(err)
	}
	gate, err := management.New(adminKey)
	if err != nil {
		log.Fatal(err)
	}
	if cfg.AdminPassword == "" {
		log.Printf("Administrator username: admin; password stored in %s", filepath.Join(cfg.LocalDownDir, ".nas-downloader", "queue", "admin-key.json"))
	}
	authorizationHTTP := authorization.New(dl.Context(), cfg.BaiduAppKey, cfg.BaiduSecretKey, auth, nil)

	// 创建 WebUI 服务器
	ui = webui.NewServer(dl).WithContext(dl.Context())

	// 启动后台扫描任务
	address := net.JoinHostPort(cfg.ServerHost, cfg.ServerPort)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		dl.Shutdown(shutdownCtx)
		log.Fatal(err)
	}
	server := &http.Server{Handler: applicationHandler(gate, authorizationHTTP), BaseContext: func(net.Listener) context.Context { return dl.Context() }, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	scanDone := make(chan struct{})
	dl.StartBackground(auth.Run)
	dl.StartBackground(func(scanCtx context.Context) { defer close(scanDone); runScanner(scanCtx) })
	log.Printf("HTTP server listening on %s\n", address)
	select {
	case <-ctx.Done():
	case err = <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server stopped: %v", err)
		}
		stop()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err = dl.Shutdown(shutdownCtx); err != nil {
		log.Printf("Worker shutdown incomplete: %v", err)
	}
	if err = server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP shutdown incomplete: %v", err)
		server.Close()
	}
	select {
	case <-scanDone:
	case <-shutdownCtx.Done():
		log.Printf("Scanner shutdown incomplete")
	}
	log.Println("Service stopped")
}

func applicationHandler(gate *management.Gate, authorizationHTTP *authorization.Server) http.Handler {
	mux := http.NewServeMux()
	intakeHTTP := intakeapi.New(dl, func() {
		select {
		case scanRequests <- struct{}{}:
		default:
		}
	})
	mux.HandleFunc("/", ui.HandlePageWS)
	mux.HandleFunc("/ws", ui.HandleWS)
	mux.HandleFunc("/poll", ui.HandlePagePoll)
	mux.HandleFunc("/oauth", ui.HandlePageOAuth)
	mux.HandleFunc("/intake", ui.HandlePageIntake)
	mux.HandleFunc("/res/", ui.HandleRes)
	mux.HandleFunc("/api/health", handleHealth)
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/scan", handleScan)
	mux.HandleFunc("/api/tasks/", handleTaskAction)
	mux.HandleFunc("/api/cloud", intakeHTTP.HandleCloud)
	mux.HandleFunc("/api/intakes", intakeHTTP.HandleRecords)
	mux.HandleFunc("/api/intakes/preview", intakeHTTP.HandlePreview)
	mux.HandleFunc("/api/intakes/", intakeHTTP.HandleAction)
	mux.HandleFunc("/api/oauth/device-code", authorizationHTTP.HandleDeviceCode)
	mux.HandleFunc("/api/oauth/token", authorizationHTTP.HandleToken)
	mux.HandleFunc("/api/oauth/refresh", authorizationHTTP.HandleRefresh)
	mux.HandleFunc("/api/oauth/status", authorizationHTTP.HandleStatus)
	return gate.Wrap(mux)
}

func runScanner(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(cfg.ScanInterval) * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if dl.AuthenticationReady() {
			lastScanUnix.Store(time.Now().UnixNano())
			result := &scanResult{}
			if prior := lastScanResult.Load(); prior != nil {
				result.Success = prior.Success
			}
			if err := dl.ScanAndDownload(); err != nil {
				result.Error = "scan_failed"
				if errors.Is(err, downloader.ErrAuthenticationWaiting) || baiduapi.IsAuthenticationError(err) {
					result.Error = "authentication_wait"
				}
				if !errors.Is(err, context.Canceled) {
					log.Printf("[Scanner] 扫描失败: %v", err)
				}
			} else {
				result.Success = time.Now()
			}
			lastScanResult.Store(result)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-scanRequests:
		}
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	lastScanTime := time.Unix(0, lastScanUnix.Load())
	tasks := dl.GetTasks()
	downloadingCount := 0
	doneCount := 0
	failedCount := 0
	pendingCount, retryCount, cancelledCount, authWaitCount := 0, 0, 0, 0

	for _, t := range tasks {
		switch t.Status {
		case "downloading":
			downloadingCount++
		case "done":
			doneCount++
		case "failed":
			failedCount++
		case "pending":
			pendingCount++
		case "retry_wait":
			retryCount++
		case "cancelled":
			cancelledCount++
		case "auth_wait":
			authWaitCount++
		}
	}

	response := map[string]interface{}{
		"authentication": auth.Status(), "auth_wait": authWaitCount,
		"pending": pendingCount, "retry_wait": retryCount, "cancelled": cancelledCount, "max_parallel": cfg.MaxParallel,
		"version":        version,
		"revision":       revision,
		"status":         "ok",
		"last_scan_time": lastScanTime.Format(time.RFC3339),
		"next_scan_time": lastScanTime.Add(time.Duration(cfg.ScanInterval) * time.Second).Format(time.RFC3339),
		"total_tasks":    len(tasks),
		"downloading":    downloadingCount,
		"done":           doneCount,
		"failed":         failedCount,
	}
	if lastScanUnix.Load() == 0 {
		response["last_scan_time"] = ""
		response["next_scan_time"] = ""
	}
	if result := lastScanResult.Load(); result != nil {
		if !result.Success.IsZero() {
			response["last_successful_scan"] = result.Success.Format(time.RFC3339)
		}
		if result.Error != "" {
			response["status"] = "degraded"
			response["scan_error"] = result.Error
		}
	} else {
		response["status"] = "starting"
	}
	if !auth.Status().Ready {
		response["status"] = "waiting_authorization"
	}
	if err := dl.QueueError(); err != nil {
		response["status"] = "degraded"
		response["queue_error"] = err.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	tasks := dl.GetTasks()

	// 转换为适合输出的格式
	type TaskStatus struct {
		FsID           int64  `json:"fs_id"`
		TaskID         string `json:"task_id"`
		CanCancel      bool   `json:"can_cancel"`
		CanRetry       bool   `json:"can_retry"`
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
		Attempts       int    `json:"attempts"`
		NextAttempt    string `json:"next_attempt,omitempty"`
		Root           bool   `json:"is_root"`
		DownloadMode   string `json:"download_mode,omitempty"`
	}

	statuses := make([]TaskStatus, 0, len(tasks))
	for _, t := range tasks {
		ts := TaskStatus{
			FsID:   t.FileInfo.FsID,
			TaskID: t.TaskID, CanCancel: t.CanCancel, CanRetry: t.CanRetry,
			Path:           t.FileInfo.Path,
			ServerFilename: t.FileInfo.ServerFilename,
			Size:           t.FileInfo.Size,
			Status:         t.Status,
			LocalPath:      t.LocalPath,
			Error:          t.Error,
			Progress:       t.Progress,
			Speed:          t.Speed, // 添加速度信息
			Attempts:       t.Attempts,
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

	select {
	case scanRequests <- struct{}{}:
	default:
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "scan triggered",
	})
}

func handleTaskAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/tasks/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid task id", http.StatusBadRequest)
		return
	}
	switch parts[1] {
	case "cancel":
		err = dl.Cancel(id)
	case "retry":
		err = dl.Retry(id)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, downloader.ErrTaskNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "task action accepted"})
}
