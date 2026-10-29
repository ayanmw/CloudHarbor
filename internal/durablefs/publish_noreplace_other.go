//go:build !linux && !windows

package durablefs

import "errors"

func platformPublishNoReplace(string, string) error {
	return errors.New("atomic no-replace publication is unsupported on this platform")
}
