//go:build !linux && !windows

package downloader

import "web_browser_baidu_onekey_upload/internal/durablefs"

func publishFileNoReplace(source, destination string) error {
	return durablefs.PublishNoReplace(source, destination)
}
