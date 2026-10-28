package downloader

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

var ErrSourceOperationUnknown = errors.New("网盘后处理结果未知，保持隔离，禁止重发")
var ErrSourceIdentityChanged = errors.New("网盘源项身份已变化，未执行改名或删除")

func validSourceRecord(job *queueJob) bool {
	if job.AttemptID != "" {
		decoded, err := hex.DecodeString(job.AttemptID)
		if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != job.AttemptID {
			return false
		}
	}
	if (job.SourceOperationID == "") != (job.SourceAttemptID == "") {
		return false
	}
	if job.SourceOperationID != "" {
		for _, id := range []string{job.SourceOperationID, job.SourceAttemptID} {
			decoded, err := hex.DecodeString(id)
			if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != id {
				return false
			}
		}
		if job.SourceState != "sending" && job.SourceState != "unknown" {
			return false
		}
	}
	switch job.SourceState {
	case "":
		return job.Status != "source_unknown"
	case "sending", "unknown":
		return job.Status == "source_unknown"
	case "changed":
		return job.Status == "failed"
	case "confirmed":
		return job.Status == "done"
	}
	return false
}

func newSourceOperationID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func sourceOperationEvidence(info baiduapi.FileInfo, state string) json.RawMessage {
	body, _ := json.Marshal(struct {
		State    string `json:"state"`
		FsID     int64  `json:"fs_id"`
		Path     string `json:"path"`
		Size     int64  `json:"size"`
		Mtime    int64  `json:"mtime"`
		IsDir    int    `json:"is_dir"`
		Filename string `json:"server_filename"`
	}{state, info.FsID, info.Path, info.Size, info.Mtime, info.IsDir, info.ServerFilename})
	return body
}

func sourceOperationResultEvidence(info baiduapi.FileInfo, state string, operationErr error) json.RawMessage {
	providerOperation, providerHTTPStatus, providerErrorCode := "", 0, 0
	if providerErr := apiError(operationErr); providerErr != nil {
		providerOperation = providerErr.Operation
		providerHTTPStatus = providerErr.Status
		providerErrorCode = providerErr.Code
	}
	body, _ := json.Marshal(struct {
		State              string `json:"state"`
		FsID               int64  `json:"fs_id"`
		Path               string `json:"path"`
		Size               int64  `json:"size"`
		Mtime              int64  `json:"mtime"`
		IsDir              int    `json:"is_dir"`
		Filename           string `json:"server_filename"`
		ErrorClass         string `json:"error_class,omitempty"`
		ProviderOperation  string `json:"provider_operation,omitempty"`
		ProviderHTTPStatus int    `json:"provider_http_status,omitempty"`
		ProviderErrorCode  int    `json:"provider_error_code,omitempty"`
	}{state, info.FsID, info.Path, info.Size, info.Mtime, info.IsDir, info.ServerFilename, errorClass(operationErr), providerOperation, providerHTTPStatus, providerErrorCode})
	return body
}
func (q *scheduler) rebuildSourceBlocks() {
	q.blockedSources = make(map[string]bool)
	for _, job := range q.jobs {
		if job.SourceState != "sending" && job.SourceState != "unknown" {
			continue
		}
		q.blockedSources[job.Info.Path] = true
		if job.Mode == DownloadModeKeep {
			q.blockedSources[path.Join(path.Dir(job.Info.Path), path.Base(job.Info.Path)+".可删除")] = true
		}
	}
}
func (d *Downloader) beginSourceOperation(ctx context.Context, info baiduapi.FileInfo) (bool, error) {
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.blockedSources[info.Path] {
		return false, ErrSourceOperationUnknown
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if q.err != nil {
		return false, q.err
	}
	// Legacy nonpersistent callers have no recovery journal. Main always uses
	// OpenDownloader; its source requests require a registered durable root.
	if q.store == nil {
		return false, nil
	}
	job := q.jobs[info.FsID]
	if job == nil || job.Info != info || job.Status != "downloading" || q.stopping {
		return false, ErrTaskConflict
	}
	operationID, err := newSourceOperationID()
	if err != nil {
		return false, err
	}
	attemptID, err := newSourceOperationID()
	if err != nil {
		return false, err
	}
	job.SourceState = "sending"
	job.SourceOperationID = operationID
	job.SourceAttemptID = attemptID
	job.Status = "source_unknown"
	job.Error = "网盘后处理请求即将发送；完成前禁止重发"
	job.NextAttempt = time.Time{}
	q.rebuildSourceBlocks()
	event := taskstore.Event{Kind: "source_operation_sending", OperationID: operationID, AttemptID: attemptID, EvidenceType: "local_send_intent", Evidence: sourceOperationEvidence(info, "sending")}
	if err := q.persistWithEvent(job, event); err != nil {
		return false, err
	}
	q.reflect(job)
	return true, nil
}
func (d *Downloader) finishSourceOperation(info baiduapi.FileInfo, operationErr error) error {
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	job := q.jobs[info.FsID]
	if job == nil || job.SourceState != "sending" {
		return ErrTaskConflict
	}
	operationID, attemptID := job.SourceOperationID, job.SourceAttemptID
	event := taskstore.Event{OperationID: operationID, AttemptID: attemptID}
	responseObservedAt := time.Now().UTC()
	if providerErr := apiError(operationErr); providerErr != nil && providerErr.Status != 0 {
		event.ResponseObservedAtUTC = &responseObservedAt
	}
	switch {
	case errors.Is(operationErr, ErrSourceIdentityChanged):
		job.SourceState = "changed"
		job.Status = "failed"
		job.Error = operationErr.Error()
		job.SourceOperationID = ""
		job.SourceAttemptID = ""
		event.Kind = "source_identity_changed"
		event.EvidenceType = "local_identity_conflict"
		event.CloudObservedAtUTC = &responseObservedAt
	case operationErr == nil:
		job.SourceState = "confirmed"
		job.Status = "done"
		job.Error = ""
		job.SourceOperationID = ""
		job.SourceAttemptID = ""
		event.Kind = "source_operation_confirmed"
		event.EvidenceType = "baidu_sync_response"
		event.ResponseObservedAtUTC = &responseObservedAt
	case errors.Is(operationErr, baiduapi.ErrSourceOperationUnconfirmed):
		job.SourceState = "unknown"
		job.Status = "source_unknown"
		job.Error = ErrSourceOperationUnknown.Error()
		event.Kind = "source_operation_unknown"
		event.EvidenceType = "baidu_outcome_unconfirmed"
	default:
		job.SourceState = ""
		job.Status = "downloading"
		job.Error = ""
		job.SourceOperationID = ""
		job.SourceAttemptID = ""
		event.Kind = "source_operation_retryable_failure"
		event.EvidenceType = "explicit_retryable_failure"
	}
	event.Evidence = sourceOperationResultEvidence(info, job.SourceState, operationErr)
	if err := q.persistWithEvent(job, event); err != nil {
		// The old durable sending record must remain authoritative if saving a
		// receipt fails. Never let the in-memory state release the quarantine.
		job.SourceState = "unknown"
		job.Status = "source_unknown"
		job.SourceOperationID = operationID
		job.SourceAttemptID = attemptID
		job.Error = ErrSourceOperationUnknown.Error()
		q.rebuildSourceBlocks()
		q.reflect(job)
		return err
	}
	q.rebuildSourceBlocks()
	q.reflect(job)
	return operationErr
}
