package intakeapi

import (
	"strconv"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type cleanupStringGroup struct {
	SHA256 string                   `json:"sha256"`
	Size   string                   `json:"size"`
	Keep   cachedCloudStringEntry   `json:"keep"`
	Delete []cachedCloudStringEntry `json:"delete"`
}

func cleanupStringEntry(entry taskstore.CloudInventoryEntry) cachedCloudStringEntry {
	var size *string
	if entry.DirectorySizeBytes != nil {
		value := strconv.FormatInt(*entry.DirectorySizeBytes, 10)
		size = &value
	}
	return cachedCloudStringEntry{entry, strconv.FormatInt(entry.FsID, 10), strconv.FormatInt(entry.Size, 10), size, strconv.FormatInt(entry.ProviderMtime, 10)}
}
func cleanupGroupsResponse(groups []taskstore.CloudDuplicateGroup) []cleanupStringGroup {
	result := make([]cleanupStringGroup, 0, len(groups))
	for _, group := range groups {
		items := make([]cachedCloudStringEntry, 0, len(group.Delete))
		for _, entry := range group.Delete {
			items = append(items, cleanupStringEntry(entry))
		}
		result = append(result, cleanupStringGroup{group.SHA256, strconv.FormatInt(group.Size, 10), cleanupStringEntry(group.Keep), items})
	}
	return result
}

// The worker flag is a live observation, never inferred from a saved checkpoint.
func duplicateScanStringStatus(state taskstore.CloudDuplicateScanState, active bool) any {
	return map[string]any{
		"run_id": state.RunID, "status": state.Status, "phase": state.Phase, "worker_active": active,
		"next_fs_id": strconv.FormatInt(state.NextFsID, 10), "total_files": strconv.FormatInt(state.TotalFiles, 10), "processed_files": strconv.FormatInt(state.ProcessedFiles, 10),
		"total_bytes": strconv.FormatInt(state.TotalBytes, 10), "processed_bytes": strconv.FormatInt(state.ProcessedBytes, 10),
		"started_at_utc": state.StartedAtUTC, "updated_at_utc": state.UpdatedAtUTC, "last_error": state.LastError, "retry_at_utc": state.RetryAtUTC,
		"current_fs_id": strconv.FormatInt(state.CurrentFsID, 10), "current_path": state.CurrentPath, "current_name": state.CurrentName, "current_size": strconv.FormatInt(state.CurrentSize, 10), "current_bytes": strconv.FormatInt(state.CurrentBytes, 10),
	}
}
