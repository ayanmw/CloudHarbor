package intakeapi

import (
	"strconv"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

// The new workbench exposes only the fields it renders, with exact counters.
// Legacy responses and evidence downloads retain their original complete schema.
func scanEvidenceRow(runID, status, phase, directory string, pages, items, pending int64) map[string]any {
	return map[string]any{"run_id": runID, "status": status, "phase": phase, "current_directory": directory, "pages_processed": strconv.FormatInt(pages, 10), "items_processed": strconv.FormatInt(items, 10), "pending_count": strconv.FormatInt(pending, 10)}
}
func scanEventRow(runID, kind, status, phase, directory string, sequence, pages, items, pending int64, recorded time.Time) map[string]any {
	row := scanEvidenceRow(runID, status, phase, directory, pages, items, pending)
	row["kind"] = kind
	row["sequence"] = strconv.FormatInt(sequence, 10)
	row["recorded_at_utc"] = recorded
	return row
}
func scanEvidencePage(rows any, next int64, more bool) map[string]any {
	result := []map[string]any{}
	switch values := rows.(type) {
	case []taskstore.CloudScanRun:
		for _, v := range values {
			r := scanEvidenceRow(v.RunID, v.Status, v.Phase, v.CurrentDirectory, v.PagesProcessed, v.ItemsProcessed, v.PendingCount)
			r["sequence"] = strconv.FormatInt(v.Sequence, 10)
			r["started_at_utc"] = v.StartedAtUTC
			r["updated_at_utc"] = v.UpdatedAtUTC
			r["request_budget"], r["requests_used"], r["last_error"] = historyInt(v.RequestBudget), historyInt(v.RequestsUsed), v.LastError
			r["coverage_status"] = v.CoverageStatus
			r["coverage_incomplete_directories"], r["coverage_generation"] = historyNullableInt(v.CoverageIncompleteDirectories), historyNullableInt(v.CoverageGeneration)
			r["coverage_observed_at_utc"], r["coverage_reasons"], r["coverage_comparison"] = v.CoverageObservedAtUTC, historyReasons(v.CoverageReasons), historyComparison(v.CoverageComparison)
			result = append(result, r)
		}
	case []taskstore.CloudInventoryRefreshRun:
		for _, v := range values {
			r := scanEvidenceRow(v.RunID, v.Status, v.Phase, v.CurrentDirectory, v.PagesProcessed, v.ItemsProcessed, v.PendingCount)
			r["sequence"] = strconv.FormatInt(v.StartSequence, 10)
			r["root_path"] = v.RootPath
			r["started_at_utc"] = v.StartedAtUTC
			r["updated_at_utc"] = v.LastRecordedAtUTC
			r["request_budget"], r["requests_used"], r["last_error"] = historyInt(v.RequestBudget), historyInt(v.RequestsUsed), v.LastError
			r["latest_kind"], r["latest_sequence"] = v.LatestKind, historyInt(v.LatestSequence)
			result = append(result, r)
		}
	case []taskstore.CloudScanEvent:
		for _, v := range values {
			r := scanEventRow(v.RunID, v.Kind, v.Status, v.Phase, v.CurrentDirectory, v.Sequence, v.PagesProcessed, v.ItemsProcessed, v.PendingCount, v.RecordedAtUTC)
			r["request_budget"], r["requests_used"], r["last_error"], r["restart_reason"] = historyInt(v.RequestBudget), historyInt(v.RequestsUsed), v.LastError, v.RestartReason
			result = append(result, r)
		}
	case []taskstore.CloudInventoryRefreshEvent:
		for _, v := range values {
			r := scanEventRow(v.RunID, v.Kind, v.Status, v.Phase, v.CurrentDirectory, v.Sequence, v.PagesProcessed, v.ItemsProcessed, v.PendingCount, v.RecordedAtUTC)
			r["request_budget"], r["requests_used"], r["request_budget_added"] = historyInt(v.RequestBudget), historyInt(v.RequestsUsed), historyInt(v.RequestBudgetAdded)
			r["retry_at_utc"], r["last_error"], r["restart_reason"] = v.RetryAtUTC, v.LastError, v.RestartReason
			r["restart_offset"], r["restart_fs_id"] = historyNullableInt(v.RestartOffset), historyNullableInt(v.RestartFSID)
			r["cloud_observed_at_utc"], r["directory_changed"] = v.CloudObservedAtUTC, v.DirectoryChanged
			r["added_count"], r["removed_count"], r["changed_count"] = historyNullableInt(v.AddedCount), historyNullableInt(v.RemovedCount), historyNullableInt(v.ChangedCount)
			result = append(result, r)
		}
	}
	return map[string]any{"rows": result, "next_before_sequence": strconv.FormatInt(next, 10), "has_more": more}
}
