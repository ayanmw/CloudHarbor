package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// The digest changes on scope changes as well as committed cleanup successes.
// It is only a local observation token, not a remote directory version.
func (d *Downloader) CloudCleanupRevision(ctx context.Context) (string, error) {
	scope, err := d.cleanupScope(ctx)
	if err != nil {
		return "", err
	}
	sequence, err := d.queue.store.CleanupChangeSequence(ctx, scope)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(scope.AccountFingerprint + ":" + scope.ApplicationFingerprint + ":" + scope.CacheNamespace + ":" + strconv.FormatInt(sequence, 10)))
	return hex.EncodeToString(digest[:]), nil
}
