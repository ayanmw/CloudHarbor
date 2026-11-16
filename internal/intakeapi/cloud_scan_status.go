package intakeapi

import (
	"strconv"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func cloudScanStatusResponse(state taskstore.CloudScanState, exact bool) any {
	if !exact {
		return state
	}
	out := map[string]any{"run_id": state.RunID, "status": state.Status, "worker_active": state.WorkerActive, "phase": state.Phase, "current_directory": state.CurrentDirectory, "started_at_utc": state.StartedAtUTC, "updated_at_utc": state.UpdatedAtUTC, "last_error": state.LastError, "retry_at_utc": state.RetryAtUTC}
	counts := map[string]int64{"pending_count": state.PendingCount, "next_offset": state.NextOffset, "pages_processed": state.PagesProcessed, "items_processed": state.ItemsProcessed, "request_budget": state.RequestBudget, "requests_used": state.RequestsUsed, "boundary_restart_count": state.BoundaryRestartCount, "retry_delay_millis": state.RetryDelayMillis}
	for key, value := range counts {
		out[key] = strconv.FormatInt(value, 10)
	}
	if coverage := state.InventoryCoverage; coverage != nil {
		out["inventory_coverage"] = map[string]any{"status": coverage.Status, "incomplete_directories": strconv.FormatInt(coverage.IncompleteDirectories, 10), "cache_generation": strconv.FormatInt(coverage.Generation, 10), "scan_updated_at_utc": coverage.ScanUpdatedAtUTC}
	}
	return out
}
func cloudRefreshStatusResponse(state taskstore.CloudInventoryRefreshState, exact bool) any {
	if !exact {
		return state
	}
	out := cloudScanStatusResponse(taskstore.CloudScanState{RunID: state.RunID, Status: state.Status, WorkerActive: state.WorkerActive, Phase: state.Phase, CurrentDirectory: state.CurrentDirectory, StartedAtUTC: state.StartedAtUTC, UpdatedAtUTC: state.UpdatedAtUTC, LastError: state.LastError, RetryAtUTC: state.RetryAtUTC, PendingCount: state.PendingCount, NextOffset: state.NextOffset, PagesProcessed: state.PagesProcessed, ItemsProcessed: state.ItemsProcessed, RequestBudget: state.RequestBudget, RequestsUsed: state.RequestsUsed, BoundaryRestartCount: state.BoundaryRestartCount, RetryDelayMillis: state.RetryDelayMillis}, true).(map[string]any)
	out["root_path"] = state.RootPath
	return out
}
