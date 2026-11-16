package intakeapi

import (
	"strconv"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func historyInt(value int64) string { return strconv.FormatInt(value, 10) }
func historyNullableInt(value *int64) any {
	if value == nil {
		return nil
	}
	return historyInt(*value)
}
func historyReasons(value *taskstore.CloudInventoryCoverageReasons) any {
	if value == nil {
		return nil
	}
	return map[string]string{"scanning_directories": historyInt(value.ScanningDirectories), "stale_directories": historyInt(value.StaleDirectories), "unknown_directories": historyInt(value.UnknownDirectories), "failed_directories": historyInt(value.FailedDirectories), "missing_directory_status": historyInt(value.MissingDirectoryStatus), "orphan_directory_status": historyInt(value.OrphanDirectoryStatus), "parent_not_complete": historyInt(value.ParentNotComplete), "path_mismatch": historyInt(value.PathMismatch), "missing_root": historyInt(value.MissingRoot)}
}
func historyComparison(value *taskstore.CloudScanCoverageComparison) any {
	if value == nil {
		return nil
	}
	return map[string]any{"status": value.Status, "reason_status": value.ReasonStatus, "previous_run_id": value.PreviousRunID, "previous_sequence": historyInt(value.PreviousSequence), "previous_observed_at_utc": value.PreviousObservedAtUTC, "previous_coverage_status": value.PreviousCoverageStatus, "previous_incomplete_directories": historyNullableInt(value.PreviousIncompleteDirectories), "previous_reasons": historyReasons(value.PreviousReasons), "incomplete_directories_delta": historyNullableInt(value.IncompleteDirectoriesDelta), "reason_deltas": historyReasons(value.ReasonDeltas)}
}
func hashHistoryRuns(runs []taskstore.CloudDuplicateScanRun, next int64, more bool) any {
	rows := make([]map[string]any, 0, len(runs))
	for _, v := range runs {
		rows = append(rows, map[string]any{"run_id": v.RunID, "sequence": historyInt(v.StartSequence), "latest_sequence": historyInt(v.LatestSequence), "status": v.Status, "phase": v.Phase, "next_fs_id": historyInt(v.NextFsID), "total_files": historyInt(v.TotalFiles), "processed_files": historyInt(v.ProcessedFiles), "total_bytes": historyInt(v.TotalBytes), "processed_bytes": historyInt(v.ProcessedBytes), "started_at_utc": v.StartedAtUTC, "updated_at_utc": v.UpdatedAtUTC, "last_error": v.LastError})
	}
	return map[string]any{"rows": rows, "next_before_sequence": historyInt(next), "has_more": more}
}

// The caller masks evidence first. Send it as JSON text so browser parsing does
// not round nested provider IDs or counters beyond Number's exact integer range.
func hashHistoryEvents(events []taskstore.CloudDuplicateScanEvent, run string, next int64, more bool) any {
	rows := make([]map[string]any, 0, len(events))
	for _, v := range events {
		rows = append(rows, map[string]any{"run_id": v.RunID, "sequence": historyInt(v.Sequence), "status": v.Status, "phase": v.Phase, "recorded_at_utc": v.RecordedAtUTC, "evidence_json": string(v.Evidence)})
	}
	return map[string]any{"run_id": run, "rows": rows, "next_before_sequence": historyInt(next), "has_more": more}
}
