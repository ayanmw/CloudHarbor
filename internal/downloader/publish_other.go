//go:build !linux && !windows

package downloader

import "errors"

func publishFileNoReplace(string, string) error {
	return errors.New("atomic no-replace file publication is unsupported on this platform")
}
