package uploads

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type RecoveryEvent struct {
	Sequence             string `json:"sequence"`
	ItemID               string `json:"item_id"`
	Path                 string `json:"path"`
	PreviousState        string `json:"previous_state"`
	PreviousUpdatedAtUTC string `json:"previous_updated_at_utc"`
	State                string `json:"state"`
	ConfirmedPartBytes   string `json:"confirmed_part_bytes"`
	RecordedAtUTC        string `json:"recorded_at_utc"`
}

func parseRecoveryEvent(event *RecoveryEvent, message string) error {
	parts := strings.Split(message, "; ")
	if len(parts) != 3 || !strings.HasPrefix(parts[0], "restart_recovery: previous_state=") || !strings.HasPrefix(parts[1], "previous_updated_at_utc=") {
		return fmt.Errorf("invalid recovery evidence")
	}
	event.PreviousState = strings.TrimPrefix(parts[0], "restart_recovery: previous_state=")
	event.PreviousUpdatedAtUTC = strings.TrimPrefix(parts[1], "previous_updated_at_utc=")
	expected, action := "queued", "action=requeued_without_confirmed_part_resume"
	switch event.PreviousState {
	case "hashing", "uploading":
		if event.State == "paused" {
			expected = "paused"
			action = "action=paused_after_restart_request"
		}
	case "submitting", "finalizing":
		expected = "unknown"
		action = "action=isolated_unconfirmed_remote_submit"
	default:
		return fmt.Errorf("invalid recovery stage")
	}
	if event.State != expected || parts[2] != action {
		return fmt.Errorf("invalid recovery outcome")
	}
	for _, value := range []string{event.PreviousUpdatedAtUTC, event.RecordedAtUTC} {
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			return fmt.Errorf("invalid recovery time")
		}
	}
	return nil
}

func (m *Manager) recoveryEvents(batch string, before int64, limit int) ([]RecoveryEvent, *string, error) {
	var exists int
	if err := m.db.QueryRow(`SELECT 1 FROM batches WHERE id=?`, batch).Scan(&exists); err != nil {
		return nil, nil, err
	}
	// Explicit history reads use the sequence primary key; never run this scan
	// from the automatic two-second upload progress poll.
	rows, err := m.db.Query(`SELECT CAST(e.sequence AS TEXT),e.item,i.relative_path,e.state,CAST(e.sent AS TEXT),e.recorded,e.error FROM events e JOIN items i ON i.id=e.item WHERE i.batch=? AND substr(e.error,1,17)='restart_recovery:' AND (?=0 OR e.sequence<?) ORDER BY e.sequence DESC LIMIT ?`, batch, before, before, limit+1)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	result := []RecoveryEvent{}
	for rows.Next() {
		var event RecoveryEvent
		var message string
		if err = rows.Scan(&event.Sequence, &event.ItemID, &event.Path, &event.State, &event.ConfirmedPartBytes, &event.RecordedAtUTC, &message); err != nil {
			return nil, nil, err
		}
		if err = parseRecoveryEvent(&event, message); err != nil {
			return nil, nil, err
		}
		result = append(result, event)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(result) > limit {
		next := result[limit-1].Sequence
		return result[:limit], &next, nil
	}
	return result, nil, nil
}

func (m *Manager) HandleRecovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	batch := r.PathValue("batch")
	if !validID(batch) {
		http.Error(w, "批次ID无效", 400)
		return
	}
	before, limit := int64(0), 50
	query := r.URL.Query()
	for key, values := range query {
		if (key != "before_sequence" && key != "limit") || len(values) != 1 {
			http.Error(w, "恢复历史分页条件无效", 400)
			return
		}
	}
	if values, ok := query["before_sequence"]; ok {
		n, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || n <= 0 || strconv.FormatInt(n, 10) != values[0] {
			http.Error(w, "恢复历史游标无效", 400)
			return
		}
		before = n
	}
	if values, ok := query["limit"]; ok {
		n, err := strconv.Atoi(values[0])
		if err != nil || n < 1 || n > 100 || strconv.Itoa(n) != values[0] {
			http.Error(w, "恢复历史页大小无效", 400)
			return
		}
		limit = n
	}
	records, next, err := m.recoveryEvents(batch, before, limit)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "上传恢复证据不可读", 500)
		}
		return
	}
	write(w, map[string]any{"batch_id": batch, "records": records, "next_before_sequence": next})
}
