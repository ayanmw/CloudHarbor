package baiduapi

import (
	"context"
	"errors"
	"os"
	"time"
)

// UploadSessionResume refers to historical evidence. Admission must revalidate
// the durable journal and absence of a live source worker before any HTTP.
type UploadSessionResume struct {
	SourceAttemptID string            `json:"source_attempt_id"`
	SourceSequence  int64             `json:"source_sequence,string"`
	Plan            UploadSessionPlan `json:"plan"`
	ConfirmedParts  []int             `json:"confirmed_parts"`
}
type UploadResumeAdmission struct {
	SourceAttemptID  string             `json:"source_attempt_id"`
	SourceSequence   int64              `json:"source_sequence,string"`
	SourcePlanDigest string             `json:"source_plan_digest"`
	CurrentScope     UploadAccountScope `json:"current_scope"`
	RemainingParts   []int              `json:"remaining_parts"`
	ObservedAtUTC    time.Time          `json:"observed_at_utc"`
}
type uploadResumeAdmissionKey struct{}

func WithUploadResumeAdmission(ctx context.Context, record func(UploadResumeAdmission) error) context.Context {
	return context.WithValue(ctx, uploadResumeAdmissionKey{}, record)
}

// ResumeUploadLocalFileMeasured never calls precreate and never falls back to a
// fresh upload. A rejected/expired session must be handled by its durable owner.
func (c *Client) ResumeUploadLocalFileMeasured(ctx context.Context, local, remote string, source UploadSessionResume, progress func(string, int64) error, observe func(UploadTransferSample) error) (UploadReceipt, error) {
	var empty UploadReceipt
	scope, err := c.uploadAccountScope(ctx)
	if err != nil {
		return empty, err
	}
	if scope == nil || source.SourceSequence <= 0 || !sessionHex(source.SourceAttemptID, 16) || source.Plan.RemotePath != remote || source.Plan.CreateAdmissionVersion != 1 {
		return empty, errors.New("upload resume evidence requires verification")
	}
	if err = ValidateUploadSessionScope(source.Plan, *scope); err != nil {
		return empty, err
	}
	record, ok := ctx.Value(uploadResumeAdmissionKey{}).(func(UploadResumeAdmission) error)
	if !ok || record == nil {
		return empty, errors.New("upload resume requires durable admission")
	}
	if observer, ok := ctx.Value(uploadSessionObserverKey{}).(func(UploadSessionCheckpoint) error); !ok || observer == nil {
		return empty, errors.New("upload resume requires durable part/create observations")
	}
	needed := map[int]bool{}
	for _, part := range source.Plan.NeededParts {
		needed[part] = true
	}
	confirmed := map[int]bool{}
	var previous int64
	for _, part := range source.ConfirmedParts {
		if !needed[part] || confirmed[part] {
			return empty, errors.New("invalid confirmed upload parts")
		}
		confirmed[part] = true
		previous += min(int64(UploadBlockSize), source.Plan.Size-int64(part)*UploadBlockSize)
	}
	remaining := []int{}
	for _, part := range source.Plan.NeededParts {
		if !confirmed[part] {
			remaining = append(remaining, part)
		}
	}
	f, err := os.Open(local)
	if err != nil {
		return empty, err
	}
	defer f.Close()
	if err = verifyUploadSessionPayload(ctx, f, source.Plan, func() error { return progress("hashing", previous) }); err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	admission := UploadResumeAdmission{SourceAttemptID: source.SourceAttemptID, SourceSequence: source.SourceSequence, SourcePlanDigest: source.Plan.Digest(), CurrentScope: *scope, RemainingParts: remaining, ObservedAtUTC: time.Now().UTC()}
	if err = record(admission); err != nil {
		return empty, err
	}
	return c.uploadSessionParts(ctx, f, source.Plan, remaining, previous, progress, observe)
}

type uploadSessionResumeKey struct{}

func WithUploadSessionResume(ctx context.Context, source UploadSessionResume) context.Context {
	return context.WithValue(ctx, uploadSessionResumeKey{}, source)
}
func UploadSessionResumeFromContext(ctx context.Context) (UploadSessionResume, bool) {
	source, ok := ctx.Value(uploadSessionResumeKey{}).(UploadSessionResume)
	return source, ok
}
