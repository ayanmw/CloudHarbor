package taskstore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	"web_browser_baidu_onekey_upload/internal/directoryanalysis"
)

type CloudAnalysisSnapshot struct {
	Generation int64
	ReadAtUTC  time.Time
	Inputs     []directoryanalysis.Input
}

// CloudAnalysisSnapshot reads both comparison sources from one transaction.
// It never requests the provider, writes derived aggregates, or certifies that
// cached observations still match the current remote namespace.
func (s *Store) CloudAnalysisSnapshot(ctx context.Context, account string, roots []string) (CloudAnalysisSnapshot, error) {
	empty := CloudAnalysisSnapshot{}
	if account == "" || len(account) > 128 || len(roots) < 1 || len(roots) > 2 {
		return empty, fmt.Errorf("invalid analysis scope")
	}
	for _, root := range roots {
		if !validInventoryPath(root) {
			return empty, fmt.Errorf("invalid analysis root")
		}
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return empty, os.ErrClosed
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	result := CloudAnalysisSnapshot{ReadAtUTC: time.Now().UTC(), Inputs: []directoryanalysis.Input{}}
	if err = cloudInventoryGeneration(tx, account, &result.Generation); err != nil {
		return empty, err
	}
	for _, root := range roots {
		input := directoryanalysis.Input{Label: "网盘缓存", Root: root, Entries: []directoryanalysis.Entry{}}
		prefix, upper := cloudDirectoryPathRange(root)
		var complete, dirs, incomplete, missing int64
		var observed sql.NullInt64
		if err = tx.QueryRowContext(ctx, cloudInventoryDirectoryAggregateSQL, root, root, account, root, prefix, upper, account, root, prefix, upper, account, root, prefix, upper, account, root, prefix, upper, account, root, prefix, upper).Scan(&complete, &dirs, &incomplete, &observed, &missing); err != nil {
			return empty, err
		}
		input.Complete = complete == 1 && incomplete == 0 && missing == 0
		rows, err := tx.QueryContext(ctx, `SELECT e.path,e.fs_id,e.is_dir,e.size_bytes,e.observed_at_utc,coalesce(h.sha256,'') FROM cloud_inventory e LEFT JOIN cloud_content_hashes h ON h.account_fingerprint=e.account_fingerprint AND h.fs_id=e.fs_id AND h.path=e.path AND h.size_bytes=e.size_bytes AND h.provider_mtime=e.provider_mtime AND e.is_dir=0 WHERE e.account_fingerprint=? AND e.path>=? AND e.path<? ORDER BY e.path,e.fs_id`, account, prefix, upper)
		if err != nil {
			return empty, err
		}
		for rows.Next() {
			var absolute string
			var id, size, stamp int64
			var directory bool
			var sha string
			if err = rows.Scan(&absolute, &id, &directory, &size, &stamp, &sha); err != nil {
				rows.Close()
				return empty, err
			}
			at := time.UnixMicro(stamp).UTC()
			entry := directoryanalysis.Entry{Path: strings.TrimPrefix(absolute, prefix), ID: strconv.FormatInt(id, 10), Directory: directory, Size: strconv.FormatInt(size, 10), SHA256: sha, Observed: &at}
			input.Entries = append(input.Entries, entry)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return empty, err
		}
		result.Inputs = append(result.Inputs, input)
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return result, nil
}
