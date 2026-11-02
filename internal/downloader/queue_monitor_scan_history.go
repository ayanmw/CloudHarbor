package downloader

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"time"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

var ErrQueueMonitorScanSkipped = errors.New("automatic queue scan skipped")
var ErrQueueMonitorScanHistoryUnavailable = errors.New("automatic queue scan history unavailable")

const queueMonitorRetryBase = 30 * time.Second
const queueMonitorRetryMax = 15 * time.Minute

func queueMonitorRetryDelay(failures int, retryAfter time.Duration) time.Duration {
	if failures < 1 {
		failures = 1
	}
	exponent := failures - 1
	if exponent > 20 {
		exponent = 20
	}
	delay := time.Duration(float64(queueMonitorRetryBase) * math.Pow(2, float64(exponent)))
	if delay > queueMonitorRetryMax {
		delay = queueMonitorRetryMax
	}
	if retryAfter > delay {
		delay = retryAfter
	}
	if delay > time.Hour {
		delay = time.Hour
	}
	return delay
}

func providerRetryAfter(err error) time.Duration {
	var apiErr *baiduapi.APIError
	if errors.As(err, &apiErr) {
		return apiErr.RetryAfter
	}
	return 0
}

func (d *Downloader) QueueMonitorRetryState(ctx context.Context) (*taskstore.QueueMonitorRetryState, error) {
	if d.queue.store == nil {
		return nil, nil
	}
	account, err := d.queueMonitorHistoryAccount(ctx)
	if err != nil {
		return nil, err
	}
	return d.queue.store.QueueMonitorRetryState(account)
}

func (d *Downloader) queueMonitorHistoryAccount(ctx context.Context) (string, error) {
	if d.queue.store != nil {
		account, bindErr := d.queue.store.BoundAccountFingerprint()
		if bindErr == nil {
			return account, nil
		}
		if !errors.Is(bindErr, taskstore.ErrAccountNotBound) {
			return "", bindErr
		}
	}
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return "", err
	}
	return client.Fingerprint(), nil
}

func newQueueMonitorScanID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func (d *Downloader) QueueMonitorScanHistoryRuns(ctx context.Context, before int64, filter taskstore.QueueMonitorScanFilter, limit int) ([]taskstore.QueueMonitorScanRun, int64, bool, error) {
	if d.queue.store == nil {
		return nil, 0, false, ErrQueueMonitorScanHistoryUnavailable
	}
	account, err := d.queueMonitorHistoryAccount(ctx)
	if err != nil {
		return nil, 0, false, err
	}
	if before < 0 || limit < 1 || limit > 100 {
		return nil, 0, false, fmt.Errorf("invalid queue monitor scan page")
	}
	runs, err := d.queue.store.ListQueueMonitorScanRuns(account, before, filter, limit+1)
	if err != nil {
		return nil, 0, false, err
	}
	if len(runs) <= limit {
		return runs, 0, false, nil
	}
	page := runs[:limit]
	return page, page[len(page)-1].Sequence, true, nil
}

func (d *Downloader) QueueMonitorScanHistoryEvents(ctx context.Context, runID string, before int64, limit int) ([]taskstore.QueueMonitorScanEvent, int64, bool, error) {
	if d.queue.store == nil {
		return nil, 0, false, ErrQueueMonitorScanHistoryUnavailable
	}
	account, err := d.queueMonitorHistoryAccount(ctx)
	if err != nil {
		return nil, 0, false, err
	}
	if before < 0 || limit < 1 || limit > 100 {
		return nil, 0, false, fmt.Errorf("invalid queue monitor scan event page")
	}
	events, err := d.queue.store.ListQueueMonitorScanEvents(account, runID, before, limit+1)
	if err != nil {
		return nil, 0, false, err
	}
	if len(events) <= limit {
		return events, 0, false, nil
	}
	page := events[:limit]
	return page, page[len(page)-1].Sequence, true, nil
}

func (d *Downloader) QueueMonitorScanHistoryExport(ctx context.Context, runID string) (taskstore.QueueMonitorScanBundle, error) {
	if d.queue.store == nil {
		return taskstore.QueueMonitorScanBundle{}, ErrQueueMonitorScanHistoryUnavailable
	}
	account, err := d.queueMonitorHistoryAccount(ctx)
	if err != nil {
		return taskstore.QueueMonitorScanBundle{}, err
	}
	return d.queue.store.ExportQueueMonitorScanEvidence(ctx, account, runID)
}

func (d *Downloader) QueueMonitorScanLatest(ctx context.Context) (latest *taskstore.QueueMonitorScanRun, latestSuccess *taskstore.QueueMonitorScanRun, err error) {
	if d.queue.store == nil {
		return nil, nil, ErrQueueMonitorScanHistoryUnavailable
	}
	account, err := d.queueMonitorHistoryAccount(ctx)
	if err != nil {
		return nil, nil, err
	}
	return d.queue.store.LatestQueueMonitorScanRuns(ctx, account)
}
