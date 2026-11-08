package downloader

// Called under q.mu. Mutations must respect all durable uncertainty journals,
// including an intake's original identity and a download's pending postprocess.
// Keep scheduler eligibility's existing projections separate from this check.
func (q *scheduler) isCloudMutationBlocked(id int64, cloudPath string) bool {
	if q.isRemoteBlocked(id, cloudPath) || q.blockedIntakeIDs[id] {
		return true
	}
	if job := q.jobs[id]; job != nil && (job.SourceState == "sending" || job.SourceState == "unknown") {
		return true
	}
	for _, paths := range []map[string]bool{q.blockedSources, q.blockedIntakes, q.blockedIntakeSources} {
		for blocked := range paths {
			if blocked != "" && insideCloud(cloudPath, blocked) {
				return true
			}
		}
	}
	return false
}
