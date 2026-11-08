package taskstore

import (
	"context"
	"fmt"
	"os"
	"time"
)

// CloudDirectoryIdentity is the remote identity whose cached aggregate is read.
// A reused path must not borrow the previous directory's recursively cached size.
type CloudDirectoryIdentity struct {
	FsID int64
	Path string
}
type CloudDirectorySizeSnapshot struct {
	Generation int64
	ReadAtUTC  time.Time
	Sizes      map[int64]CloudDirectorySize
}

// CloudDirectorySizesSnapshot reads identities and generation-scoped aggregates
// in one transaction. It may populate derived local size caches, never remote data.
// No partial snapshot is returned when any query, cancellation or commit fails.
func (s *Store) CloudDirectorySizesSnapshot(ctx context.Context, account string, identities []CloudDirectoryIdentity) (CloudDirectorySizeSnapshot, error) {
	empty := CloudDirectorySizeSnapshot{}
	if account == "" || len(account) > 128 {
		return empty, fmt.Errorf("invalid cloud account")
	}
	ids := make(map[int64]bool, len(identities))
	paths := make(map[string]bool, len(identities))
	for _, i := range identities {
		if i.FsID <= 0 || i.Path == "/" || !validInventoryPath(i.Path) || ids[i.FsID] || paths[i.Path] {
			return empty, fmt.Errorf("invalid cloud directory identities")
		}
		ids[i.FsID] = true
		paths[i.Path] = true
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return empty, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	result := CloudDirectorySizeSnapshot{ReadAtUTC: time.Now().UTC(), Sizes: make(map[int64]CloudDirectorySize, len(identities))}
	if err = cloudInventoryGeneration(tx, account, &result.Generation); err != nil {
		return empty, err
	}
	for _, i := range identities {
		var count, fsid, isDirectory int64
		if err = tx.QueryRowContext(ctx, `SELECT count(*),coalesce(max(fs_id),0),coalesce(max(is_dir),0) FROM cloud_inventory WHERE account_fingerprint=? AND path=?`, account, i.Path).Scan(&count, &fsid, &isDirectory); err != nil {
			return empty, err
		}
		size := CloudDirectorySize{Path: i.Path}
		if count == 1 && fsid == i.FsID && isDirectory == 1 {
			size, err = cloudDirectorySizeInTx(tx, account, i.Path, result.ReadAtUTC)
			if err != nil {
				return empty, err
			}
		}
		result.Sizes[i.FsID] = size
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return result, nil
}
