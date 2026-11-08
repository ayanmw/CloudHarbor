package foldersync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
)

type remoteArchiveVerifier interface {
	VerifyArchive(context.Context, Config, Step, string, []Entry) (RemoteArchiveVerification, error)
}

// Internal worker only. Public admission remains disabled until subtree
// completion/baseline verification and the real-account acceptance are wired.
func (m *Manager) executeSubtreeResolution(decision Resolution, run Run) {
	ctx := WithRunID(m.ctx, run.ID)
	c := decision.Config
	finish := func(status string, e error) {
		run.Status = status
		if e != nil {
			run.Error = e.Error()
		}
		if e = m.finish(run); e != nil {
			_ = m.event(run.ID, "persistence_failure", map[string]string{"error": e.Error()})
		}
	}
	compiled, err := CompileSubtreePlan(c.Mode, decision.Choice, decision.LocalCopy, decision.SubtreeReview)
	if err != nil {
		finish("failed", err)
		return
	}
	fixed := []Step{}
	for offset := 0; ; {
		page, e := m.SubtreeStepsPage(ctx, decision.ID, offset)
		if e != nil {
			finish("failed", e)
			return
		}
		fixed = append(fixed, page.Steps...)
		if !page.More {
			break
		}
		offset += len(page.Steps)
	}
	if len(compiled) == 0 || encoded(compiled) != encoded(fixed) || len(fixed) != run.StepCount {
		finish("failed", fmt.Errorf("固定有序步骤与来源清单不一致"))
		return
	}
	for _, s := range fixed {
		if s.Action == "remote_archive" {
			if _, ok := m.remote.(remoteArchiveVerifier); !ok {
				finish("failed", fmt.Errorf("远端缺少完整归档验证能力"))
				return
			}
		}
	}
	before, err := m.snapshot(ctx, c, nil)
	if err != nil {
		finish("failed", err)
		return
	}
	proof := subtreeCheckEvidence(decision, before)
	if proof.LocalExpectedSHA256 != proof.LocalObservedSHA256 || proof.RemoteExpectedSHA256 != proof.RemoteObservedSHA256 {
		finish("conflict", fmt.Errorf("执行前完整子树已变化"))
		return
	}
	if decision.LocalCopy != "" {
		for _, entries := range [][]Entry{before.Local, before.Remote} {
			for _, entry := range entries {
				if under(strings.ToLower(entry.Path), strings.ToLower(decision.LocalCopy)) {
					finish("conflict", fmt.Errorf("固定副本路径已占用"))
					return
				}
			}
		}
	}
	if err = m.event(run.ID, "snapshot_before", before); err != nil {
		finish("failed", err)
		return
	}
	for i, step := range fixed {
		current, e := m.snapshot(ctx, c, nil)
		if e != nil {
			finish("failed", e)
			return
		}
		lh, _ := subtreeIdentity(before.Local, "")
		rh, _ := subtreeIdentity(before.Remote, "")
		lnow, _ := subtreeIdentity(current.Local, "")
		rnow, _ := subtreeIdentity(current.Remote, "")
		if lh != lnow || rh != rnow {
			finish("conflict", fmt.Errorf("步骤之间出现未经确认的变化"))
			return
		}
		record := StepRecord{Ordinal: i + 1, Step: step, Status: "intent"}
		if err = m.saveStep(run.ID, record); err != nil {
			finish("failed", err)
			return
		}
		var result any
		switch step.Action {
		case "local_archive":
			result, err = archiveLocalSubtree(ctx, c, run.ID, step.Path, before.Local)
		case "local_move", "local_mkdir", "download":
			result, err = m.applyLocal(ctx, c, step, run.ID)
		case "remote_archive", "remote_mkdir", "upload":
			result, err = m.remote.Apply(ctx, c, step, run.ID, func(kind string, evidence any) error { return m.event(run.ID, kind, evidence) })
			if err == nil && step.Action == "remote_archive" {
				var archive RemoteArchiveVerification
				archive, err = m.remote.(remoteArchiveVerifier).VerifyArchive(ctx, c, step, run.ID, before.Remote)
				if err == nil {
					expectedHash, expectedCount := subtreeIdentity(before.Remote, step.Path)
					observedHash, observedCount := subtreeIdentity(archive.Entries, step.Path)
					expectedContent, _ := ArchiveContentIdentity(before.Remote, step.Path)
					observedContent, _ := ArchiveContentIdentity(archive.Entries, step.Path)
					if archive.ExpectedContentIdentitySHA256 != expectedContent || archive.ObservedContentIdentitySHA256 != observedContent || expectedContent != observedContent || encoded(archive.MtimeChanges) != encoded(ArchiveMtimeChanges(before.Remote, archive.Entries, step.Path)) {
						err = fmt.Errorf("归档身份内容或时间差异证据不符")
					}

					if archive.Version != "remote-archive-verification-v2" || archive.Root != step.Path || archive.ArchivePath != path.Join(c.RemoteRoot, privateDirectory, c.ID, run.ID, step.Path) || archive.VerifiedAt.IsZero() || archive.ExpectedSHA256 != expectedHash || archive.ObservedSHA256 != observedHash || archive.ExpectedItems != expectedCount || archive.ObservedItems != expectedCount || observedCount != fmt.Sprint(len(archive.Entries)) {
						err = fmt.Errorf("归档完整读回证据不符")
					}
				}
				if err != nil {
					err = fmt.Errorf("归档已移动，完整读回失败：%v", err)
				} else {
					result = map[string]any{"move": result, "verification": archive}
					err = m.event(run.ID, "subtree_archive_verified", archive)
				}
			}
		default:
			err = &NotStartedError{fmt.Errorf("不支持的固定目录步骤")}
		}
		if err == nil {
			var after Snapshot
			after, err = m.snapshot(ctx, c, nil)
			if err == nil {
				err = verifySubtreeTransition(before, after, step)
			}
			if err == nil {
				err = m.event(run.ID, "subtree_step_observed", map[string]any{"ordinal": i + 1, "snapshot": after})
				before = after
			} else {
				err = fmt.Errorf("操作后变化核对失败：%v", err)
			}
		}
		if err != nil {
			record.Status = "unknown"
			var notStarted *NotStartedError
			if errors.As(err, &notStarted) {
				record.Status = "failed"
			}
			record.Error = err.Error()
			record.Result = json.RawMessage(encoded(result))
			if e = m.saveStep(run.ID, record); e != nil {
				finish("unknown", e)
				return
			}
			finish(record.Status, err)
			return
		}
		record.Status = "confirmed"
		record.Result = json.RawMessage(encoded(result))
		if err = m.saveStep(run.ID, record); err != nil {
			finish("unknown", err)
			return
		}
		run.Confirmed++
		if err = m.progress(run); err != nil {
			finish("unknown", err)
			return
		}
	}
	if err = m.event(run.ID, "snapshot_after", before); err != nil {
		finish("failed", err)
		return
	}
	finish("verification_pending", nil)
}

func (m *Manager) subtreeExecutionSteps(ctx context.Context, d Resolution) ([]Step, error) {
	if d.State != "review_required" || d.SubtreeReview == nil || d.SubtreeReview.PlanVersion != "subtree-steps-v1" {
		return nil, fmt.Errorf("没有完整目录计划")
	}
	compiled, err := CompileSubtreePlan(d.Config.Mode, d.Choice, d.LocalCopy, d.SubtreeReview)
	if err != nil {
		return nil, err
	}
	if len(compiled) == 0 {
		return nil, fmt.Errorf("目录计划没有步骤")
	}
	fixed := []Step{}
	for offset := 0; ; {
		page, e := m.SubtreeStepsPage(ctx, d.ID, offset)
		if e != nil {
			return nil, e
		}
		fixed = append(fixed, page.Steps...)
		if !page.More {
			break
		}
		offset += len(page.Steps)
	}
	if encoded(compiled) != encoded(fixed) {
		return nil, fmt.Errorf("目录固定计划不一致")
	}
	for _, step := range fixed {
		if step.Action == "remote_archive" {
			if _, ok := m.remote.(remoteArchiveVerifier); !ok {
				return nil, fmt.Errorf("远端不支持完整归档核对")
			}
		}
	}
	return fixed, nil
}
