package downloader

import (
	"context"
	"time"
)

const cloudAPIRequestInterval = 250 * time.Millisecond

// cloudAPIPacer gives all API clients owned by one Downloader a shared,
// cancellation-aware minimum interval between request starts.
type cloudAPIPacer struct {
	gate     chan struct{}
	interval time.Duration
	next     time.Time
}

func newCloudAPIPacer(interval time.Duration) *cloudAPIPacer {
	return &cloudAPIPacer{interval: interval, gate: make(chan struct{}, 1)}
}

func (p *cloudAPIPacer) Wait(ctx context.Context) error {
	select {
	case p.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-p.gate }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay := time.Until(p.next); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p.next = time.Now().Add(p.interval)
	return nil
}
