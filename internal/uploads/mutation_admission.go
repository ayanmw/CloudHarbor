package uploads

// A critical evidence write failure stops new requests before mu is released.
// Existing receipt observations remain permitted so in-flight facts are kept.
func (m *Manager) evidencePersistenceFailureLocked(err error) error {
	if err != nil && m.admissionError == "" {
		m.admissionError = err.Error()
	}
	return err
}

// Caller holds mu through both this decision and the durable admission event.
// Reply observations do not use this gate: they remain evidence even after a
// peer becomes unknown or the manager starts shutting down.
func (m *Manager) checkMutationAdmissionLocked(item string) error {
	if err := m.ctx.Err(); err != nil {
		return err
	}
	if m.admissionError != "" {
		return errUploadBatchBlocked
	}
	if m.isPauseRequested(item) {
		return errUploadPausedAtCheckpoint
	}
	var batch string
	if err := m.db.QueryRow(`SELECT batch FROM items WHERE id=?`, item).Scan(&batch); err != nil {
		return err
	}
	blocked, err := m.batchBlockedLocked(batch)
	if err != nil {
		return err
	}
	if blocked {
		return errUploadBatchBlocked
	}
	return nil
}
