package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func (c *Config) Validate() error {
	if c.ScanInterval < 1 || c.ScanInterval > 86400 {
		return fmt.Errorf("SCAN_INTERVAL must be 1..86400 seconds")
	}
	if c.MaxParallel < 1 || c.MaxParallel > 32 {
		return fmt.Errorf("MAX_PARALLEL must be 1..32")
	}
	if c.MaxRetries < 0 || c.MaxRetries > 20 {
		return fmt.Errorf("MAX_RETRIES must be 0..20")
	}
	if c.RetryBackoffSeconds < 1 || c.RetryBackoffSeconds > 300 {
		return fmt.Errorf("RETRY_BACKOFF_SECONDS must be 1..300")
	}
	port, err := strconv.Atoi(c.ServerPort)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("SERVER_PORT must be 1..65535")
	}
	return nil
}

// Config 全局配置
type Config struct {
	// 百度网盘 App 配置
	BaiduAppID     string
	BaiduAppKey    string
	BaiduSecretKey string
	BaiduAppSource string
	BaiduSignKey   string

	// OAuth 相关
	AccessToken  string
	RefreshToken string

	// 业务配置
	QueueDir             string // 网盘中的队列目录，默认 /NAS_Download_Queue
	QueueDirExplicit     bool   // .env 或启动环境明确设置时，网页持久化值不可覆盖
	LocalDownDir         string // 本地下载目录
	LocalDownDirExplicit bool
	StateDir             string // SQLite 与应用 sidecar 的稳定目录；空时沿用下载目录内的旧位置
	ScanInterval         int    // 扫描间隔（秒）
	MaxParallel          int
	MaxRetries           int
	RetryBackoffSeconds  int

	// HTTP 服务
	ServerPort    string
	ServerHost    string
	AdminPassword string
	PublicOrigin  string // Optional external HTTPS management origin behind a TLS proxy.
}

// Load 从环境变量加载配置
func Load() *Config {
	cfg, err := LoadFrom(".env")
	if err != nil {
		cfg, _ = LoadFrom(os.DevNull)
	}
	return cfg
}

// LoadFrom loads application keys from dotenvPath, overridden by the process
// environment. Other settings keep their existing process-environment rules.
func LoadFrom(dotenvPath string) (*Config, error) {
	dotenv, err := loadDotEnv(dotenvPath)
	if err != nil {
		return nil, err
	}
	appID := dotenv["BAIDU_APP_ID"]
	appKey, secretKey, signKey := dotenv["BAIDU_APP_KEY"], dotenv["BAIDU_SECRET_KEY"], dotenv["BAIDU_SIGN_KEY"]
	source := ""
	if appID != "" || appKey != "" || secretKey != "" || signKey != "" {
		source = "env"
	}
	if value, ok := os.LookupEnv("BAIDU_APP_ID"); ok && value != "" {
		appID = value
	}
	if value, ok := os.LookupEnv("BAIDU_APP_KEY"); ok && value != "" {
		appKey = value
	}
	if value, ok := os.LookupEnv("BAIDU_SECRET_KEY"); ok && value != "" {
		secretKey = value
	}
	if value, ok := os.LookupEnv("BAIDU_SIGN_KEY"); ok && value != "" {
		signKey = value
	}
	if appID != "" || appKey != "" || secretKey != "" || signKey != "" {
		source = "env"
	}
	queueDir := dotenv["QUEUE_DIR"]
	queueDirExplicit := queueDir != ""
	if value := os.Getenv("QUEUE_DIR"); value != "" {
		queueDir, queueDirExplicit = value, true
	}
	queueDirDefault := dotenv["QUEUE_DIR_DEFAULT"]
	if value := os.Getenv("QUEUE_DIR_DEFAULT"); value != "" {
		queueDirDefault = value
	}
	if queueDir == "" {
		queueDir = queueDirDefault
	}
	if queueDir == "" {
		queueDir = "/NAS_Download_Queue"
	}
	localDownDir := os.Getenv("LOCAL_DOWNLOAD_DIR")
	localDownDirExplicit := localDownDir != ""
	if localDownDir == "" {
		localDownDir = os.Getenv("LOCAL_DOWN_DIR")
		localDownDirExplicit = localDownDir != ""
	}
	if localDownDir == "" {
		localDownDir = dotenv["LOCAL_DOWNLOAD_DIR"]
		localDownDirExplicit = localDownDir != ""
	}
	if localDownDir == "" {
		localDownDir = dotenv["LOCAL_DOWN_DIR"]
		localDownDirExplicit = localDownDir != ""
	}
	if localDownDir == "" {
		localDownDir = "./downloads"
	}
	cfg := &Config{
		BaiduAppID:     appID,
		BaiduAppKey:    appKey,
		BaiduSecretKey: secretKey,
		BaiduSignKey:   signKey,
		BaiduAppSource: source,

		AccessToken:  getEnv("BAIDU_ACCESS_TOKEN", ""),
		RefreshToken: getEnv("BAIDU_REFRESH_TOKEN", ""),

		QueueDir:         queueDir,
		QueueDirExplicit: queueDirExplicit,
		// 兼容早期 Docker 变量，规范变量优先，避免下载落入容器临时层。
		LocalDownDir:         localDownDir,
		LocalDownDirExplicit: localDownDirExplicit,
		ScanInterval:         getEnvInt("SCAN_INTERVAL", 300), // 默认 5 分钟
		MaxParallel:          getEnvInt("MAX_PARALLEL", 2),
		MaxRetries:           getEnvInt("MAX_RETRIES", 3),
		RetryBackoffSeconds:  getEnvInt("RETRY_BACKOFF_SECONDS", 5),

		ServerPort:    getEnv("SERVER_PORT", "8080"),
		ServerHost:    getEnv("SERVER_HOST", "127.0.0.1"),
		AdminPassword: getEnv("ADMIN_PASSWORD", ""),
		PublicOrigin:  getEnv("PUBLIC_ORIGIN", ""),
	}
	return cfg, nil
}

func loadDotEnv(name string) (map[string]string, error) {
	values := map[string]string{}
	f, err := os.Open(name)
	if os.IsNotExist(err) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024), 64*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("invalid .env entry")
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key == "" {
			return nil, fmt.Errorf("invalid .env entry")
		}
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func ResolveBaiduAppConfig(envID, envKey, envSecret, envSign, queueID, queueKey, queueSecret, queueSign string) (string, string, string, string, string, error) {
	envValues := []string{envID, envKey, envSecret, envSign}
	queueValues := []string{queueID, queueKey, queueSecret, queueSign}
	envCount, queueCount := 0, 0
	for _, value := range envValues {
		if value != "" {
			envCount++
		}
	}
	for _, value := range queueValues {
		if value != "" {
			queueCount++
		}
	}
	if envCount != 0 && envCount != len(envValues) {
		return "", "", "", "", "", fmt.Errorf("BAIDU_APP_ID, BAIDU_APP_KEY, BAIDU_SECRET_KEY and BAIDU_SIGN_KEY must be configured together")
	}
	if envCount == len(envValues) {
		return envID, envKey, envSecret, envSign, "env", nil
	}
	if queueCount != 0 && queueCount != len(queueValues) {
		return "", "", "", "", "", fmt.Errorf("queue Baidu application config must contain all four credentials")
	}
	if queueCount == len(queueValues) {
		return queueID, queueKey, queueSecret, queueSign, "queue", nil
	}
	return "", "", "", "", "", nil
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
		// Keep invalid explicit values visible to Validate instead of silently defaulting.
		return -1
	}
	return defaultVal
}
