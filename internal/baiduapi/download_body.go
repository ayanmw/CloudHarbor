package baiduapi

import (
	"context"
	"io"
	"time"
)

type idleTimeoutError struct{}

func (*idleTimeoutError) Error() string   { return "download idle timeout" }
func (*idleTimeoutError) Timeout() bool   { return true }
func (*idleTimeoutError) Temporary() bool { return true }

var errDownloadIdleTimeout = &idleTimeoutError{}

// An idle limit is reset on received data; unlike a short total timeout it
// permits a large file to download for hours while still recovering a stall.
type idleBody struct {
	io.ReadCloser
	ctx     context.Context
	cancel  context.CancelCauseFunc
	timer   *time.Timer
	timeout time.Duration
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.timer.Reset(b.timeout)
	}
	if context.Cause(b.ctx) == errDownloadIdleTimeout {
		return n, errDownloadIdleTimeout
	}
	return n, err
}
func (b *idleBody) Close() error { b.timer.Stop(); b.cancel(nil); return b.ReadCloser.Close() }

func (c *Client) WithIdleTimeout(timeout time.Duration) *Client {
	copy := *c
	copy.idleTimeout = timeout
	return &copy
}
