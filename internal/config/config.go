package config

import (
	"fmt"
	"os"
	"strconv"
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
	BaiduSignKey   string

	// OAuth 相关
	AccessToken  string
	RefreshToken string

	// 业务配置
	QueueDir            string // 网盘中的队列目录，默认 /NAS_Download_Queue
	LocalDownDir        string // 本地下载目录
	ScanInterval        int    // 扫描间隔（秒）
	MaxParallel         int
	MaxRetries          int
	RetryBackoffSeconds int

	// HTTP 服务
	ServerPort string
	ServerHost string
}

// Load 从环境变量加载配置
func Load() *Config {
	cfg := &Config{
		BaiduAppID:     getEnv("BAIDU_APP_ID", ""),
		BaiduAppKey:    getEnv("BAIDU_APP_KEY", ""),
		BaiduSecretKey: getEnv("BAIDU_SECRET_KEY", ""),
		BaiduSignKey:   getEnv("BAIDU_SIGN_KEY", ""),

		AccessToken:  getEnv("BAIDU_ACCESS_TOKEN", ""),
		RefreshToken: getEnv("BAIDU_REFRESH_TOKEN", ""),

		QueueDir: getEnv("QUEUE_DIR", "/NAS_Download_Queue"),
		// 兼容早期 Docker 变量，规范变量优先，避免下载落入容器临时层。
		LocalDownDir:        getEnv("LOCAL_DOWNLOAD_DIR", getEnv("LOCAL_DOWN_DIR", "./downloads")),
		ScanInterval:        getEnvInt("SCAN_INTERVAL", 300), // 默认 5 分钟
		MaxParallel:         getEnvInt("MAX_PARALLEL", 2),
		MaxRetries:          getEnvInt("MAX_RETRIES", 3),
		RetryBackoffSeconds: getEnvInt("RETRY_BACKOFF_SECONDS", 5),

		ServerPort: getEnv("SERVER_PORT", "8080"),
		ServerHost: getEnv("SERVER_HOST", "127.0.0.1"),
	}
	return cfg
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
	}
	return defaultVal
}
