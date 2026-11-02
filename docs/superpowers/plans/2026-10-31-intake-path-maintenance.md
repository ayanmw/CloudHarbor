# Intake Path Maintenance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make intake previews, persisted intake history, remote directory protection, and managed path changes remain correct across expiry, restart, and concurrent operations.

**Architecture:** Add a shared read/write gate for path-sensitive operations. Intake previews, commits, rechecks, and remote cloud mutations hold the read side; managed path changes hold the write side and read the current paths only after acquiring it. Derive expired preview state in returned records without rewriting JSON, and validate/recheck historical intakes against each record's saved queue directory.

**Tech Stack:** Go `sync.RWMutex`, existing JSON intake journal, Baidu API client, embedded web UI, Go tests and Node WebUI tests.

**Spec:** `docs/superpowers/specs/2026-10-31-expired-intake-preview-state-design.zh-CN.md`

## Global Constraints

- Do not introduce worktrees or modify unrelated pre-existing work.
- Keep intake JSON format and persisted creation facts unchanged.
- Preserve the existing no-blind-retry and unknown-result quarantine behavior.
- Do not send Baidu requests from tests to a real account; use the existing local provider fixture.
- Keep path changes blocked by a currently valid preview or a running intake operation.
- Do not publish a preview until the five outcomes pass the release gates.

## Review Focus

- Expired preview, restart, and auth-epoch mismatch must become read-only `expired` projections; assert persisted JSON remains `preview` and commit still rejects it.
- A completed intake from a previous queue directory must load after path change/restart; an unknown result must recheck only its recorded destination.
- The path write lock must serialize intake operations and cloud mutations with path changes, including admission failures and cancellation.
- Cloud directory move/delete checks must use the queue path captured after acquiring the operation gate.
- A second path update must derive its source path after acquiring the write gate, so it cannot migrate from a pre-gate snapshot.

---

### Task 1: Project expired preview state without changing the journal

**Files:**
- Modify: `internal/downloader/intake_store.go`
- Modify: `internal/downloader/managed_paths.go`
- Modify: `internal/downloader/intake_test.go`
- Modify: `internal/downloader/managed_paths_test.go`

**Interfaces:**
- Produces: a helper that determines whether an `intakeRecord` in state `preview` is still valid for the current boot ID, auth epoch, and expiry time.
- `GetIntakes()` returns `expired` on its copied result only; journal records stay unchanged.

- [x] **Step 1: Write failing tests** named `TestGetIntakesProjectsExpiredPreviewWithoutChangingJournal`, `TestGetIntakesProjectsRestartAndAuthEpochPreviewsExpired`, and `TestSetManagedPathsIgnoresExpiredPreviewButBlocksValidPreview`. Assert that expired results are labeled `expired`, stored JSON still says `preview`, valid previews still block path updates, and expired previews do not.
- [x] **Step 2: Run the targeted downloader tests** with `go test ./internal/downloader -run 'TestGetIntakesProjects|TestSetManagedPathsIgnoresExpiredPreview' -count=1`; confirm the failures identify missing projection/block behavior.
- [x] **Step 3: Implement** a validity helper using a single captured `now`, current boot ID, and auth epoch. Project only returned copies and use the projected records in `SetManagedPaths` eligibility checks.
- [x] **Step 4: Re-run the targeted tests** and confirm they pass; targeted expiry and path eligibility tests passed.

### Task 2: Preserve historical queue-directory identity

**Files:**
- Modify: `internal/downloader/intake_store.go`
- Modify: `internal/downloader/intake.go`
- Modify: `internal/downloader/intake_test.go`

**Interfaces:**
- Changes `previewIntakeItems` to accept the queue directory snapshot explicitly.
- Changes `checkIntakeOutcome` to accept the queue directory whose destination it must inspect.

- [x] **Step 1: Write failing tests** named `TestLoadIntakesAcceptsCompletedHistoryFromPriorQueueDirectory` and `TestRecheckIntakeUsesRecordedQueueDirectory`. Create a completed record under an old queue path, reopen with a new configured path, and verify an unknown move can be confirmed by checking the old recorded destination without another move request.
- [x] **Step 2: Run** `go test ./internal/downloader -run 'TestLoadIntakesAcceptsCompletedHistoryFromPriorQueueDirectory|TestRecheckIntakeUsesRecordedQueueDirectory' -count=1`; confirm current loader rejects the old record and current recheck misses the target.
- [x] **Step 3: Implement** validation against `r.QueueDir`; pass a single current queue snapshot into preview checks; require commit's record directory to match its valid preview context; use `r.QueueDir` for commit outcome checks and rechecks.
- [x] **Step 4: Re-run the targeted tests** and confirm old history loads, stale previews remain uncommittable, and unknown results stay isolated until the old target identity is confirmed; targeted queue-history tests passed.

### Task 3: Serialize path changes with intake and cloud mutations

**Files:**
- Modify: `internal/downloader/downloader.go`
- Modify: `internal/downloader/intake.go`
- Modify: `internal/downloader/cloud_management.go`
- Modify: `internal/downloader/managed_paths.go`
- Modify: `internal/downloader/intake_test.go`
- Modify: `internal/downloader/cloud_management_test.go`
- Modify: `internal/downloader/managed_paths_test.go`

**Interfaces:**
- Produces: `startPathSensitiveOperation(context.Context) (context.Context, func(), error)` that holds the read gate for one accepted intake or cloud mutation operation, releases it together with its existing queue wait-group registration, and rejects entry while a prior async local migration still owns queue maintenance.
- `SetManagedPaths` holds the write gate through path checks, settings persistence, and any synchronous path activation.

- [x] **Step 1: Write failing concurrency tests** named `TestSetManagedPathsWaitsForIntakeOperation` and `TestPreviewIntakeRejectedDuringQueueMaintenance`; add `TestSetManagedPathsReadsCurrentPathAfterAdmission` to pin the local migration source snapshot. Channel barriers control overlap.
- [x] **Step 2: Run** `go test ./internal/downloader -run 'TestSetManagedPathsWaitsForIntakeOperation|TestPreviewIntakeRejectedDuringQueueMaintenance' -count=1`; both failed as expected before the gate was added.
- [x] **Step 3: Implement** a `sync.RWMutex` admission gate. Have `PreviewIntake`, `CommitIntake`, `RecheckIntake`, and `MutateCloud` acquire the read side through the helper. `SetManagedPaths` acquires the write side before reading current paths. New path-sensitive operations fail while an asynchronous local migration still owns queue maintenance.
- [x] **Step 4: Re-run targeted concurrency tests**; they passed on Windows. The gate tests also passed under WSL `go test -race` with Go 1.22.2.

### Task 4: Use the protected queue-path snapshot in remote-operation checks

**Files:**
- Modify: `internal/downloader/cloud_management.go`
- Modify: `internal/downloader/cloud_management_test.go`

**Interfaces:**
- Consumes: `startPathSensitiveOperation` from Task 3.
- `MutateCloud` reads `queueDir` only after acquiring the path-sensitive read gate and uses that same value for managed-directory protection and its post-operation queue reconciliation.

- [x] **Step 1: Write a failing test** named `TestMutateCloudUsesQueuePathAfterPathChangeBeforeAdmission`. A hook pauses before admission while the current queue path changes.
- [x] **Step 2: Run** `go test ./internal/downloader -run TestMutateCloudUsesQueuePathAfterPathChangeBeforeAdmission -count=1`; with the old pre-admission snapshot the request incorrectly reached Baidu and was confirmed.
- [x] **Step 3: Implement** the queue-path snapshot after acquiring the shared gate, retaining it for all path-sensitive checks and queue reconciliation in that operation.
- [x] **Step 4: Re-run the targeted test**; it passes and verifies that the changed monitor directory is protected before any remote request.

### Task 5: Show expired state in intake history and complete release verification

**Files:**
- Modify: `internal/webui/res/intake.js`
- Modify: `scripts/webui.test.cjs`
- Modify: `docs/ai/progress.md`
- Modify: `docs/ai/cloud-management-roadmap.zh-CN.md`
- Modify: `docs/ai/release-versioning.zh-CN.md`

- [x] **Step 1: Write a failing WebUI test** named `expired intake previews are labeled and expose no action`; assert `expired` is rendered as “预检已失效（未提交）” and exposes no commit or recheck action.
- [x] **Step 2: Run** `node --test --test-name-pattern='expired intake previews' scripts/webui.test.cjs`; it failed because the page rendered the record as “处理记录”.
- [x] **Step 3: Implement** the explicit `expired` label. Final progress/roadmap evidence will be written after release gates pass.
- [x] **Step 4: Run** `go test -count=1 ./...`, `go vet ./...`, `node scripts/webui.test.cjs`, `node scripts/userscript.test.cjs`, and `git diff --check`.
- [x] **Step 5: Run the repository preview package gate** for `v0.8.30-preview.101`; inspect ZIP contents, source snapshot, hashes, and ensure no private `.nas-downloader`, credentials, `.env`, or download data entered the release.
- [x] **Step 6: Update the same preview build** until all gates pass; only then create the release artifact and validation record. Do not commit or push.

## Execution Notes

- The current branch is `main`; no worktree was created, following the user's explicit worktree restriction.
- The checkout has extensive pre-existing modified and untracked files. Stage no files and preserve all unrelated changes.
- Do not perform real-account remote writes or claim NAS/SMB acceptance. Local provider fixtures and package gates are the authorized verification layer for this batch.



