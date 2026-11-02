package baiduapi

import (
	"log"
	"time"
)

type downloadTrace struct {
	transferID, batchID, path string
	fsID                      int64
}

// Copies isolate concurrent file traces; never attach temporary download URLs
// or account credentials to a trace.
func (c *Client) WithDownloadTrace(transferID, batchID string, fsID int64, path string) *Client {
	copy := *c
	copy.downloadTrace = downloadTrace{transferID: transferID, batchID: batchID, fsID: fsID, path: path}
	return &copy
}

func (c *Client) downloadLog(event, format string, args ...any) {
	trace := c.downloadTrace
	fields := []any{time.Now().UTC().Format(time.RFC3339Nano), trace.transferID, trace.batchID, trace.fsID, trace.path, event}
	log.Printf("[BaiduAPI] at_utc=%s transfer_id=%q batch_id=%q fsid=%d path=%q event=%s "+format, append(fields, args...)...)
}
