package uploads

// DirectorySummary keeps namespace work out of file byte and completion counts.
type DirectorySummary struct {
	Total     int `json:"total"`
	Queued    int `json:"queued"`
	Sending   int `json:"sending"`
	Confirmed int `json:"confirmed"`
	Existing  int `json:"existing"`
	Failed    int `json:"failed"`
	Unknown   int `json:"unknown"`
	Cancelled int `json:"cancelled"`
}

func (s *DirectorySummary) add(state string, n int) {
	s.Total += n
	switch state {
	case "queued":
		s.Queued += n
	case "sending":
		s.Sending += n
	case "confirmed":
		s.Confirmed += n
	case "existing":
		s.Existing += n
	case "failed":
		s.Failed += n
	case "unknown":
		s.Unknown += n
	case "cancelled":
		s.Cancelled += n
	}
}

func summarizeDirectories(directories []Directory) *DirectorySummary {
	if len(directories) == 0 {
		return nil
	}
	s := &DirectorySummary{}
	for _, d := range directories {
		s.add(d.State, 1)
	}
	return s
}

func (m *Manager) directorySummary(batch string) (*DirectorySummary, error) {
	rows, err := m.db.Query(`SELECT state,COUNT(*) FROM upload_directories WHERE batch=? GROUP BY state`, batch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	s := &DirectorySummary{}
	for rows.Next() {
		var state string
		var count int
		if err = rows.Scan(&state, &count); err != nil {
			return nil, err
		}
		s.add(state, count)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if s.Total == 0 {
		return nil, nil
	}
	return s, nil
}

// Caller holds mu, so a queued directory cannot be claimed between this
// cancellation and its evidence. Already-sent/unknown work is never cancelled.
func (m *Manager) cancelPendingBatch(batch string) error {
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := now()
	if _, err = tx.Exec(`INSERT INTO upload_directory_events(directory,state,recorded,evidence)
 SELECT id,'cancelled',?,'user_cancel: previous_state='||state||'; previous_updated_at_utc='||updated FROM upload_directories WHERE batch=? AND state='queued'`, at, batch); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE upload_directories SET state='cancelled',updated=? WHERE batch=? AND state='queued'`, at, batch); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE items SET state='cancelled',updated=? WHERE batch=? AND state IN ('receiving','queued','paused')`, at, batch); err != nil {
		return err
	}
	return tx.Commit()
}

func (m *Manager) retryFailedDirectories(batch string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := now()
	if _, err = tx.Exec(`INSERT INTO upload_directory_events(directory,state,recorded,evidence)
 SELECT id,'queued',?,'user_retry: previous_state=failed; previous_updated_at_utc='||updated||'; previous_operation_id='||operation_id FROM upload_directories WHERE batch=? AND state='failed'`, at, batch); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE upload_directories SET state='queued',updated=?,error='',actual_path='',operation_id='' WHERE batch=? AND state='failed'`, at, batch); err != nil {
		return err
	}
	return tx.Commit()
}
