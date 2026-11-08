//go:build linux

package uploads

import (
	"errors"
	"golang.org/x/sys/unix"
	"math"
)

func uploadAvailableBytes(path string) (int64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, err
	}
	if stat.Bavail < 0 || stat.Bsize < 0 || stat.Bsize != 0 && uint64(stat.Bavail) > math.MaxInt64/uint64(stat.Bsize) {
		return 0, errors.New("invalid available capacity")
	}
	return int64(uint64(stat.Bavail) * uint64(stat.Bsize)), nil
}
