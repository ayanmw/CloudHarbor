package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
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
	"web_browser_baidu_onekey_upload/internal/taskstore"
	"web_browser_baidu_onekey_upload/internal/webui"
)

var (
	cfg               *config.Config
	dl                *downloader.Downloader
	ui                *webui.Server
	lastScanUnix      atomic.Int64
	auth              *credentials.Manager
	authorizationHTTP *authorization.Server
	appConfigMu       sync.RWMutex
	appConfigSaveMu   sync.Mutex
	lastScanResult    atomic.Pointer[scanResult]
	scanRequests      = make(chan struct{}, 1)
)

var version = "dev"
var revision = "unknown"

type scanResult struct {
	Success time.Time
	Error   string
}

type taskStatusDTO struct {
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
	Speed          int64  `json:"speed,omitempty"`
	Attempts       int    `json:"attempts"`
	NextAttempt    string `json:"next_attempt,omitempty"`
	Root           bool   `json:"is_root"`
	DownloadMode   string `json:"download_mode,omitempty"`
}

func toTaskStatus(t *downloader.Task) taskStatusDTO {
	status := taskStatusDTO{
		FsID: t.FileInfo.FsID, TaskID: t.TaskID, CanCancel: t.CanCancel, CanRetry: t.CanRetry,
		Path: t.FileInfo.Path, ServerFilename: t.FileInfo.ServerFilename, Size: t.FileInfo.Size,
		Status: t.Status, LocalPath: t.LocalPath, Error: t.Error, Progress: t.Progress,
		Speed: t.Speed, Attempts: t.Attempts, Root: t.Root, DownloadMode: string(t.DownloadMode),
	}
	if !t.StartTime.IsZero() {
		status.StartTime = t.StartTime.Format(time.RFC3339)
	}
	if !t.NextAttempt.IsZero() {
		status.NextAttempt = t.NextAttempt.Format(time.RFC3339)
	}
	if !t.FinishTime.IsZero() {
		status.FinishTime = t.FinishTime.Format(time.RFC3339)
	}
	return status
}

func loadMainConfig(dotenvPath string) error {
	loaded, err := config.LoadFrom(dotenvPath)
	if err != nil {
		return err
	}
	cfg = loaded
	return nil
}

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	revokeAdminSessions := flag.Bool("revoke-admin-sessions", false, "revoke all persistent browser admin sessions and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("baidu-nas-downloader " + version + " (" + revision + ")")
		return
	}
	log.Println("baidu-nas-downloader service starting...")

	// 加载配置
	var err error
	if err = loadMainConfig(".env"); err != nil {
		log.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}
	workDir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	localDir, err := filepath.Abs(cfg.LocalDownDir)
	if err != nil {
		log.Fatal(err)
	}
	cfg.StateDir = filepath.Join(workDir, ".nas-downloader")
	if err = downloader.PrepareStateDirectory(filepath.Join(cfg.StateDir, "queue"), filepath.Join(localDir, ".nas-downloader", "queue")); err != nil {
		log.Fatalf("cannot prepare stable application state: %v", err)
	}
	if *revokeAdminSessions {
		queueStateDir := filepath.Join(cfg.StateDir, "queue")
		if _, err := os.Stat(filepath.Join(queueStateDir, "manifest.json")); err != nil {
			log.Fatalf("cannot revoke browser sessions: queue state is unavailable: %v", err)
		}
		store, err := taskstore.OpenUsingExistingScope(queueStateDir, path.Clean(cfg.QueueDir))
		if err != nil {
			log.Fatal(err)
		}
		err = store.RevokeAllAdminSessions(time.Now().UTC())
		closeErr := store.Close()
		if err != nil {
			log.Fatal(err)
		}
		if closeErr != nil {
			log.Fatal(closeErr)
		}
		log.Println("All browser administrator sessions were revoked.")
		return
	}

	log.Printf("配置加载完成: QueueDir=%s, LocalDownDir=%s, ScanInterval=%ds",
		cfg.QueueDir, cfg.LocalDownDir, cfg.ScanInterval)

	// 创建百度网盘客户端
	client := baiduapi.NewClient(cfg.AccessToken, cfg.BaiduAppKey)

	// 创建下载器
	dl, err = downloader.OpenDownloader(cfg, client)
	if err != nil {
		log.Fatal(err)
	}
	dl.SetLocalMigrationCompletionHandler(func() {
		select {
		case scanRequests <- struct{}{}:
		default:
		}
	})
	if err = dl.ResumeLocalDirectoryMigration(); err != nil {
		log.Fatal(err)
	}
	managedPaths := dl.GetManagedPaths()
	log.Printf("当前生效路径: QueueDir=%s, LocalDownDir=%s", managedPaths.QueueDir, managedPaths.LocalDownDir)
	queueAppID, queueAppKey, queueSecretKey, queueSignKey, err := dl.LoadBaiduAppConfig()
	if err != nil {
		log.Fatal(err)
	}
	cfg.BaiduAppID, cfg.BaiduAppKey, cfg.BaiduSecretKey, cfg.BaiduSignKey, cfg.BaiduAppSource, err = config.ResolveBaiduAppConfig(cfg.BaiduAppID, cfg.BaiduAppKey, cfg.BaiduSecretKey, cfg.BaiduSignKey, queueAppID, queueAppKey, queueSecretKey, queueSignKey)
	if err != nil {
		log.Fatal(err)
	}
	client = baiduapi.NewClient(cfg.AccessToken, cfg.BaiduAppKey)
	dl.UpdateClient(client)
	if diagnostic, diagnosticErr := dl.TaskStoreStartupDiagnostics(); diagnosticErr != nil {
		log.Printf("SQLite startup diagnostics unavailable: %v", diagnosticErr)
	} else if diagnosticJSON, marshalErr := json.Marshal(diagnostic); marshalErr != nil {
		log.Printf("SQLite startup diagnostics unavailable: %v", marshalErr)
	} else {
		log.Printf("SQLite startup diagnostics: %s", diagnosticJSON)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dl.SetAuthentication(client, false, time.Time{})
	auth, err = credentials.Open(dl, credentials.Options{AppKey: cfg.BaiduAppKey, SecretKey: cfg.BaiduSecretKey, AccessToken: cfg.AccessToken, RefreshToken: cfg.RefreshToken}, nil, applyCredentialUpdate)
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
	gate, err := management.NewWithOrigin(adminKey, cfg.PublicOrigin)
	if err != nil {
		log.Fatal(err)
	}
	gate.SetSessionStore(dl)
	if cfg.AdminPassword == "" {
		log.Printf("Administrator username: admin; password stored in %s", filepath.Join(cfg.StateDir, "queue", "admin-key.json"))
	}
	authorizationHTTP = authorization.New(dl.Context(), cfg.BaiduAppKey, cfg.BaiduSecretKey, auth, nil)

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

func applyCredentialUpdate(update credentials.Update) {
	appConfigMu.RLock()
	appKey := cfg.BaiduAppKey
	appConfigMu.RUnlock()
	dl.SetAuthentication(baiduapi.NewClient(update.AccessToken, appKey), update.Ready, update.ExpiresAt)
	if update.Ready {
		select {
		case scanRequests <- struct{}{}:
		default:
		}
	}
}

func maskCredential(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return strings.Repeat("*", len(value))
	}
	left, right := 2, 2
	if len(value) < 9 {
		left, right = 1, 1
	}
	return value[:left] + "***" + value[len(value)-right:]
}

func handleAppConfig(w http.ResponseWriter, r *http.Request) {
	appConfigSaveMu.Lock()
	defer appConfigSaveMu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		appConfigMu.RLock()
		configured := cfg.BaiduAppID != "" && cfg.BaiduAppKey != "" && cfg.BaiduSecretKey != "" && cfg.BaiduSignKey != ""
		response := map[string]any{"configured": configured, "editable": !configured && cfg.BaiduAppSource == "", "source": cfg.BaiduAppSource}
		if configured {
			response["app_id_mask"] = maskCredential(cfg.BaiduAppID)
			response["app_key_mask"] = maskCredential(cfg.BaiduAppKey)
			response["secret_key_mask"] = maskCredential(cfg.BaiduSecretKey)
			response["sign_key_mask"] = maskCredential(cfg.BaiduSignKey)
		}
		appConfigMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	case http.MethodPost:
		var request struct {
			AppID     string `json:"app_id"`
			AppKey    string `json:"app_key"`
			SecretKey string `json:"secret_key"`
			SignKey   string `json:"sign_key"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			http.Error(w, "invalid application config request", http.StatusBadRequest)
			return
		}
		request.AppID, request.AppKey = strings.TrimSpace(request.AppID), strings.TrimSpace(request.AppKey)
		request.SecretKey, request.SignKey = strings.TrimSpace(request.SecretKey), strings.TrimSpace(request.SignKey)
		if request.AppID == "" || request.AppKey == "" || request.SecretKey == "" || request.SignKey == "" || len(request.AppID) > 4096 || len(request.AppKey) > 4096 || len(request.SecretKey) > 4096 || len(request.SignKey) > 4096 {
			http.Error(w, "all four application credentials are required", http.StatusBadRequest)
			return
		}
		appConfigMu.RLock()
		alreadyConfigured := cfg.BaiduAppSource != "" || cfg.BaiduAppID != "" || cfg.BaiduAppKey != "" || cfg.BaiduSecretKey != "" || cfg.BaiduSignKey != ""
		appConfigMu.RUnlock()
		if alreadyConfigured {
			http.Error(w, "application config is already set", http.StatusConflict)
			return
		}
		if err := auth.ConfigureApp(r.Context(), request.AppKey, request.SecretKey, func() error {
			return dl.SaveBaiduAppConfigIfEmpty(request.AppID, request.AppKey, request.SecretKey, request.SignKey)
		}); err != nil {
			if errors.Is(err, taskstore.ErrBaiduAppConfigExists) {
				http.Error(w, "application config is already set", http.StatusConflict)
				return
			}
			http.Error(w, "application config could not be enabled", http.StatusConflict)
			return
		}
		if err := authorizationHTTP.ConfigureApp(request.AppKey, request.SecretKey); err != nil {
			// The same validation already succeeded in the credential manager;
			// this is an invariant failure and should fail closed.
			http.Error(w, "application config was saved but activation failed; restart the service", http.StatusInternalServerError)
			return
		}
		appConfigMu.Lock()
		cfg.BaiduAppID, cfg.BaiduAppKey, cfg.BaiduSecretKey, cfg.BaiduSignKey, cfg.BaiduAppSource = request.AppID, request.AppKey, request.SecretKey, request.SignKey, "queue"
		dl.UpdateClient(baiduapi.NewClient(cfg.AccessToken, cfg.BaiduAppKey))
		appConfigMu.Unlock()
		auth.Wake()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"configured": true, "source": "queue", "app_id_mask": maskCredential(request.AppID), "app_key_mask": maskCredential(request.AppKey), "secret_key_mask": maskCredential(request.SecretKey), "sign_key_mask": maskCredential(request.SignKey)})
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func appConfigBarrier(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appConfigSaveMu.Lock()
		defer appConfigSaveMu.Unlock()
		next(w, r)
	}
}

func handleManagedPaths(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		paths := dl.GetManagedPaths()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"queue_dir": paths.QueueDir, "local_download_dir": paths.LocalDownDir,
			"queue_editable": !cfg.QueueDirExplicit, "local_download_editable": !cfg.LocalDownDirExplicit,
		})
	case http.MethodPost:
		var request struct {
			downloader.ManagedPaths
			MigrateFiles bool `json:"migrate_files"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
			http.Error(w, "invalid path settings request", http.StatusBadRequest)
			return
		}
		current := dl.GetManagedPaths()
		if (cfg.QueueDirExplicit && request.QueueDir != current.QueueDir) || (cfg.LocalDownDirExplicit && !sameLocalSettingPath(request.LocalDownDir, current.LocalDownDir)) {
			http.Error(w, "此路径由 .env 或启动环境配置，请在那里修改", http.StatusConflict)
			return
		}
		migration, err := dl.SetManagedPaths(r.Context(), request.ManagedPaths, request.MigrateFiles)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if migration.ID != "" {
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"migration": migration, "paths": dl.GetManagedPaths()})
			return
		}
		_ = json.NewEncoder(w).Encode(dl.GetManagedPaths())
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleLocalDirectoryMigration(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	migration, err := dl.LocalDirectoryMigrationStatus()
	if errors.Is(err, sql.ErrNoRows) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "none"})
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(migration)
}

func sameLocalSettingPath(a, b string) bool {
	a, errA := filepath.Abs(a)
	b, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
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
	mux.HandleFunc("/login", ui.HandlePageLogin)
	mux.HandleFunc("/api/login", gate.HandleLogin)
	mux.HandleFunc("/api/logout", gate.HandleLogout)
	mux.HandleFunc("/ws", ui.HandleWS)
	mux.HandleFunc("/poll", ui.HandlePagePoll)
	mux.HandleFunc("/oauth", ui.HandlePageOAuth)
	mux.HandleFunc("/intake", ui.HandlePageIntake)
	mux.HandleFunc("/cloud", ui.HandlePageCloud)
	mux.HandleFunc("/res/", ui.HandleRes)
	mux.HandleFunc("/api/health", handleHealth)
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/scan", handleScan)
	mux.HandleFunc("/api/tasks/{id}/events", handleTaskEvents)
	mux.HandleFunc("/api/tasks/", handleTaskAction)
	mux.HandleFunc("/api/cloud", intakeHTTP.HandleCloud)
	mux.HandleFunc("/api/cloud-inventory", intakeHTTP.HandleInventory)
	mux.HandleFunc("/api/cloud-inventory/size", intakeHTTP.HandleInventorySize)
	mux.HandleFunc("/api/cloud-inventory/scan", intakeHTTP.HandleInventoryScan)
	mux.HandleFunc("/api/cloud-cleanup/empty-directories", intakeHTTP.HandleEmptyCloudDirectories)
	mux.HandleFunc("/api/cloud-cleanup/duplicate-scan", intakeHTTP.HandleDuplicateScan)
	mux.HandleFunc("/api/cloud-cleanup/duplicates", intakeHTTP.HandleDuplicatePreview)
	mux.HandleFunc("/api/cloud/mutations", intakeHTTP.HandleCloudMutation)
	mux.HandleFunc("/api/settings/paths", handleManagedPaths)
	mux.HandleFunc("/api/settings/local-migration", handleLocalDirectoryMigration)
	mux.HandleFunc("/api/intakes", intakeHTTP.HandleRecords)
	mux.HandleFunc("/api/intakes/preview", intakeHTTP.HandlePreview)
	mux.HandleFunc("/api/intakes/", intakeHTTP.HandleAction)
	mux.HandleFunc("/api/app-config", handleAppConfig)
	mux.HandleFunc("/api/oauth/device-code", appConfigBarrier(authorizationHTTP.HandleDeviceCode))
	mux.HandleFunc("/api/oauth/token", appConfigBarrier(authorizationHTTP.HandleToken))
	mux.HandleFunc("/api/oauth/refresh", appConfigBarrier(authorizationHTTP.HandleRefresh))
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
	totalTasks, taskCounts := dl.GetTaskStats()
	downloadingCount := taskCounts["downloading"]
	doneCount := taskCounts["done"]
	failedCount := taskCounts["failed"]
	pendingCount := taskCounts["pending"]
	retryCount := taskCounts["retry_wait"]
	cancelledCount := taskCounts["cancelled"]
	authWaitCount := taskCounts["auth_wait"]
	sourceUnknownCount := taskCounts["source_unknown"]

	response := map[string]interface{}{
		"authentication": auth.Status(), "auth_wait": authWaitCount, "source_unknown": sourceUnknownCount,
		"pending": pendingCount, "retry_wait": retryCount, "cancelled": cancelledCount, "max_parallel": cfg.MaxParallel,
		"version":        version,
		"revision":       revision,
		"status":         "ok",
		"last_scan_time": lastScanTime.Format(time.RFC3339),
		"next_scan_time": lastScanTime.Add(time.Duration(cfg.ScanInterval) * time.Second).Format(time.RFC3339),
		"total_tasks":    totalTasks,
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
	query := r.URL.Query()
	if query.Has("offset") || query.Has("limit") || query.Has("sort_by") {
		offset := 0
		if raw := query.Get("offset"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 0 {
				http.Error(w, "invalid offset", http.StatusBadRequest)
				return
			}
			offset = parsed
		}
		limit := 100
		if raw := query.Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 || parsed > 200 {
				http.Error(w, "invalid limit", http.StatusBadRequest)
				return
			}
			limit = parsed
		}
		sortBy := query.Get("sort_by")
		if sortBy != "" && sortBy != "name" && sortBy != "progress" && sortBy != "size" {
			http.Error(w, "invalid sort_by", http.StatusBadRequest)
			return
		}
		page := dl.GetTaskPage(offset, limit, sortBy)
		statuses := make([]taskStatusDTO, 0, len(page.Tasks))
		for _, task := range page.Tasks {
			statuses = append(statuses, toTaskStatus(task))
		}
		var nextOffset *int
		if page.HasMore {
			next := page.Offset + page.Limit
			nextOffset = &next
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tasks": statuses, "total_tasks": page.Total, "status_counts": page.Counts,
			"offset": page.Offset, "limit": page.Limit, "has_more": page.HasMore, "next_offset": nextOffset,
		})
		return
	}

	tasks := dl.GetTasks()

	statuses := make([]taskStatusDTO, 0, len(tasks))
	for _, t := range tasks {
		statuses = append(statuses, toTaskStatus(t))
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

func handleTaskEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid task id", http.StatusBadRequest)
		return
	}
	query := r.URL.Query()
	beforeSequence := int64(0)
	if raw := query.Get("before_sequence"); raw != "" {
		beforeSequence, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || beforeSequence < 0 {
			http.Error(w, "invalid before_sequence", http.StatusBadRequest)
			return
		}
	}
	limit := 20
	if raw := query.Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 50 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
	}
	events, nextBefore, more, err := dl.TaskEventPage(id, beforeSequence, limit)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "task events unavailable", http.StatusServiceUnavailable)
		return
	}
	var next *int64
	if more {
		next = &nextBefore
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"events": events, "next_before_sequence": next,
	})
}
