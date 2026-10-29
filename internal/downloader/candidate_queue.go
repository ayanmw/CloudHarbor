package downloader

import (
	"container/heap"
	"time"
)

type candidateEntry struct {
	job     *queueJob
	delayed bool
	index   int
}

type candidateHeap []*candidateEntry

func (h candidateHeap) Len() int { return len(h) }
func (h candidateHeap) Less(i, j int) bool {
	a, b := h[i].job, h[j].job
	if h[i].delayed && !a.NextAttempt.Equal(b.NextAttempt) {
		return a.NextAttempt.Before(b.NextAttempt)
	}
	if !a.Created.Equal(b.Created) {
		return a.Created.Before(b.Created)
	}
	return a.Info.FsID < b.Info.FsID
}
func (h candidateHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *candidateHeap) Push(value any) {
	entry := value.(*candidateEntry)
	entry.index = len(*h)
	*h = append(*h, entry)
}
func (h *candidateHeap) Pop() any {
	old := *h
	last := len(old) - 1
	entry := old[last]
	old[last] = nil
	entry.index = -1
	*h = old[:last]
	return entry
}

// The caller holds q.mu for every candidate-index operation.
func (q *scheduler) clearCandidates() {
	for _, entry := range q.ready {
		entry.job.candidate = nil
	}
	for _, entry := range q.delayed {
		entry.job.candidate = nil
	}
	q.ready = nil
	q.delayed = nil
}

func (q *scheduler) removeCandidate(job *queueJob) {
	entry := job.candidate
	if entry == nil {
		return
	}
	job.candidate = nil
	if entry.delayed {
		heap.Remove(&q.delayed, entry.index)
	} else {
		heap.Remove(&q.ready, entry.index)
	}
}

func (q *scheduler) candidateEligible(job *queueJob) bool {
	id := job.Info.FsID
	eligibleInfo := q.eligible[id]
	return !q.isRemoteBlocked(job.Info.FsID, job.Info.Path) && !q.blockedIntakes[job.Info.Path] && !q.blockedSources[job.Info.Path] &&
		(job.Status == "pending" || job.Status == "retry_wait") && q.active[id] == nil &&
		eligibleInfo != nil && *eligibleInfo == job.Info
}

func (q *scheduler) enqueueCandidate(job *queueJob) {
	q.enqueueCandidateAt(job, time.Now())
}

func (q *scheduler) enqueueCandidateAt(job *queueJob, now time.Time) {
	q.removeCandidate(job)
	if !q.candidateEligible(job) {
		return
	}
	entry := &candidateEntry{job: job}
	if now.Before(job.NextAttempt) {
		entry.delayed = true
		heap.Push(&q.delayed, entry)
	} else {
		heap.Push(&q.ready, entry)
	}
	job.candidate = entry
}

func (q *scheduler) rebuildCandidates() {
	q.clearCandidates()
	for _, job := range q.jobs {
		q.enqueueCandidate(job)
	}
}

func (q *scheduler) promoteDueCandidates(now time.Time) {
	for len(q.delayed) > 0 && !now.Before(q.delayed[0].job.NextAttempt) {
		entry := heap.Pop(&q.delayed).(*candidateEntry)
		entry.job.candidate = nil
		q.enqueueCandidateAt(entry.job, now)
	}
}

func (q *scheduler) popReadyCandidate(now time.Time) *queueJob {
	q.promoteDueCandidates(now)
	for len(q.ready) > 0 {
		entry := heap.Pop(&q.ready).(*candidateEntry)
		job := entry.job
		job.candidate = nil
		if q.candidateEligible(job) && !now.Before(job.NextAttempt) {
			return job
		}
	}
	return nil
}
