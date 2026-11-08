package baiduapi

import (
	"io"
	"sync/atomic"
	"time"
)

// UploadTransferSample describes a completed request observation, not a remote
// receipt. PayloadBytesRead excludes multipart framing; failed requests may have
// partial observations. No wall-clock gap may be used to reconstruct a sample.
type UploadTransferSample struct {
	PlannedPayloadBytes *int64    `json:"planned_payload_bytes,string,omitempty"`
	PlannedPartCount    int       `json:"planned_part_count,omitempty"`
	PartPayloadBytes    int64     `json:"part_payload_bytes,string,omitempty"`
	PartConfirmed       bool      `json:"part_confirmed,omitempty"`
	PartIndex           int       `json:"part_index"`
	PayloadBytesRead    int64     `json:"payload_bytes_read,string"`
	ActiveNanos         int64     `json:"active_nanos,string"`
	StartedAtUTC        time.Time `json:"started_at_utc"`
	FinishedAtUTC       time.Time `json:"finished_at_utc"`
}

type uploadPayloadReader struct {
	reader        io.Reader
	position      int64
	start, end    int64
	contentLength int64
	payloadRead   atomic.Int64
}

func newUploadPayloadReader(reader io.Reader, start, length int64) *uploadPayloadReader {
	result := &uploadPayloadReader{reader: reader, start: start, end: start + length}
	if sized, ok := reader.(interface{ Len() int }); ok {
		result.contentLength = int64(sized.Len())
	}
	return result
}
func (r *uploadPayloadReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	end := r.position + int64(n)
	overlap := min(end, r.end) - max(r.position, r.start)
	if overlap > 0 {
		r.payloadRead.Add(overlap)
	}
	r.position = end
	return n, err
}
