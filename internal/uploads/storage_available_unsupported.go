//go:build !windows && !linux

package uploads

import "errors"

func uploadAvailableBytes(string) (int64, error) {
	return 0, errors.New("available capacity is unsupported on this platform")
}
