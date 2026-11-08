//go:build !windows

package foldersync

import (
	"fmt"
	"os"
	"syscall"
)

func fileIdentity(_ string, info os.FileInfo) (string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("无法读取文件系统身份")
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}
func isReparse(os.FileInfo) bool { return false }
