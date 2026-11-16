//go:build !windows && !linux

package library

import (
	"errors"
	"os"
)

func renameNoReplace(*os.Root, string, string, string, string) error {
	return errors.New("当前平台不支持安全的无覆盖改名")
}
func syncParent(*os.Root, string) error { return nil }
