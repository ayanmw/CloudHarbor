package downloader

import (
	"context"
	"errors"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
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
	if !d.authenticationReadyLocked() || (d.authManaged && d.queue.eligible[info.FsID] != info) {
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
		q.eligible = make(map[int64]baiduapi.FileInfo)
		q.clearCandidates()
	}
	usable := d.authenticationReadyLocked()
	d.clientMutex.Unlock()
	if !usable {
		q.parkAuthenticationWaiting()
	}
	q.signal()
}
func (d *Downloader) SetAuthenticationFailureHandler(handler func(string)) {
	d.clientMutex.Lock()
	defer d.clientMutex.Unlock()
	d.onRejected = handler
}
func (q *scheduler) parkAuthenticationWaiting() {
	q.clearCandidates()
	for _, job := range q.jobs {
		if job.Status != "pending" && job.Status != "retry_wait" {
			continue
		}
		job.Status = "auth_wait"
		job.Error = ErrAuthenticationWaiting.Error()
		if q.persist(job) != nil {
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
	q.eligible = make(map[int64]baiduapi.FileInfo)
	d.clientMutex.Unlock()
	q.parkAuthenticationWaiting()
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
