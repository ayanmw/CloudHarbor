//go:build !linux

package durablefs

// Directory fsync is not available through a portable Go file operation on
// these platforms. File Sync and native rename/link behavior remain unchanged.
func platformSyncDirectory(string) error { return nil }
