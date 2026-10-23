# Scheduler Ready Heaps Implementation Plan

> **For agentic workers:** This plan is being executed inline in the current isolated worktree. Steps use checkbox syntax for tracking.

**Goal:** Remove the O(history) scan from each scheduler claim while preserving task eligibility, retry deadlines, and existing ordering.

**Architecture:** Keep `scheduler.jobs` as durable-job authority. Maintain two in-memory indexed heaps only for cloud-scan-eligible runnable jobs: immediate jobs ordered by `Created` then `FsID`, and delayed jobs ordered by `NextAttempt` then the same tie-break. Rebuild these indexes during the existing full reconciliation scan; update one job's heap membership on claim, completion, cancel, retry, or auth invalidation. Do not change persisted JSON or cloud operations.

**Tech Stack:** Go standard library `container/heap`; existing Go tests and benchmarks.

**Spec:** `docs/ai/experience-plan.md`, D7 and D6 constraints.

## Global Constraints

- Preserve durable JSON v1 records and source-operation isolation.
- Only successfully scanned cloud metadata is eligible to run or perform source operations.
- Preserve earliest-created eligible task ordering; equal creation times use lower `FsID`.
- Preserve retry `NextAttempt`, auth waiting, active worker limits, and cancellation behavior.
- Add no dependencies and do not retain heap entries for completed history.

## Review Focus

- Retry deadlines compete with immediately ready jobs: only due retries enter the ready ordering.
- A successful scan changes eligibility and may replace job identity: old heap entries must not survive reconciliation.
- Auth invalidation must remove runnable candidates until a fresh scan.
- Cancel or failed persistence must not leave an executable stale heap entry.
- Concurrent workers must claim distinct jobs and preserve the configured cap.

### Task 1: Replace linear scheduler claim scans with eligible-job heaps

**Files:**
- Modify: `internal/downloader/scheduler.go`
- Modify: `internal/downloader/auth.go`
- Test: `internal/downloader/scheduler_test.go`, `internal/downloader/actions_test.go`, existing downloader regression suite
- Modify: `docs/ai/experience-plan.md`, `docs/ai/progress.md`

**Interfaces:** ready and delayed heaps are private scheduler implementation details; no API or persisted record changes.

- [x] Add regression tests for earliest-created ready selection, future retry deadline selection, auth invalidation, intake quarantine changes, and idle claims over large completed history; observe missing candidate-index helper fail before implementation.
- [x] Add indexed heap entries and scheduler helpers to clear, rebuild, remove, and enqueue eligible pending/retry jobs.
- [x] Update reconcile/auth invalidation/completion/cancel/retry/intake-block transitions to maintain candidate heaps under `q.mu`.
- [x] Run focused tests and benchmark idle claims with 10k/100k completed records; Windows full tests/vet, WSL race/vet and Node tests passed. Windows `-race` was attempted but GCC link failed.
- [ ] Build and smoke-test an independent package, then record exact package evidence and remaining NAS validation boundary in D7 progress.

**Validation:** no timing threshold in unit tests; benchmark should show `claim()` no longer scales with completed-history count. Existing retry, auth, cancellation, restart, and concurrency tests must pass.
