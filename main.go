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
	"math"
	"net"
	"net/http"
	"net/url"
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
	"unicode"
	"unicode/utf8"

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
	scanInProgress    atomic.Bool
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

type globalTaskEventDTO struct {
	Sequence              string          `json:"sequence"`
	JobID                 string          `json:"job_id"`
	Kind                  string          `json:"kind"`
	RecordedAtUTC         time.Time       `json:"recorded_at_utc"`
	OccurredAtUTC         *time.Time      `json:"occurred_at_utc,omitempty"`
	CloudObservedAtUTC    *time.Time      `json:"cloud_observed_at_utc,omitempty"`
	ResponseObservedAtUTC *time.Time      `json:"response_observed_at_utc,omitempty"`
	RunID                 string          `json:"run_id,omitempty"`
	OperationID           string          `json:"operation_id,omitempty"`
	AttemptID             string          `json:"attempt_id,omitempty"`
	EvidenceType          string          `json:"evidence_type"`
	Evidence              json.RawMessage `json:"evidence,omitempty"`
}

func toGlobalTaskEventDTO(events []taskstore.Event) []globalTaskEventDTO {
	result := make([]globalTaskEventDTO, len(events))
	for i, event := range events {
		result[i] = globalTaskEventDTO{
			Sequence: strconv.FormatInt(event.Sequence, 10), JobID: strconv.FormatInt(event.JobID, 10), Kind: event.Kind,
			RecordedAtUTC: event.RecordedAtUTC, OccurredAtUTC: event.OccurredAtUTC, CloudObservedAtUTC: event.CloudObservedAtUTC,
			ResponseObservedAtUTC: event.ResponseObservedAtUTC, RunID: event.RunID, OperationID: event.OperationID,
			AttemptID: event.AttemptID, EvidenceType: event.EvidenceType, Evidence: event.Evidence,
		}
	}
	return result
}

func shouldReportRuntimeQueueScanFailure(result *scanResult, inProgress bool) bool {
	return !inProgress && result != nil && result.Error != ""
}

func applyQueueMonitorHistoryReadFailure(response map[string]interface{}, result *scanResult) {
	response["scan_history_available"] = false
	response["scan_history_error"] = "history_unavailable"
	response["status"] = "degraded"
	if result == nil {
		return
	}
	if !result.Success.IsZero() {
		response["last_successful_scan"] = result.Success.UTC().Format(time.RFC3339)
		response["last_successful_scan_source"] = "runtime"
	}
	if result.Error != "" {
		response["scan_error"] = result.Error
	}
}

type taskStatusDTO struct {
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
	Speed          int64                            `json:"speed,omitempty"`
	Attempts       int                              `json:"attempts"`
	NextAttempt    string                           `json:"next_attempt,omitempty"`
	LocalStorage   *downloader.LocalStorageEvidence `json:"local_storage,omitempty"`
	Root           bool                             `json:"is_root"`
	DownloadMode   string                           `json:"download_mode,omitempty"`
}

func toTaskStatus(t *downloader.Task) taskStatusDTO {
	status := taskStatusDTO{
		FsID: t.FileInfo.FsID, TaskID: t.TaskID, CanCancel: t.CanCancel, CanPause: t.CanPause, CanResume: t.CanResume, CanRetry: t.CanRetry,
		Path: t.FileInfo.Path, ServerFilename: t.FileInfo.ServerFilename, Size: t.FileInfo.Size,
		Status: t.Status, LocalPath: t.LocalPath, Error: t.Error, Progress: t.Progress,
		Speed: t.Speed, Attempts: t.Attempts, Root: t.Root, DownloadMode: string(t.DownloadMode), LocalStorage: t.LocalStorage,
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
	auth, err = credentials.Open(dl, credentials.Options{AppKey: cfg.BaiduAppKey, SecretKey: cfg.BaiduSecretKey, AccessToken: cfg.AccessToken, RefreshToken: cfg.RefreshToken}, credentials.BaiduProvider{RequestPacer: dl.CloudAPIRequestPacer()}, applyCredentialUpdate)
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
	// The management UI must outlive a fail-closed downloader context so it can
	// report queue errors and guide the operator through recovery.
	ui = webui.NewServer(dl).WithContext(ctx)

	// 启动后台扫描任务
	address := net.JoinHostPort(cfg.ServerHost, cfg.ServerPort)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		dl.Shutdown(shutdownCtx)
		log.Fatal(err)
	}
	server := &http.Server{Handler: applicationHandler(gate, authorizationHTTP), BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second}
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
		response := map[string]any{
			"queue_dir": paths.QueueDir, "local_download_dir": paths.LocalDownDir,
			"queue_editable": !cfg.QueueDirExplicit, "local_download_editable": !cfg.LocalDownDirExplicit,
		}
		if targetQueueDir := r.URL.Query().Get("queue_dir"); targetQueueDir != "" {
			query := r.URL.Query()
			offset, limit := 0, 50
			if values, ok := query["offset"]; ok {
				if len(values) != 1 {
					http.Error(w, "监控目录影响预览 offset 无效", http.StatusBadRequest)
					return
				}
				parsed, parseErr := strconv.Atoi(values[0])
				if parseErr != nil || parsed < 0 {
					http.Error(w, "监控目录影响预览 offset 无效", http.StatusBadRequest)
					return
				}
				offset = parsed
			}
			if values, ok := query["limit"]; ok {
				if len(values) != 1 {
					http.Error(w, "监控目录影响预览 limit 无效", http.StatusBadRequest)
					return
				}
				parsed, parseErr := strconv.Atoi(values[0])
				if parseErr != nil || parsed < 1 || parsed > 50 {
					http.Error(w, "监控目录影响预览 limit 无效", http.StatusBadRequest)
					return
				}
				limit = parsed
			}
			impact, err := dl.PreviewManagedPathChangePage(targetQueueDir, offset, limit)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			response["queue_change_impact"] = impact
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	case http.MethodPost:
		var request struct {
			downloader.ManagedPaths
			MigrateFiles             bool   `json:"migrate_files"`
			QueueChangePreviewDigest string `json:"queue_change_preview_digest,omitempty"`
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
		migration, err := dl.SetManagedPathsConfirmed(r.Context(), request.ManagedPaths, request.MigrateFiles, request.QueueChangePreviewDigest)
		if err != nil {
			var confirmationErr *downloader.ManagedPathImpactConfirmationError
			if errors.As(err, &confirmationErr) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "queue_change_impact": confirmationErr.Impact})
				return
			}
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
	mux.HandleFunc("/operations", ui.HandlePageOperations)
	mux.HandleFunc("/monitor-history", ui.HandlePageMonitorHistory)
	mux.HandleFunc("/task-events", ui.HandlePageTaskEvents)
	mux.HandleFunc("/res/", ui.HandleRes)
	mux.HandleFunc("/api/health", handleHealth)
	mux.HandleFunc("/api/monitor-scans", intakeHTTP.HandleQueueMonitorScanRuns)
	mux.HandleFunc("/api/monitor-scans/events", intakeHTTP.HandleQueueMonitorScanEvents)
	mux.HandleFunc("/api/monitor-scans/{id}/export", intakeHTTP.HandleQueueMonitorScanExport)
	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/scan", handleScan)
	mux.HandleFunc("/api/queue/pause", handleQueuePause)
	mux.HandleFunc("/api/queue/resume", handleQueueResume)
	mux.HandleFunc("/api/tasks/{id}/events", handleTaskEvents)
	mux.HandleFunc("/api/tasks/{id}/events/export", handleTaskEventEvidenceExport)
	mux.HandleFunc("/api/tasks/{id}/events/{sequence}/context", handleTaskEventContext)
	mux.HandleFunc("/api/task-events", handleTaskEventHistory)
	mux.HandleFunc("/api/task-events/export", handleTaskEventHistoryExport)
	mux.HandleFunc("/api/tasks/", handleTaskAction)
	mux.HandleFunc("/api/cloud", intakeHTTP.HandleCloud)
	mux.HandleFunc("/api/cloud-inventory", intakeHTTP.HandleInventory)
	mux.HandleFunc("/api/cloud-inventory/incomplete-directories", intakeHTTP.HandleIncompleteCloudInventoryDirectories)
	mux.HandleFunc("/api/cloud-inventory/search", intakeHTTP.HandleInventorySearch)
	mux.HandleFunc("/api/cloud-inventory/size", intakeHTTP.HandleInventorySize)
	mux.HandleFunc("/api/cloud-inventory/scan", intakeHTTP.HandleInventoryScan)
	mux.HandleFunc("/api/cloud-inventory/scan/runs", intakeHTTP.HandleCloudScanHistoryRuns)
	mux.HandleFunc("/api/cloud-inventory/scan/events", intakeHTTP.HandleCloudScanHistoryEvents)
	mux.HandleFunc("/api/cloud-inventory/scan/runs/{id}/export", intakeHTTP.HandleCloudScanHistoryExport)
	mux.HandleFunc("/api/cloud-inventory/refresh", intakeHTTP.HandleInventoryRefresh)
	mux.HandleFunc("/api/cloud-inventory/refresh/events", intakeHTTP.HandleInventoryRefreshEvents)
	mux.HandleFunc("/api/cloud-inventory/refresh/changes", intakeHTTP.HandleInventoryRefreshChanges)
	mux.HandleFunc("/api/cloud-inventory/refresh/changes/export", intakeHTTP.HandleInventoryRefreshChangesExport)
	mux.HandleFunc("/api/cloud-inventory/refresh/runs", intakeHTTP.HandleInventoryRefreshRuns)
	mux.HandleFunc("/api/cloud-inventory/refresh/runs/export", intakeHTTP.HandleInventoryRefreshRunSummaryExport)
	mux.HandleFunc("/api/cloud-inventory/refresh/runs/{id}/export", intakeHTTP.HandleInventoryRefreshEvidenceExport)
	mux.HandleFunc("/api/cloud-cleanup/empty-directories", intakeHTTP.HandleEmptyCloudDirectories)
	mux.HandleFunc("/api/cloud-cleanup/duplicate-scan", intakeHTTP.HandleDuplicateScan)
	mux.HandleFunc("/api/cloud-cleanup/duplicate-scan/events", intakeHTTP.HandleDuplicateScanEvents)
	mux.HandleFunc("/api/cloud-cleanup/duplicate-scan/runs", intakeHTTP.HandleDuplicateScanRuns)
	mux.HandleFunc("/api/cloud-cleanup/duplicate-scan/runs/export", intakeHTTP.HandleDuplicateScanRunSummaryExport)
	mux.HandleFunc("/api/cloud-cleanup/duplicate-scan/runs/{id}/events", intakeHTTP.HandleDuplicateScanRunEvents)
	mux.HandleFunc("/api/cloud-cleanup/duplicate-scan/runs/{id}/export", intakeHTTP.HandleDuplicateScanEvidenceExport)
	mux.HandleFunc("/api/cloud-cleanup/duplicates", intakeHTTP.HandleDuplicatePreview)
	mux.HandleFunc("/api/cloud/mutations", intakeHTTP.HandleCloudMutation)
	mux.HandleFunc("/api/cloud/operations/export", intakeHTTP.HandleCloudOperationExport)
	mux.HandleFunc("/api/cloud/operations", intakeHTTP.HandleCloudOperations)
	mux.HandleFunc("/api/cloud/operations/{id}/events", intakeHTTP.HandleCloudOperationEvents)
	mux.HandleFunc("/api/cloud/operations/{id}/events/export", intakeHTTP.HandleCloudOperationFilteredEvidenceExport)
	mux.HandleFunc("/api/cloud/operations/{id}/export", intakeHTTP.HandleCloudOperationEvidenceExport)
	mux.HandleFunc("/api/settings/paths", handleManagedPaths)
	mux.HandleFunc("/api/settings/local-migration", handleLocalDirectoryMigration)
	mux.HandleFunc("/api/settings/local-migration/export", intakeHTTP.HandleLocalDirectoryMigrationEvidenceExport)
	mux.HandleFunc("/api/settings/local-migrations", intakeHTTP.HandleLocalDirectoryMigrations)
	mux.HandleFunc("/api/settings/local-migrations/export", intakeHTTP.HandleLocalDirectoryMigrationSummaryExport)
	mux.HandleFunc("/api/settings/local-migrations/{id}/events", intakeHTTP.HandleLocalDirectoryMigrationEvents)
	mux.HandleFunc("/api/settings/local-migrations/{id}/export", intakeHTTP.HandleLocalDirectoryMigrationEvidenceExportByID)
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
			scanInProgress.Store(true)
			func() {
				defer scanInProgress.Store(false)
				scanStarted := time.Now()
				result := &scanResult{}
				if prior := lastScanResult.Load(); prior != nil {
					result.Success = prior.Success
				}
				if err := dl.ScanAndDownload(); err != nil {
					if errors.Is(err, downloader.ErrQueueMonitorScanSkipped) {
						return
					}
					lastScanUnix.Store(scanStarted.UnixNano())
					result.Error = "scan_failed"
					if errors.Is(err, downloader.ErrAuthenticationWaiting) || baiduapi.IsAuthenticationError(err) {
						result.Error = "authentication_wait"
					}
					if !errors.Is(err, context.Canceled) {
						log.Printf("[Scanner] 扫描失败: %v", err)
					}
				} else {
					lastScanUnix.Store(scanStarted.UnixNano())
					result.Success = time.Now()
				}
				lastScanResult.Store(result)
			}()
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
		"version":          version,
		"revision":         revision,
		"status":           "ok",
		"last_scan_time":   lastScanTime.Format(time.RFC3339),
		"next_scan_time":   lastScanTime.Add(time.Duration(cfg.ScanInterval) * time.Second).Format(time.RFC3339),
		"total_tasks":      totalTasks,
		"downloading":      downloadingCount,
		"done":             doneCount,
		"failed":           failedCount,
		"scan_in_progress": scanInProgress.Load(),
	}
	if lastScanUnix.Load() == 0 {
		response["last_scan_time"] = ""
		response["next_scan_time"] = ""
	}
	latest, latestSuccess, historyErr := dl.QueueMonitorScanLatest(r.Context())
	if historyErr == nil {
		response["scan_history_available"] = true
		if latest != nil {
			response["last_scan_time"] = latest.StartedAtUTC.Format(time.RFC3339)
			response["next_scan_time"] = latest.StartedAtUTC.Add(time.Duration(cfg.ScanInterval) * time.Second).Format(time.RFC3339)
			if latest.Status == "failed" || latest.Status == "interrupted" {
				response["status"] = "degraded"
				response["scan_error"] = latest.ErrorCode
			} else if latest.Status == "running" && scanInProgress.Load() {
				response["status"] = "scanning"
			} else if latest.Status == "running" {
				response["status"] = "starting"
			}
		} else if result := lastScanResult.Load(); result == nil {
			response["status"] = "starting"
		}
		if latestSuccess != nil && latestSuccess.FinishedAtUTC != nil {
			response["last_successful_scan"] = latestSuccess.FinishedAtUTC.Format(time.RFC3339)
			response["last_successful_scan_source"] = "sqlite"
		}
	} else {
		applyQueueMonitorHistoryReadFailure(response, lastScanResult.Load())
	}
	if retryState, retryErr := dl.QueueMonitorRetryState(r.Context()); retryErr == nil && retryState != nil {
		retryAt := retryState.RetryAtUTC
		response["queue_monitor_retry_at_utc"] = retryAt.Format(time.RFC3339)
		remaining := time.Until(retryAt).Seconds()
		if remaining < 0 {
			remaining = 0
		}
		response["queue_monitor_retry_remaining_seconds"] = int64(math.Ceil(remaining))
		if remaining > 0 {
			response["next_scan_time"] = retryAt.Format(time.RFC3339)
		}
	} else if retryErr != nil {
		response["queue_monitor_retry_state_available"] = false
	}
	// A failed history write leaves older rows readable; keep the current process's latest failure visible.
	if runtimeResult := lastScanResult.Load(); shouldReportRuntimeQueueScanFailure(runtimeResult, scanInProgress.Load()) {
		response["status"] = "degraded"
		response["scan_error"] = runtimeResult.Error
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
	if query.Has("offset") || query.Has("limit") || query.Has("sort_by") || query.Has("status") || query.Has("q") {
		if len(query["status"]) > 1 || len(query["q"]) > 1 {
			http.Error(w, "invalid task filters", http.StatusBadRequest)
			return
		}
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
		page, filterErr := dl.GetTaskPageFiltered(offset, limit, sortBy, query.Get("status"), query.Get("q"))
		if filterErr != nil {
			http.Error(w, "invalid task filters", http.StatusBadRequest)
			return
		}
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
			"queue_total_tasks": page.QueueTotal, "queue_pause": dl.QueuePauseStatus(), "queue_healthy": dl.QueueOperational(),
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
		"tasks": statuses, "queue_pause": dl.QueuePauseStatus(), "queue_healthy": dl.QueueOperational(),
	})
}

func handleQueuePause(w http.ResponseWriter, r *http.Request)  { handleQueuePauseChange(w, r, true) }
func handleQueueResume(w http.ResponseWriter, r *http.Request) { handleQueuePauseChange(w, r, false) }

func handleQueuePauseChange(w http.ResponseWriter, r *http.Request, paused bool) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	state, err := dl.SetQueuePaused(paused)
	if err != nil {
		http.Error(w, "队列状态未更改，持久化或队列状态不可用", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"queue_pause": state}); err != nil {
		return
	}
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
	case "pause":
		err = dl.PauseTask(id)
	case "resume":
		err = dl.ResumeTask(id)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, downloader.ErrTaskNotFound) {
			status = http.StatusNotFound
		} else if errors.Is(err, downloader.ErrQueueUnavailable) {
			status = http.StatusServiceUnavailable
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
	filter, err := parseTaskEventFilter(query, true)
	if err != nil {
		http.Error(w, "invalid task event filter", http.StatusBadRequest)
		return
	}
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
	events, total, nextBefore, more, kindCounts, err := dl.TaskEventPageFilteredWithCounts(r.Context(), id, beforeSequence, limit, filter)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "task events unavailable", http.StatusServiceUnavailable)
		return
	}
	if err = maskTaskEventEvidence(events); err != nil {
		http.Error(w, "task events unavailable", http.StatusServiceUnavailable)
		return
	}
	var next *int64
	if more {
		next = &nextBefore
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"events": events, "total_matches": total, "next_before_sequence": next, "kind_counts": kindCounts,
	})
}

func handleTaskEventContext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid task id", http.StatusBadRequest)
		return
	}
	sequence, err := strconv.ParseInt(r.PathValue("sequence"), 10, 64)
	if err != nil || sequence <= 0 {
		http.Error(w, "invalid event sequence", http.StatusBadRequest)
		return
	}
	eventContext, err := dl.TaskEventContext(r.Context(), id, sequence)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "task event context unavailable", http.StatusServiceUnavailable)
		return
	}
	contextEvents := make([]taskstore.Event, 0, len(eventContext.Before)+1+len(eventContext.After))
	contextEvents = append(contextEvents, eventContext.Before...)
	contextEvents = append(contextEvents, eventContext.Anchor)
	contextEvents = append(contextEvents, eventContext.After...)
	if err = maskTaskEventEvidence(contextEvents); err != nil {
		http.Error(w, "task event context unavailable", http.StatusServiceUnavailable)
		return
	}
	copy(eventContext.Before, contextEvents[:len(eventContext.Before)])
	eventContext.Anchor = contextEvents[len(eventContext.Before)]
	copy(eventContext.After, contextEvents[len(eventContext.Before)+1:])
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(eventContext)
}

func maskTaskEventEvidence(events []taskstore.Event) error {
	for i := range events {
		if len(events[i].Evidence) == 0 {
			continue
		}
		evidence, err := intakeapi.MaskSensitiveEvidence(events[i].Evidence)
		if err != nil {
			return err
		}
		events[i].Evidence = evidence
	}
	return nil
}

func parseTaskEventFilter(query url.Values, allowPage bool) (taskstore.TaskEventFilter, error) {
	allowed := map[string]bool{"kind": true, "operation_id": true, "attempt_id": true, "from_utc": true, "through_utc": true, "recorded_from_utc": true, "recorded_before_utc": true}
	if allowPage {
		allowed["before_sequence"], allowed["limit"] = true, true
	}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			return taskstore.TaskEventFilter{}, fmt.Errorf("invalid query parameter")
		}
	}
	filter := taskstore.TaskEventFilter{}
	if values, ok := query["kind"]; ok {
		filter.Kind = values[0]
		if filter.Kind != "" && !taskstore.IsTaskEventFilterKind(filter.Kind) {
			return taskstore.TaskEventFilter{}, fmt.Errorf("invalid task event kind")
		}
	}
	parseCorrelationID := func(key string) (string, error) {
		values, ok := query[key]
		if !ok {
			return "", nil
		}
		value := values[0]
		if !utf8.ValidString(value) || len(value) < 1 || len(value) > 128 || strings.TrimSpace(value) != value || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return "", fmt.Errorf("invalid task event correlation ID")
		}
		return value, nil
	}
	operationID, err := parseCorrelationID("operation_id")
	if err != nil {
		return taskstore.TaskEventFilter{}, err
	}
	filter.OperationID = operationID
	attemptID, err := parseCorrelationID("attempt_id")
	if err != nil {
		return taskstore.TaskEventFilter{}, err
	}
	filter.AttemptID = attemptID
	parseDate := func(key string) (*time.Time, error) {
		values, ok := query[key]
		if !ok || values[0] == "" {
			return nil, nil
		}
		parsed, err := time.Parse("2006-01-02", values[0])
		if err != nil {
			return nil, fmt.Errorf("invalid UTC date")
		}
		parsed = parsed.UTC()
		return &parsed, nil
	}
	from, err := parseDate("from_utc")
	if err != nil {
		return taskstore.TaskEventFilter{}, err
	}
	through, err := parseDate("through_utc")
	if err != nil {
		return taskstore.TaskEventFilter{}, err
	}
	filter.RecordedAtFromUTC = from
	if through != nil {
		before := through.AddDate(0, 0, 1)
		filter.RecordedAtBeforeUTC = &before
	}
	if from != nil && through != nil && through.Before(*from) {
		return taskstore.TaskEventFilter{}, fmt.Errorf("reversed UTC date range")
	}
	parseExactUTC := func(key string) (*time.Time, error) {
		values, ok := query[key]
		if !ok {
			return nil, nil
		}
		value := values[0]
		if value == "" || !strings.HasSuffix(value, "Z") {
			return nil, fmt.Errorf("exact UTC time must end with Z")
		}
		parsed, parseErr := time.Parse(time.RFC3339Nano, value)
		if parseErr != nil || parsed.Nanosecond()%1000 != 0 {
			return nil, fmt.Errorf("invalid exact UTC time")
		}
		parsed = parsed.UTC()
		return &parsed, nil
	}
	exactFrom, err := parseExactUTC("recorded_from_utc")
	if err != nil {
		return taskstore.TaskEventFilter{}, err
	}
	exactBefore, err := parseExactUTC("recorded_before_utc")
	if err != nil {
		return taskstore.TaskEventFilter{}, err
	}
	if (exactFrom != nil || exactBefore != nil) && (from != nil || through != nil) {
		return taskstore.TaskEventFilter{}, fmt.Errorf("date and exact UTC ranges cannot be combined")
	}
	if exactFrom != nil || exactBefore != nil {
		_, hasFromDate := query["from_utc"]
		_, hasThroughDate := query["through_utc"]
		if hasFromDate || hasThroughDate {
			return taskstore.TaskEventFilter{}, fmt.Errorf("date and exact UTC ranges cannot be combined")
		}
	}
	if exactFrom != nil && exactBefore != nil && !exactBefore.After(*exactFrom) {
		return taskstore.TaskEventFilter{}, fmt.Errorf("reversed exact UTC range")
	}
	filter.RecordedExactFromUTC, filter.RecordedExactBeforeUTC = exactFrom, exactBefore
	return filter, nil
}

func parseGlobalTaskEventFilter(query url.Values, allowPage bool) (taskstore.GlobalTaskEventFilter, int64, *int64, int, error) {
	allowed := map[string]bool{"task_id": true, "as_of_sequence": true, "kind": true, "operation_id": true, "attempt_id": true, "from_utc": true, "through_utc": true, "recorded_from_utc": true, "recorded_before_utc": true}
	if allowPage {
		allowed["before_sequence"], allowed["limit"] = true, true
	}
	filterQuery := make(url.Values)
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			return taskstore.GlobalTaskEventFilter{}, 0, nil, 0, fmt.Errorf("invalid query parameter")
		}
		switch key {
		case "kind", "operation_id", "attempt_id", "from_utc", "through_utc", "recorded_from_utc", "recorded_before_utc":
			filterQuery[key] = values
		}
	}
	eventFilter, err := parseTaskEventFilter(filterQuery, false)
	if err != nil {
		return taskstore.GlobalTaskEventFilter{}, 0, nil, 0, err
	}
	filter := taskstore.GlobalTaskEventFilter{TaskEventFilter: eventFilter}
	parseInt := func(key string, requiredPositive bool) (int64, bool, error) {
		values, ok := query[key]
		if !ok {
			return 0, false, nil
		}
		value, parseErr := strconv.ParseInt(values[0], 10, 64)
		if parseErr != nil || value < 0 || (requiredPositive && value == 0) {
			return 0, true, fmt.Errorf("invalid %s", key)
		}
		return value, true, nil
	}
	if filter.TaskID, _, err = parseInt("task_id", true); err != nil {
		return taskstore.GlobalTaskEventFilter{}, 0, nil, 0, err
	}
	before, _, err := parseInt("before_sequence", false)
	if err != nil {
		return taskstore.GlobalTaskEventFilter{}, 0, nil, 0, err
	}
	asOfValue, asOfPresent, err := parseInt("as_of_sequence", false)
	if err != nil {
		return taskstore.GlobalTaskEventFilter{}, 0, nil, 0, err
	}
	var asOf *int64
	if asOfPresent {
		asOf = &asOfValue
	}
	limit := 50
	if raw, ok := query["limit"]; ok {
		limit, err = strconv.Atoi(raw[0])
		if err != nil || limit < 1 || limit > 50 {
			return taskstore.GlobalTaskEventFilter{}, 0, nil, 0, fmt.Errorf("invalid limit")
		}
	}
	return filter, before, asOf, limit, nil
}

func handleTaskEventHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	filter, before, asOf, limit, err := parseGlobalTaskEventFilter(r.URL.Query(), true)
	if err != nil {
		http.Error(w, "invalid global task event filter", http.StatusBadRequest)
		return
	}
	page, err := dl.TaskEventHistoryPage(r.Context(), filter, before, asOf, limit)
	if errors.Is(err, taskstore.ErrTaskEventHistorySnapshotInvalid) || errors.Is(err, taskstore.ErrTaskEventHistoryCursorInvalid) {
		http.Error(w, "invalid global task event snapshot cursor", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "global task event history unavailable", http.StatusServiceUnavailable)
		return
	}
	if err = maskTaskEventEvidence(page.Events); err != nil {
		http.Error(w, "global task event history unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	var next any
	if page.HasMore {
		next = strconv.FormatInt(page.NextBeforeSequence, 10)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"events": toGlobalTaskEventDTO(page.Events), "total_matches": page.TotalMatches,
		"as_of_sequence": strconv.FormatInt(page.AsOfSequence, 10), "next_before_sequence": next,
		"has_more": page.HasMore, "kind_counts": page.KindCounts,
	})
}

func handleTaskEventHistoryExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	filter, _, asOf, _, err := parseGlobalTaskEventFilter(r.URL.Query(), false)
	if err != nil {
		http.Error(w, "invalid global task event filter", http.StatusBadRequest)
		return
	}
	events, total, exportAsOf, err := dl.TaskEventHistoryExport(r.Context(), filter, asOf)
	if errors.Is(err, taskstore.ErrTaskEventHistorySnapshotInvalid) {
		http.Error(w, "invalid global task event snapshot", http.StatusBadRequest)
		return
	}
	if errors.Is(err, taskstore.ErrTaskEventHistoryExportTooLarge) {
		http.Error(w, "当前筛选匹配超过 1,000 条，未导出部分证据；请缩小筛选条件。", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "global task event evidence export unavailable", http.StatusServiceUnavailable)
		return
	}
	if err = maskTaskEventEvidence(events); err != nil {
		http.Error(w, "global task event evidence export unavailable", http.StatusServiceUnavailable)
		return
	}
	now := time.Now().UTC()
	filters := taskEventFilterSummary(filter.TaskEventFilter)
	if filter.TaskID > 0 {
		filters["task_id"] = strconv.FormatInt(filter.TaskID, 10)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="task-events-global-`+now.Format("20060102T150405Z")+`.json"`)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"exported_at_utc": now, "as_of_sequence": strconv.FormatInt(exportAsOf, 10), "filters": filters, "total_events": total, "events": toGlobalTaskEventDTO(events),
	})
}

func handleTaskEventEvidenceExport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid task id", http.StatusBadRequest)
		return
	}
	filter, err := parseTaskEventFilter(r.URL.Query(), false)
	if err != nil {
		http.Error(w, "invalid task event filter", http.StatusBadRequest)
		return
	}
	events, total, err := dl.TaskEventEvidenceExportFiltered(r.Context(), id, filter)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if errors.Is(err, taskstore.ErrTaskEventEvidenceExportTooLarge) {
		http.Error(w, "当前筛选匹配超过 100 条，未导出部分证据；请缩小类型、UTC 日期或精确时间范围。", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "任务事件证据导出失败，请稍后重试。", http.StatusServiceUnavailable)
		return
	}
	for i := range events {
		if len(events[i].Evidence) == 0 {
			continue
		}
		evidence, maskErr := intakeapi.MaskSensitiveEvidence(events[i].Evidence)
		if maskErr != nil {
			http.Error(w, "任务事件证据导出失败，请稍后重试。", http.StatusServiceUnavailable)
			return
		}
		events[i].Evidence = evidence
	}
	now := time.Now().UTC()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="task-events-`+strconv.FormatInt(id, 10)+`-`+now.Format("20060102T150405Z")+`.json"`)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"exported_at_utc": now,
		"task_id":         strconv.FormatInt(id, 10),
		"filters":         taskEventFilterSummary(filter),
		"total_events":    total,
		"events":          events,
	})
}

func taskEventFilterSummary(filter taskstore.TaskEventFilter) map[string]string {
	result := map[string]string{}
	if filter.Kind != "" {
		result["kind"] = filter.Kind
	}
	if filter.OperationID != "" {
		result["operation_id"] = filter.OperationID
	}
	if filter.AttemptID != "" {
		result["attempt_id"] = filter.AttemptID
	}
	if filter.RecordedAtFromUTC != nil {
		result["from_utc"] = filter.RecordedAtFromUTC.UTC().Format("2006-01-02")
	}
	if filter.RecordedAtBeforeUTC != nil {
		result["through_utc"] = filter.RecordedAtBeforeUTC.AddDate(0, 0, -1).UTC().Format("2006-01-02")
	}
	if filter.RecordedExactFromUTC != nil {
		result["recorded_from_utc"] = filter.RecordedExactFromUTC.UTC().Format(time.RFC3339Nano)
	}
	if filter.RecordedExactBeforeUTC != nil {
		result["recorded_before_utc"] = filter.RecordedExactBeforeUTC.UTC().Format(time.RFC3339Nano)
	}
	return result
}
