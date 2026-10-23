//go:build !linux

package taskstore

import "os"

func openJobFile(name string) (*os.File, error) {
	if err := safePath(name); err != nil {
		return nil, err
	}
	return os.Open(name)
}
