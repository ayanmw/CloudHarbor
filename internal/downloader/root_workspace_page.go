package downloader

import (
	"container/heap"
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
)

var ErrTaskWorkspaceRootNotFound = errors.New("download root not found")

func WorkspaceStatusPriority(status string) int {
	switch status {
	case "downloading":
		return 0
	case "failed", "source_unknown", "auth_wait":
		return 1
	case "pending", "retry_wait":
		return 2
	case "paused":
		return 3
	case "cancelled":
		return 4
	case "done":
		return 5
	default:
		return 6
	}
}

func rootWorkspaceBefore(a, b taskCandidate) bool {
	ap, bp := WorkspaceStatusPriority(a.status()), WorkspaceStatusPriority(b.status())
	if ap != bp {
		return ap < bp
	}
	ai, bi := a.info(), b.info()
	if ai.Path != bi.Path {
		return ai.Path < bi.Path
	}
	return strconv.FormatInt(ai.FsID, 10) < strconv.FormatInt(bi.FsID, 10)
}

// Keep references to the smallest offset+limit candidates, with the worst
// selected item at the root. Only the final page is copied into display Tasks.
type rootWorkspaceHeap []taskCandidate

func (h rootWorkspaceHeap) Len() int           { return len(h) }
func (h rootWorkspaceHeap) Less(i, j int) bool { return rootWorkspaceBefore(h[j], h[i]) }
func (h rootWorkspaceHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *rootWorkspaceHeap) Push(value any)    { *h = append(*h, value.(taskCandidate)) }
func (h *rootWorkspaceHeap) Pop() any {
	old := *h
	v := old[len(old)-1]
	*h = old[:len(old)-1]
	return v
}

func (d *Downloader) GetRootWorkspacePage(offset, limit int, status, query string) (TaskPage, error) {
	status, query, err := NormalizeTaskFilters(status, query)
	if err != nil {
		return TaskPage{}, err
	}
	if offset < 0 || limit < 0 || limit > 200 {
		return TaskPage{}, errors.New("invalid root page")
	}
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	d.taskMutex.RLock()
	defer d.taskMutex.RUnlock()
	total := len(q.jobs)
	wanted := 0
	if offset < total && limit > 0 {
		wanted = total
		if limit <= total-offset {
			wanted = offset + limit
		}
	}
	selected := rootWorkspaceHeap{}
	page := TaskPage{Offset: offset, Limit: limit, QueueTotal: total, Counts: map[string]int{}}
	for id, job := range q.jobs {
		candidate := taskCandidate{job: job, live: d.tasks[id]}
		st := candidate.status()
		page.Counts[st]++
		if status != "" && st != status {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(candidate.info().Path), query) {
			continue
		}
		page.Total++
		if wanted == 0 {
			continue
		}
		if len(selected) < wanted {
			heap.Push(&selected, candidate)
		} else if rootWorkspaceBefore(candidate, selected[0]) {
			selected[0] = candidate
			heap.Fix(&selected, 0)
		}
	}
	if page.Offset > page.Total {
		page.Offset = page.Total
	}
	page.HasMore = page.Offset < page.Total && limit < page.Total-page.Offset
	if limit == 0 || offset >= page.Total {
		return page, nil
	}
	sort.Slice(selected, func(i, j int) bool { return rootWorkspaceBefore(selected[i], selected[j]) })
	end := len(selected)
	if limit < end-offset {
		end = offset + limit
	}
	page.Tasks = make([]*Task, 0, end-offset)
	for _, candidate := range selected[offset:end] {
		task := candidate.taskView(d)
		task.Root = true
		page.Tasks = append(page.Tasks, d.taskWithActionsLocked(task.FileInfo.FsID, task))
	}
	return page, nil
}

func (d *Downloader) RootWorkspaceCount() int {
	d.queue.mu.Lock()
	defer d.queue.mu.Unlock()
	return len(d.queue.jobs)
}

func (d *Downloader) GetTaskWorkspaceRoot(ctx context.Context, id int64) (*Task, error) {
	q := d.queue
	q.mu.Lock()
	job := q.jobs[id]
	if job == nil {
		q.mu.Unlock()
		return nil, ErrTaskWorkspaceRootNotFound
	}
	d.taskMutex.RLock()
	task := d.taskViewLocked(id, job)
	task.Root = true
	task = d.taskWithActionsLocked(id, task)
	d.taskMutex.RUnlock()
	q.mu.Unlock()
	if err := d.EnrichRootWorkspacePage(ctx, []*Task{task}); err != nil {
		return nil, err
	}
	return task, nil
}

// Capture active progress only for the selected root snapshots. Another attempt
// may start after page selection; never attach its progress to the previous one.
func (d *Downloader) EnrichRootWorkspacePage(ctx context.Context, roots []*Task) error {
	q := d.queue
	q.mu.Lock()
	d.taskMutex.RLock()
	tasks := append([]*Task{}, roots...)
	for id, live := range d.tasks {
		if q.jobs[id] != nil || live.FileInfo.IsDir != 0 || live.Status != "downloading" {
			continue
		}
		for _, root := range roots {
			job := q.jobs[root.FileInfo.FsID]
			if job != nil && job.AttemptID == root.AttemptID && job.Info == root.FileInfo && strings.HasPrefix(live.FileInfo.Path, strings.TrimSuffix(root.FileInfo.Path, "/")+"/") {
				tasks = append(tasks, d.taskViewLocked(id, nil))
				break
			}
		}
	}
	d.taskMutex.RUnlock()
	q.mu.Unlock()
	return d.enrichTaskWorkspaceHistory(ctx, tasks)
}
