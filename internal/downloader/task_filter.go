package downloader

import (
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

// NormalizeTaskFilters validates API and WebSocket task list filters.
func NormalizeTaskFilters(status, query string) (string, string, error) {
	if status != "" {
		switch status {
		case "pending", "downloading", "paused", "retry_wait", "auth_wait", "source_unknown", "failed", "cancelled", "done":
		default:
			return "", "", errors.New("invalid task status filter")
		}
	}
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) > 128 {
		return "", "", errors.New("task search query is too long")
	}
	return status, strings.ToLower(query), nil
}

// status returns the current task status from live progress when present,
// falling back to the durable job projection for inactive tasks.
func (c taskCandidate) status() string {
	if c.live != nil {
		return c.live.Status
	}
	if c.job != nil {
		return c.job.Status
	}
	return ""
}

// GetTaskPageFiltered applies exact status and case-insensitive substring
// filters before selecting a bounded sorted page. QueueTotal and Counts always
// describe the unfiltered queue.
func (d *Downloader) GetTaskPageFiltered(offset, limit int, sortBy, status, query string) (TaskPage, error) {
	status, query, err := NormalizeTaskFilters(status, query)
	if err != nil {
		return TaskPage{}, err
	}
	if status == "" && query == "" {
		page := d.GetTaskPage(offset, limit, sortBy)
		page.QueueTotal = page.Total
		return page, nil
	}
	if offset < 0 {
		offset = 0
	}
	if limit < 0 {
		limit = 0
	}
	if sortBy != "progress" && sortBy != "size" {
		sortBy = "name"
	}
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	d.taskMutex.RLock()
	defer d.taskMutex.RUnlock()
	queueTotal, counts := d.taskStatsLocked()
	wanted := queueTotal
	if offset <= queueTotal && limit <= queueTotal-offset {
		wanted = offset + limit
	}
	selected := make([]taskCandidate, 0, wanted)
	matched := 0
	include := func(candidate taskCandidate) {
		if status != "" && candidate.status() != status {
			return
		}
		if query != "" {
			info := candidate.info()
			if !strings.Contains(strings.ToLower(info.Path), query) && !strings.Contains(strings.ToLower(info.ServerFilename), query) {
				return
			}
		}
		matched++
		if len(selected) < wanted {
			selected = append(selected, candidate)
			siftTaskCandidatesUp(selected, len(selected)-1, sortBy)
		} else if wanted > 0 && taskCandidateComesBefore(candidate, selected[0], sortBy) {
			selected[0] = candidate
			siftTaskCandidatesDown(selected, 0, sortBy)
		}
	}
	for id, job := range q.jobs {
		include(taskCandidate{job: job, live: d.tasks[id]})
	}
	for id, task := range d.tasks {
		if q.jobs[id] == nil {
			include(taskCandidate{live: task})
		}
	}
	sort.Slice(selected, func(i, j int) bool { return taskCandidateComesBefore(selected[i], selected[j], sortBy) })
	page := TaskPage{Offset: offset, Limit: limit, Total: matched, QueueTotal: queueTotal, Counts: counts}
	page.HasMore = offset < matched && limit < matched-offset
	if limit == 0 || offset >= len(selected) {
		return page, nil
	}
	end := len(selected)
	if limit < end-offset {
		end = offset + limit
	}
	page.Tasks = make([]*Task, 0, end-offset)
	for _, candidate := range selected[offset:end] {
		page.Tasks = append(page.Tasks, d.taskWithActionsLocked(candidate.info().FsID, candidate.taskView(d)))
	}
	return page, nil
}
