//go:build windows

package uploads

import (
	"errors"
	"golang.org/x/sys/windows"
	"math"
)

func uploadAvailableBytes(path string) (int64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available, total, free uint64
	if err = windows.GetDiskFreeSpaceEx(p, &available, &total, &free); err != nil {
		return 0, err
	}
	if available > math.MaxInt64 {
		return 0, errors.New("available capacity overflows int64")
	}
	return int64(available), nil
}
