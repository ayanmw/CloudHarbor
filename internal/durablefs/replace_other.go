//go:build !windows

package durablefs

import "os"

func platformReplaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
