package uploads

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

type ReceiptVerifier func(context.Context, baiduapi.UploadAccountScope, baiduapi.UploadReceipt) (baiduapi.UploadReceiptVerification, error)
type receiptCandidate struct {
	AttemptID string                      `json:"attempt_id"`
	Sequence  int64                       `json:"source_sequence,string"`
	Scope     baiduapi.UploadAccountScope `json:"original_scope"`
	Receipt   baiduapi.UploadReceipt      `json:"receipt"`
}
type receiptVerificationEvidence struct {
	RequestID    string                              `json:"request_id"`
	Candidate    receiptCandidate                    `json:"candidate"`
	StartedAtUTC string                              `json:"started_at_utc"`
	Result       *baiduapi.UploadReceiptVerification `json:"result,omitempty"`
}

func (m *Manager) SetReceiptVerifier(verify ReceiptVerifier) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.verifyReceipt = verify
}
func (m *Manager) receiptCandidate(item string) (receiptCandidate, error) {
	var candidate receiptCandidate
	latest, err := m.latestUploadAttempt(item)
	if err != nil {
		return candidate, err
	}
	candidate.AttemptID = latest.AttemptID
	journal, err := m.readSessionJournal(item, latest.AttemptID)
	if errors.Is(err, sql.ErrNoRows) {
		pre, err := m.readPrecreateJournal(item, latest.AttemptID)
		if err != nil {
			return candidate, err
		}
		if pre.RapidReceipt == nil || pre.Intent.AccountScope == nil {
			return candidate, baiduapi.ErrUploadUnconfirmed
		}
		candidate.Sequence = pre.LastSequence
		candidate.Receipt = pre.RapidReceipt.Receipt
		candidate.Scope = *pre.Intent.AccountScope
	} else {
		if err != nil {
			return candidate, err
		}
		if journal.CreateReceipt == nil || journal.Plan.AccountScope == nil {
			return candidate, baiduapi.ErrUploadUnconfirmed
		}
		candidate.Sequence = journal.LastSequence
		candidate.Receipt = journal.CreateReceipt.Receipt
		candidate.Scope = *journal.Plan.AccountScope
	}
	if !candidate.Scope.Valid() || !candidate.Receipt.Valid() {
		return receiptCandidate{}, baiduapi.ErrUploadUnconfirmed
	}
	return candidate, nil
}

// ReconcileReceipt never repeats a remote write. A saved receipt plus a fresh,
// matching identity observation is required before the local task can complete.
func (m *Manager) ReconcileReceipt(ctx context.Context, batch, item string) error {
	m.mu.Lock()
	if !validID(batch) || !validID(item) || m.ctx.Err() != nil || ctx.Err() != nil || m.verifyReceipt == nil || m.verifyingReceipts[item] || m.activeAttempts[item].ID != "" {
		m.mu.Unlock()
		return errors.New("当前不能核对该上传回执")
	}
	var state string
	if err := m.db.QueryRow(`SELECT state FROM items WHERE batch=? AND id=?`, batch, item).Scan(&state); err != nil {
		m.mu.Unlock()
		return err
	}
	if state != "unknown" {
		m.mu.Unlock()
		return errors.New("仅核对已隔离且有创建回执的上传")
	}
	candidate, err := m.receiptCandidate(item)
	if err != nil {
		m.mu.Unlock()
		return errors.New("缺少可信创建回执，保持隔离；不能按名称猜测完成")
	}
	requestID, err := id()
	if err != nil {
		m.mu.Unlock()
		return err
	}
	evidence := receiptVerificationEvidence{RequestID: requestID, Candidate: candidate, StartedAtUTC: now()}
	body, err := json.Marshal(evidence)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	if _, err = m.db.Exec(`INSERT INTO events(item,state,sent,recorded,error) SELECT id,'receipt_verification_started',sent,?,? FROM items WHERE id=?`, evidence.StartedAtUTC, "upload_receipt_verification_v1:"+string(body), item); err != nil {
		m.mu.Unlock()
		return err
	}
	if m.verifyingReceipts == nil {
		m.verifyingReceipts = map[string]bool{}
	}
	m.verifyingReceipts[item] = true
	verify := m.verifyReceipt
	m.wg.Add(1)
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.verifyingReceipts, item); m.mu.Unlock(); m.wg.Done() }()
	scoped, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancel)
	defer func() { stop(); cancel() }()
	result, verifyErr := verify(scoped, candidate.Scope, candidate.Receipt)
	if verifyErr == nil && (scoped.Err() != nil || m.ctx.Err() != nil || !result.CurrentScope.Valid() || result.CurrentScope.AccountFingerprint != candidate.Scope.AccountFingerprint || result.CurrentScope.AppKeyFingerprint != candidate.Scope.AppKeyFingerprint || result.Receipt != candidate.Receipt || result.ObservedAtUTC.IsZero() || result.ObservedAtUTC.Year() < 1 || result.ObservedAtUTC.Year() > 9999) {
		verifyErr = baiduapi.ErrUploadUnconfirmed
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if verifyErr != nil {
		// Do not persist arbitrary callback error text, which may contain credentials.
		if _, err = m.db.Exec(`INSERT INTO events(item,state,sent,recorded,error) SELECT id,'receipt_verification_failed',sent,?,? FROM items WHERE id=?`, now(), "upload_receipt_verification_v1:"+string(body), item); err != nil {
			return err
		}
		return errors.New("线上身份核对未通过，任务继续隔离；未重发上传")
	}
	current, err := m.receiptCandidate(item)
	if err != nil || current.AttemptID != candidate.AttemptID || current.Sequence != candidate.Sequence || current.Receipt != candidate.Receipt {
		return errors.New("核对期间本机回执证据变化，保持隔离")
	}
	result.ObservedAtUTC = result.ObservedAtUTC.UTC()
	result.CurrentScope.CheckedAtUTC = result.CurrentScope.CheckedAtUTC.UTC()
	evidence.Result = &result
	body, err = json.Marshal(evidence)
	if err != nil {
		return err
	}
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	at := now()
	update, err := tx.Exec(`UPDATE items SET state='confirmed',sent=size,updated=?,error='',actual_path=? WHERE batch=? AND id=? AND state='unknown'`, at, candidate.Receipt.Path, batch, item)
	if err != nil {
		return err
	}
	changed, err := update.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return errors.New("核对期间任务状态变化")
	}
	if _, err = tx.Exec(`INSERT INTO events(item,state,sent,recorded,error) SELECT id,'receipt_verified',sent,?,? FROM items WHERE id=?`, at, "upload_receipt_verification_v1:"+string(body), item); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	m.setPauseRequested(item, false)
	if err = os.Remove(filepath.Join(m.dir, item+".data")); err != nil && !os.IsNotExist(err) {
		size, _ := strconv.ParseInt(candidate.Receipt.Size, 10, 64)
		return m.update(item, "confirmed", size, "线上文件已核对完成；本机暂存回收失败，重启时重试")
	}
	return nil
}

func (m *Manager) HandleReceiptReconciliation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(405)
		return
	}
	batch, item := r.PathValue("batch"), r.PathValue("item")
	if !validID(batch) || !validID(item) || r.URL.RawQuery != "" || r.ContentLength > 0 {
		http.Error(w, "回执核对请求无效", 400)
		return
	}
	if err := m.ReconcileReceipt(r.Context(), batch, item); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			http.Error(w, err.Error(), 409)
		}
		return
	}
	write(w, map[string]string{"batch_id": batch, "item_id": item, "state": "confirmed"})
}

func (m *Manager) receiptVerificationSummary(item, attempt string) (string, string, string, error) {
	var body, recorded, state string
	err := m.db.QueryRow(`SELECT error,recorded,state FROM events WHERE item=? AND substr(error,1,?)=? ORDER BY sequence DESC LIMIT 1`, item, len("upload_receipt_verification_v1:"), "upload_receipt_verification_v1:").Scan(&body, &recorded, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return "none", "", "", nil
	}
	if err != nil {
		return "", "", "", err
	}
	var e receiptVerificationEvidence
	if json.Unmarshal([]byte(strings.TrimPrefix(body, "upload_receipt_verification_v1:")), &e) != nil || !validID(e.RequestID) || !validID(e.Candidate.AttemptID) || e.Candidate.Sequence <= 0 || !e.Candidate.Scope.Valid() || !e.Candidate.Receipt.Valid() {
		return "", "", "", errors.New("invalid receipt verification evidence")
	}
	if e.Candidate.AttemptID != attempt {
		return "none", "", "", nil
	}
	if _, err = time.Parse(time.RFC3339Nano, recorded); err != nil {
		return "", "", "", err
	}
	if _, err = time.Parse(time.RFC3339Nano, e.StartedAtUTC); err != nil {
		return "", "", "", err
	}
	switch state {
	case "receipt_verification_started":
		if e.Result == nil {
			return "started", recorded, "", nil
		}
	case "receipt_verification_failed":
		if e.Result == nil {
			return "failed", recorded, "", nil
		}
	case "receipt_verified":
		if e.Result != nil && e.Result.Receipt == e.Candidate.Receipt && e.Result.CurrentScope.Valid() && e.Result.CurrentScope.AccountFingerprint == e.Candidate.Scope.AccountFingerprint && e.Result.CurrentScope.AppKeyFingerprint == e.Candidate.Scope.AppKeyFingerprint && !e.Result.ObservedAtUTC.IsZero() && e.Result.ObservedAtUTC.Year() >= 1 {
			return "verified", recorded, e.Result.ObservedAtUTC.UTC().Format(time.RFC3339Nano), nil
		}
	}
	return "", "", "", errors.New("inconsistent receipt verification evidence")
}
