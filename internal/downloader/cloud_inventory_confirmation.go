package downloader

import (
	"context"
	"fmt"
)

type inventoryExpectedRunKey struct{}

// WithInventoryExpectedRun binds a web confirmation to the checkpoint observed
// before submission. Nil retains legacy API behavior; empty means no prior run.
func WithInventoryExpectedRun(ctx context.Context, expected *string) context.Context {
	if expected == nil {
		return ctx
	}
	return context.WithValue(ctx, inventoryExpectedRunKey{}, *expected)
}

func checkInventoryExpectedRun(ctx context.Context, actual string) error {
	if expected, ok := ctx.Value(inventoryExpectedRunKey{}).(string); ok && expected != actual {
		return fmt.Errorf("扫描运行已变化，请刷新状态后重新确认")
	}
	return nil
}

// PauseCloudInventoryRun only cancels the worker belonging to the confirmed run.
func (d *Downloader) PauseCloudInventoryRun(ctx context.Context, kind, expected string) error {
	client, _, err := d.authorizedClient(ctx)
	if err != nil {
		return err
	}
	d.inventoryScanMutex.Lock()
	defer d.inventoryScanMutex.Unlock()
	if d.inventoryScanStarting {
		return fmt.Errorf("扫描正在启动，请刷新后重试")
	}
	var actual string
	switch kind {
	case "scan":
		state, e := d.queue.store.LoadCloudScanState(client.Fingerprint())
		err = e
		actual = state.RunID
	case "refresh":
		state, e := d.queue.store.LoadCloudInventoryRefreshState(client.Fingerprint())
		err = e
		actual = state.RunID
	case "duplicate":
		state, e := d.queue.store.LoadCloudDuplicateScanState(client.Fingerprint())
		err = e
		actual = state.RunID
	default:
		return fmt.Errorf("扫描类型无效")
	}
	if err != nil {
		return err
	}
	if expected != actual {
		return fmt.Errorf("扫描运行已变化，请刷新后重新确认")
	}
	workerKind := "full"
	if kind == "refresh" || kind == "duplicate" {
		workerKind = kind
	}
	if d.inventoryScanKind == workerKind && d.inventoryScanCancel != nil {
		d.inventoryScanCancel()
	}
	return nil
}
