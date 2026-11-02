//go:build linux

package downloader

import (
	"errors"
	"syscall"
)

func isLocalStorageFull(err error) bool { return errors.Is(err, syscall.ENOSPC) }
