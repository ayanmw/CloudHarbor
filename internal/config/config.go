package config

import (
	"os"
	"strconv"
)

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
	QueueDir     string // 网盘中的队列目录，默认 /NAS_Download_Queue
	LocalDownDir string // 本地下载目录
	ScanInterval int    // 扫描间隔（秒）

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
		LocalDownDir: getEnv("LOCAL_DOWNLOAD_DIR", getEnv("LOCAL_DOWN_DIR", "./downloads")),
		ScanInterval: getEnvInt("SCAN_INTERVAL", 300), // 默认 5 分钟

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
