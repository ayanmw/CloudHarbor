package main

import (
	"strconv"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

// Opt-in browser representation preserves int64 identities without changing
// the numeric response consumed by older clients. Evidence is masked upstream.
type taskEventStringDTO struct {
	taskstore.Event
	Sequence string `json:"sequence"`
	JobID    string `json:"job_id"`
}

func taskEventStringPage(events []taskstore.Event, total, nextBefore int64, more bool, counts []taskstore.TaskEventKindCount) any {
	rows := make([]taskEventStringDTO, 0, len(events))
	for _, event := range events {
		rows = append(rows, taskEventStringDTO{Event: event, Sequence: strconv.FormatInt(event.Sequence, 10), JobID: strconv.FormatInt(event.JobID, 10)})
	}
	var next *string
	if more {
		value := strconv.FormatInt(nextBefore, 10)
		next = &value
	}
	return map[string]any{"events": rows, "total_matches": total, "next_before_sequence": next, "kind_counts": counts}
}
