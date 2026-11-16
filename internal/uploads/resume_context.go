package uploads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

// Empty hash-only attempts may be traversed, but any precreate intent without a
// complete session stops traversal: it may already have produced a rapid upload.
func (m *Manager) uploadResumeSource(item, previous string) (string, *uploadSessionJournal, error) {
	seen := map[string]bool{}
	for previous != "" {
		if seen[previous] {
			return "", nil, errors.New("cyclic upload attempt history")
		}
		seen[previous] = true
		source, err := m.readSessionJournal(item, previous)
		if err == nil {
			if !source.CreateTracked || source.Plan.AccountScope == nil || len(source.PendingParts) != 0 || source.CreateAdmission != nil {
				return "", nil, fmt.Errorf("%w: 旧会话存在未确认请求或缺少恢复边界", baiduapi.ErrUploadUnconfirmed)
			}
			return previous, &source, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", nil, fmt.Errorf("%w: 旧会话证据无法核对", baiduapi.ErrUploadUnconfirmed)
		}
		if _, err = m.readPrecreateJournal(item, previous); !errors.Is(err, sql.ErrNoRows) {
			return "", nil, fmt.Errorf("%w: 预创建已有意图或回执，不能重新提交", baiduapi.ErrUploadUnconfirmed)
		}
		start, err := m.uploadAttemptStart(item, previous)
		if err != nil {
			return "", nil, err
		}
		previous = start.PreviousAttemptID
	}
	return "", nil, nil
}
func (m *Manager) uploadAttemptContext(item, attempt string) (context.Context, error) {
	ctx := baiduapi.WithUploadSessionObserver(m.ctx, m.sessionEvidenceRecorder(item, attempt))
	ctx = baiduapi.WithUploadPrecreateObserver(ctx, m.precreateEvidenceRecorder(item, attempt))
	ctx = baiduapi.WithUploadNamespaceAdmissionGuard(ctx, m.namespaceAdmissionGuard(item, attempt))
	current, err := m.latestUploadAttempt(item)
	if err != nil {
		return nil, err
	}
	if current.AttemptID != attempt {
		return nil, errors.New("upload attempt changed")
	}
	sourceID, source, err := m.uploadResumeSource(item, current.PreviousAttemptID)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return ctx, nil
	}
	ctx = baiduapi.WithUploadSessionResume(ctx, baiduapi.UploadSessionResume{SourceAttemptID: sourceID, SourceSequence: source.LastSequence, Plan: source.Plan, ConfirmedParts: append([]int{}, source.ConfirmedParts...)})
	return baiduapi.WithUploadResumeAdmission(ctx, m.resumeAdmissionRecorder(item, attempt)), nil
}
