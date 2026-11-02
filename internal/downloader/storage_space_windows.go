//go:build windows

package downloader

import (
	"errors"
	"math"

	"golang.org/x/sys/windows"
)

func localAvailableBytes(path string) (int64, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, storageProbeError(path, err)
	}
	var available, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(name, &available, &total, &totalFree); err != nil {
		return 0, storageProbeError(path, err)
	}
	if available > math.MaxInt64 {
		return 0, storageProbeError(path, errors.New("available byte count overflows int64"))
	}
	return int64(available), nil
}
