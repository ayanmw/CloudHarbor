package main

// Durable counts describe the sealed observation. Only sampled, uncommitted
// active transfers contribute additional progress; source status stays separate.
func applyRootDownloadHistory(row *workspaceTask) {
	h := row.DownloadHistory
	if h == nil {
		return
	}
	if h.Source == "previous_attempt" {
		// Old in-memory descendants may outlive a retry. Until the new manifest is
		// sealed, expose old totals only inside the explicitly historical evidence.
		row.Files = 0
		row.Folders = 0
		row.Size = 0
		row.Completed = 0
		row.Progress = 0
		row.Speed = 0
		row.Active = 0
		row.Prepared = false
		return
	}
	if h.Source != "current_attempt" || h.Summary == nil {
		return
	}
	s := h.Summary
	row.Files = s.Files
	row.Folders = s.Folders
	row.Size = s.Size
	row.Completed = s.Completed
	row.Progress = s.CompletedBytes + h.LiveProgress
	if row.Progress > row.Size {
		row.Progress = row.Size
	}
	row.Active = h.LiveActive
	row.Speed = h.LiveSpeed
	row.Prepared = true
}
