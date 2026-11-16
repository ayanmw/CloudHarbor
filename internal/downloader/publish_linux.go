//go:build linux

package downloader

import "github.com/ayanmw/CloudHarbor/internal/durablefs"

func publishFileNoReplace(source, destination string) error {
	return durablefs.PublishNoReplace(source, destination)
}
