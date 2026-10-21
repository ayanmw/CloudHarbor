//go:build !linux && !windows

package taskstore

import (
	"fmt"
	"os"
)

func lockFile(f *os.File) error {
	return fmt.Errorf("persistent queue supports Windows and Linux only")
}
