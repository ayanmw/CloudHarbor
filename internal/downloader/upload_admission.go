package downloader

import (
	"context"
	"errors"
	"sync"
)

type uploadLeaseContextKey struct{}

// Fields are guarded by the owning downloader's inventoryScanMutex. A token
// identifies one live member, not merely the fact a scan reservation exists.
type uploadScanLease struct {
	owner    *Downloader
	released bool
}

// Nested parent creation borrows its caller's live lease. An expired/foreign
// token must fail closed instead of falling back to independent admission.
func (d *Downloader) useUploadLease(ctx context.Context) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if ctx.Value(uploadLeaseContextKey{}) != nil {
		if !d.validUploadLease(ctx) {
			return nil, nil, errors.New("上传租约已失效")
		}
		return ctx, func() {}, nil
	}
	return d.acquireUploadLease(ctx)
}

func (d *Downloader) acquireUploadLease(ctx context.Context) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	d.queue.mu.Lock()
	defer d.queue.mu.Unlock()
	if err := d.ctx.Err(); err != nil {
		return nil, nil, err
	}
	if d.queue.stopping || d.queue.err != nil {
		return nil, nil, errors.New("下载队列不可用，无法准入上传")
	}
	d.inventoryScanMutex.Lock()
	defer d.inventoryScanMutex.Unlock()
	if d.uploadLeaseMembers == 0 {
		if d.queue.maintenance || d.inventoryScanCancel != nil || d.inventoryScanStarting {
			return nil, nil, errors.New("网盘扫描或维护正在运行，无法准入上传")
		}
		d.inventoryScanStarting = true
	} else if !d.inventoryScanStarting || d.inventoryScanCancel != nil {
		return nil, nil, errors.New("上传组预约状态不一致")
	}
	lease := &uploadScanLease{owner: d}
	d.uploadLeaseMembers++
	return context.WithValue(ctx, uploadLeaseContextKey{}, lease), func() { d.releaseUploadLease(lease) }, nil
}

func (d *Downloader) releaseUploadLease(lease *uploadScanLease) {
	d.inventoryScanMutex.Lock()
	defer d.inventoryScanMutex.Unlock()
	if lease.owner != d || lease.released {
		return
	}
	lease.released = true
	d.uploadLeaseMembers--
	if d.uploadLeaseMembers == 0 {
		d.inventoryScanStarting = false
	}
}

func (d *Downloader) validUploadLeaseLocked(ctx context.Context) bool {
	lease, ok := ctx.Value(uploadLeaseContextKey{}).(*uploadScanLease)
	return ok && lease != nil && lease.owner == d && !lease.released && d.uploadLeaseMembers > 0 && d.inventoryScanStarting && d.inventoryScanCancel == nil
}

func (d *Downloader) validUploadLease(ctx context.Context) bool {
	if ctx.Err() != nil || d.ctx.Err() != nil {
		return false
	}
	d.inventoryScanMutex.Lock()
	defer d.inventoryScanMutex.Unlock()
	return d.validUploadLeaseLocked(ctx)
}

// This gate only orders upload namespace stages. Its caller must still use
// normal maintenance, cloudMutex, account and durable operation checks.
func (d *Downloader) acquireUploadNamespace(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := d.ctx.Err(); err != nil {
		return nil, err
	}
	d.inventoryScanMutex.Lock()
	if !d.validUploadLeaseLocked(ctx) {
		d.inventoryScanMutex.Unlock()
		return nil, errors.New("上传租约已失效")
	}
	if d.uploadNamespaceGate == nil {
		d.uploadNamespaceGate = make(chan struct{}, 1)
	}
	gate := d.uploadNamespaceGate
	d.inventoryScanMutex.Unlock()
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-d.ctx.Done():
		return nil, d.ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-gate
		return nil, err
	}
	if err := d.ctx.Err(); err != nil {
		<-gate
		return nil, err
	}
	d.inventoryScanMutex.Lock()
	if !d.validUploadLeaseLocked(ctx) {
		d.inventoryScanMutex.Unlock()
		<-gate
		return nil, errors.New("上传租约已失效")
	}
	// Retain the reservation through phase cleanup even if its original member
	// is released concurrently; only the last member/phase may clear it.
	held := &uploadScanLease{owner: d}
	d.uploadLeaseMembers++
	d.inventoryScanMutex.Unlock()
	var once sync.Once
	return func() { once.Do(func() { d.releaseUploadLease(held); <-gate }) }, nil
}
