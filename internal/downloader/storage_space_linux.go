//go:build linux

package downloader

import (
	"errors"

	"golang.org/x/sys/unix"
)

func localAvailableBytes(path string) (int64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, storageProbeError(path, err)
	}
	if stat.Bavail < 0 || stat.Bsize < 0 {
		return 0, storageProbeError(path, errors.New("filesystem returned negative available capacity"))
	}
	available, err := checkedAvailableBytes(uint64(stat.Bavail), uint64(stat.Bsize))
	if err != nil {
		return 0, storageProbeError(path, err)
	}
	return available, nil
}
