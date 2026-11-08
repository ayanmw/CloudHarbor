package uploads

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
)

type uploadSessionDetails struct {
	BatchID                 string `json:"batch_id"`
	ItemID                  string `json:"item_id"`
	Path                    string `json:"path"`
	State                   string `json:"state"`
	Status                  string `json:"status"`
	AttemptID               string `json:"attempt_id"`
	PreviousAttemptID       string `json:"previous_attempt_id"`
	SourceAttemptID         string `json:"source_attempt_id"`
	Active                  bool   `json:"active"`
	NeededParts             int    `json:"needed_parts"`
	ConfirmedParts          int    `json:"confirmed_parts"`
	PendingParts            int    `json:"pending_parts"`
	NeverAdmittedParts      int    `json:"never_admitted_parts"`
	Sequence                string `json:"evidence_sequence"`
	EvidenceUTC             string `json:"evidence_recorded_at_utc"`
	ObservedUTC             string `json:"observed_at_utc"`
	ReceiptPath             string `json:"receipt_path"`
	ReceiptFsID             string `json:"receipt_fs_id"`
	VerificationStatus      string `json:"verification_status"`
	VerificationRecordedUTC string `json:"verification_recorded_at_utc"`
	VerificationObservedUTC string `json:"verification_observed_at_utc"`
}

func (m *Manager) sessionDetails(batch, item string) (uploadSessionDetails, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := uploadSessionDetails{BatchID: batch, ItemID: item, Status: "no_evidence", Sequence: "0", ObservedUTC: now(), VerificationStatus: "none"}
	if err := m.db.QueryRow(`SELECT relative_path,state FROM items WHERE batch=? AND id=?`, batch, item).Scan(&result.Path, &result.State); err != nil {
		return result, err
	}
	latest, err := m.latestUploadAttempt(item)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.AttemptID = latest.AttemptID
	result.PreviousAttemptID = latest.PreviousAttemptID
	active := m.activeAttempts[item]
	result.Active = active.ID == latest.AttemptID && !active.RunnerReturned && m.ctx.Err() == nil
	journal, err := m.readSessionJournal(item, latest.AttemptID)
	if errors.Is(err, sql.ErrNoRows) {
		pre, preErr := m.readPrecreateJournal(item, latest.AttemptID)
		if errors.Is(preErr, sql.ErrNoRows) {
			return result, nil
		}
		if preErr != nil {
			return result, preErr
		}
		result.Status = "precreate_unconfirmed"
		result.Sequence = strconv.FormatInt(pre.LastSequence, 10)
		if pre.RapidReceipt != nil {
			result.Status = "rapid_receipt"
			result.ReceiptPath = pre.RapidReceipt.Receipt.Path
			result.ReceiptFsID = pre.RapidReceipt.Receipt.FsID
		}
	} else {
		if err != nil {
			return result, err
		}
		result.Status = "session_ready"
		result.NeededParts = len(journal.Plan.NeededParts)
		result.ConfirmedParts = len(journal.ConfirmedParts)
		result.PendingParts = len(journal.PendingParts)
		result.NeverAdmittedParts = len(journal.NeverAdmittedParts)
		result.Sequence = strconv.FormatInt(journal.LastSequence, 10)
		if result.PendingParts > 0 {
			result.Status = "parts_unconfirmed"
		}
		if journal.CreateAdmission != nil {
			result.Status = "create_unconfirmed"
		}
		if journal.CreateReceipt != nil {
			result.Status = "create_receipt"
			result.ReceiptPath = journal.CreateReceipt.Receipt.Path
			result.ReceiptFsID = journal.CreateReceipt.Receipt.FsID
		}
		bridge, _, bridgeErr := m.readResumeBridge(item, latest.AttemptID)
		if bridgeErr == nil {
			result.SourceAttemptID = bridge.Admission.SourceAttemptID
		} else if !errors.Is(bridgeErr, sql.ErrNoRows) {
			return result, bridgeErr
		}
	}
	err = m.db.QueryRow(`SELECT recorded FROM events WHERE item=? AND sequence=?`, item, result.Sequence).Scan(&result.EvidenceUTC)
	if err == nil {
		result.VerificationStatus, result.VerificationRecordedUTC, result.VerificationObservedUTC, err = m.receiptVerificationSummary(item, latest.AttemptID)
	}
	return result, err
}
func (m *Manager) HandleSessionDetails(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(405)
		return
	}
	batch, item := r.PathValue("batch"), r.PathValue("item")
	if !validID(batch) || !validID(item) || r.URL.RawQuery != "" {
		http.Error(w, "上传会话详情请求无效", 400)
		return
	}
	result, err := m.sessionDetails(batch, item)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "上传会话证据不可读，请刷新核对", 500)
		}
		return
	}
	write(w, result)
}
