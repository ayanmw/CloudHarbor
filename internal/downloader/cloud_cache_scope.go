package downloader

import (
	"context"
	"errors"
	"time"

	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

// Managed readiness is published only after credentials.Manager has verified
// the account and persisted its immutable binding. Legacy unbound clients keep
// their token namespace; they cannot infer an account association from cache.
func (d *Downloader) authorizedCloudClient(ctx context.Context) (*baiduapi.Client, uint64, error) {
	client, epoch, err := d.authorizedClient(ctx)
	if err != nil {
		return nil, 0, err
	}
	d.clientMutex.RLock()
	managed := d.authManaged
	d.clientMutex.RUnlock()
	if managed && client.AppKey != "" && d.queue.store != nil {
		account, bindErr := d.queue.store.BoundAccountFingerprint()
		if bindErr != nil && !errors.Is(bindErr, taskstore.ErrAccountNotBound) {
			return nil, 0, bindErr
		}
		if bindErr == nil {
			namespace, scopeErr := d.queue.store.ResolveCloudCacheScope(account, client.ApplicationFingerprint(), client.Fingerprint(), time.Now().UTC())
			if scopeErr != nil {
				return nil, 0, scopeErr
			}
			client = client.WithCloudCacheNamespace(namespace)
		}
	}
	if _, err = d.intakeClient(ctx, epoch); err != nil {
		return nil, 0, err
	}
	return client, epoch, nil
}

func (d *Downloader) cloudIntakeClient(ctx context.Context, expectedEpoch uint64) (*baiduapi.Client, error) {
	client, epoch, err := d.authorizedCloudClient(ctx)
	if err != nil {
		return nil, err
	}
	if epoch != expectedEpoch {
		return nil, ErrIntakeStale
	}
	return client, nil
}

// Keep the final epoch check and SQLite publication together. Provider requests
// run outside this lock; a renewal cannot slip between validation and commit.
func (d *Downloader) commitCloudCacheObservation(ctx context.Context, epoch uint64, commit func() error) error {
	d.clientMutex.RLock()
	defer d.clientMutex.RUnlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if epoch != d.authEpoch || !d.authenticationReadyLocked() {
		return ErrAuthenticationWaiting
	}
	return commit()
}
