package intakeapi

import "web_browser_baidu_onekey_upload/internal/taskstore"

// Preserve metadata and nullable observation times, while opting browser clients
// into decimal strings for every integer. Legacy JSON and evidence exports stay unchanged.
func refreshChangeItemExact(item *taskstore.CloudInventoryRefreshChangeItem) any {
	if item == nil {
		return nil
	}
	return struct {
		*taskstore.CloudInventoryRefreshChangeItem
		FsID          string `json:"fs_id"`
		Size          string `json:"size"`
		ProviderMtime string `json:"provider_mtime"`
	}{item, historyInt(item.FsID), historyInt(item.Size), historyInt(item.ProviderMtime)}
}
func refreshChangesExact(run string, event int64, kind, query string, page taskstore.CloudInventoryRefreshChangePage) map[string]any {
	rows := make([]map[string]any, 0, len(page.Changes))
	for _, row := range page.Changes {
		rows = append(rows, map[string]any{
			"sequence": historyInt(row.Sequence), "ordinal": historyInt(row.Ordinal), "kind": row.Kind,
			"old": refreshChangeItemExact(row.Old), "new": refreshChangeItemExact(row.New),
			"recorded_at_utc": row.RecordedAtUTC, "cloud_observed_at_utc": row.CloudObservedAtUTC,
		})
	}
	counts := map[string]string{"total": historyInt(page.Counts.Total), "added": historyInt(page.Counts.Added), "removed": historyInt(page.Counts.Removed), "metadata_changed": historyInt(page.Counts.MetadataChanged), "identity_changed": historyInt(page.Counts.IdentityChanged)}
	return map[string]any{"run_id": run, "event_sequence": historyInt(event), "kind": kind, "query": query, "changes": rows, "counts": counts, "total_count": historyInt(page.Counts.Total), "matched_count": historyInt(page.MatchedCount), "next_before_sequence": historyInt(page.NextBeforeSequence), "has_more": page.HasMore}
}
