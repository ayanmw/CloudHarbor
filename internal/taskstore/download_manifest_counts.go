package taskstore

import (
	"context"
	"database/sql"
	"fmt"
)

const downloadFileStateCountsSQL = `SELECT CASE WHEN status='downloading' AND (run_id<>? OR NOT ?) THEN 'pending' ELSE status END AS resolved_status,count(*) FROM download_manifest_states WHERE manifest_id=? GROUP BY resolved_status`

// Completed results clear their state projection in the same transaction. Count
// only remaining observations, deriving untouched pending files from totals.
func downloadManifestStatusCounts(ctx context.Context, tx *sql.Tx, id, runID string, activeAllowed bool) (map[string]int, int, error) {
	var files, completed int
	if err := tx.QueryRowContext(ctx, `SELECT files,completed FROM download_manifest_totals WHERE manifest_id=?`, id).Scan(&files, &completed); err != nil {
		return nil, 0, err
	}
	counts := map[string]int{}
	if completed > 0 {
		counts["done"] = completed
	}
	rows, err := tx.QueryContext(ctx, downloadFileStateCountsSQL, runID, activeAllowed, id)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	observed, other := 0, 0
	for rows.Next() {
		var status string
		var count int
		if err = rows.Scan(&status, &count); err != nil {
			return nil, 0, err
		}
		observed += count
		switch status {
		case "pending": // Old-run/interrupted observations are part of pending below.
		case "downloading", "failed", "cancelled", "auth_wait":
			counts[status] = count
			other += count
		default:
			return nil, 0, fmt.Errorf("invalid unfinished download state")
		}
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	if observed > files-completed {
		return nil, 0, fmt.Errorf("download state projection contradicts file totals")
	}
	if pending := files - completed - other; pending > 0 {
		counts["pending"] = pending
	}
	return counts, files, nil
}
