package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

var ErrAuthenticationWaiting = errors.New("等待百度账号授权或续期")

func (d *Downloader) AuthenticationReady() bool {
	d.clientMutex.RLock()
	defer d.clientMutex.RUnlock()
	return d.authenticationReadyLocked()
}
func (d *Downloader) authenticationReadyLocked() bool {
	return !d.authManaged || (d.authReady && (d.authExpiry.IsZero() || time.Now().Before(d.authExpiry)))
}
func (d *Downloader) authorizedClient(ctx context.Context) (*baiduapi.Client, uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	d.clientMutex.RLock()
	defer d.clientMutex.RUnlock()
	if !d.authenticationReadyLocked() {
		return nil, 0, ErrAuthenticationWaiting
	}
	return d.client.WithContext(ctx), d.authEpoch, nil
}

func (d *Downloader) sourceOperationClient(ctx context.Context, info baiduapi.FileInfo) (*baiduapi.Client, error) {
	d.queue.mu.Lock()
	defer d.queue.mu.Unlock()
	d.clientMutex.RLock()
	defer d.clientMutex.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.queue.blockedIntakes[info.Path] {
		return nil, ErrIntakeUnconfirmed
	}
	eligibleInfo := d.queue.eligible[info.FsID]
	if !d.authenticationReadyLocked() || (d.authManaged && (eligibleInfo == nil || *eligibleInfo != info)) {
		return nil, ErrAuthenticationWaiting
	}
	return d.client.WithContext(ctx), nil
}

// Authentication changes invalidate scan eligibility. Resuming always requires
// a fresh scan; a previous account request cannot authorize a source operation.
func (d *Downloader) SetAuthentication(client *baiduapi.Client, ready bool, expiry time.Time) {
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopping || d.ctx.Err() != nil {
		return
	}
	d.clientMutex.Lock()
	changed := !d.authManaged || d.authReady != ready || !d.authExpiry.Equal(expiry) || d.client.Fingerprint() != client.Fingerprint()
	d.authManaged = true
	d.authReady = ready
	d.authExpiry = expiry
	d.client = client
	if changed {
		d.authEpoch++
		q.eligible = make(map[int64]*baiduapi.FileInfo)
		q.clearCandidates()
	}
	usable := d.authenticationReadyLocked()
	d.clientMutex.Unlock()
	if !usable {
		reason := "credentials_unavailable"
		if !ready {
			reason = "credentials_not_ready"
		} else if !expiry.IsZero() && !time.Now().Before(expiry) {
			reason = "credentials_expired"
		}
		q.parkAuthenticationWaiting(reason, nil)
	}
	q.signal()
}
func (d *Downloader) SetAuthenticationFailureHandler(handler func(string)) {
	d.clientMutex.Lock()
	defer d.clientMutex.Unlock()
	d.onRejected = handler
}
func (q *scheduler) parkAuthenticationWaiting(reason string, cause error) {
	q.clearCandidates()
	for _, job := range q.jobs {
		if job.Status != "pending" && job.Status != "retry_wait" {
			continue
		}
		previousStatus := job.Status
		var cancelledRetryDeadline *time.Time
		if !job.NextAttempt.IsZero() {
			value := job.NextAttempt.UTC()
			cancelledRetryDeadline = &value
		}
		job.Status = "auth_wait"
		job.Error = ErrAuthenticationWaiting.Error()
		job.NextAttempt = time.Time{}
		providerOperation, providerHTTPStatus, providerErrorCode := "", 0, 0
		if providerErr := apiError(cause); providerErr != nil {
			providerOperation = providerErr.Operation
			providerHTTPStatus = providerErr.Status
			providerErrorCode = providerErr.Code
		}
		evidence, _ := json.Marshal(struct {
			PreviousStatus              string     `json:"previous_status"`
			Status                      string     `json:"status"`
			Reason                      string     `json:"reason"`
			ErrorClass                  string     `json:"error_class,omitempty"`
			ProviderOperation           string     `json:"provider_operation,omitempty"`
			ProviderHTTPStatus          int        `json:"provider_http_status,omitempty"`
			ProviderErrorCode           int        `json:"provider_error_code,omitempty"`
			CancelledRetryDeadlineAtUTC *time.Time `json:"cancelled_retry_deadline_at_utc,omitempty"`
		}{previousStatus, job.Status, reason, errorClass(cause), providerOperation, providerHTTPStatus, providerErrorCode, cancelledRetryDeadline})
		event := taskstore.Event{Kind: "authentication_wait_entered", EvidenceType: "authentication_gate", Evidence: evidence}
		if providerHTTPStatus != 0 {
			observedAt := time.Now().UTC()
			event.ResponseObservedAtUTC = &observedAt
		}
		if q.persistWithEvent(job, event) != nil {
			return
		}
		q.reflect(job)
	}
}
func (d *Downloader) reportAuthenticationFailure(err error) {
	fp, auth := baiduapi.AuthenticationFingerprint(err)
	if !auth || fp == "" {
		return
	}
	q := d.queue
	q.mu.Lock()
	d.clientMutex.Lock()
	if !d.authManaged || d.client.Fingerprint() != fp || !d.authReady {
		d.clientMutex.Unlock()
		q.mu.Unlock()
		return
	}
	d.authReady = false
	d.authEpoch++
	handler := d.onRejected
	q.eligible = make(map[int64]*baiduapi.FileInfo)
	d.clientMutex.Unlock()
	q.parkAuthenticationWaiting("provider_rejected", err)
	q.mu.Unlock()
	// The manager callback may update admission; never invoke it under q.mu.
	if handler != nil {
		handler(fp)
	}
}

// Background owners drain before the shared credential/queue store closes.
func (d *Downloader) StartBackground(run func(context.Context)) error {
	q := d.queue
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopping || d.ctx.Err() != nil {
		return context.Canceled
	}
	q.wg.Add(1)
	go func() { defer q.wg.Done(); run(d.ctx) }()
	return nil
}
func (d *Downloader) Context() context.Context { return d.ctx }
