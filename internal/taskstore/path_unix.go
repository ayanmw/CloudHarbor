//go:build !windows

package taskstore

import "os"

func statePathReparse(os.FileInfo) bool { return false }
