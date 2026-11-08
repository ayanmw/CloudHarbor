package downloader

import (
	"encoding/hex"
	"fmt"
	"path"
	"strings"
	"web_browser_baidu_onekey_upload/internal/foldersync"
)

func syncRemoteArchivePath(c foldersync.Config, run, p string) (string, error) {
	if c.Mode != "both" {
		return "", fmt.Errorf("单向同步不得归档网盘内容")
	}
	for _, id := range []string{c.ID, run} {
		if len(id) != 32 {
			return "", fmt.Errorf("归档配置或批次ID无效")
		}
		if _, err := hex.DecodeString(id); err != nil {
			return "", err
		}
	}
	if !validCloudPath(c.RemoteRoot) || c.RemoteRoot == "/" || p == "" || strings.ContainsAny(p, "\\:\x00") || strings.Split(p, "/")[0] == ".nas-sync-history" {
		return "", fmt.Errorf("归档相对路径无效")
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("归档路径越界")
		}
	}
	return path.Join(c.RemoteRoot, ".nas-sync-history", c.ID, run, p), nil
}
