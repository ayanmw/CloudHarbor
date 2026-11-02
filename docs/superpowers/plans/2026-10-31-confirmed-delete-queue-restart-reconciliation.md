# 已确认网盘删除后的本地队列重启对账计划

> Inline in the primary checkout. No worktree, commit, push, live Baidu operation, or NAS/SMB test.

**Goal:** Restore a locally uncommitted queue cancellation from an exact durable confirmed delete receipt after restart.

**Architecture:** Add a taskstore lookup for a confirmed delete matching account, fs_id, exact source path, and operation ID without an existing linked queue event. During a successful authorized monitor-directory reconciliation only, convert a still-unstarted missing queue job to cancelled and append its task event transactionally. Keep all other missing/unknown identities on the current fail-closed path.

**Tech Stack:** Go, SQLite, downloader scheduler and remote-operation journal.

**Spec:** `docs/superpowers/specs/2026-10-31-confirmed-delete-queue-restart-reconciliation.zh-CN.md`

## Global Constraints

- Never repeat a provider mutation during local recovery.
- Require exact account fingerprint, fs_id, and source path from the confirmed remote-operation journal.
- Only reconcile after a successful complete monitor-directory read under the current authentication epoch.
- Change only an inactive, unstarted root job; keep task history, mode, attempts, and identity intact.
- Keep task-state and linked event atomic; preserve all unrelated existing worktree changes.

## Review Focus

- A complete directory scan omits the deleted item but the remote journal is confirmed: cancel locally with the original operation evidence.
- A source with a different fs_id or source path must remain on the existing `source_missing`/identity path.
- `unknown`, `failed`, active, processing, and terminal jobs must not be rewritten.
- A failed task-event insert must preserve the previous job projection and be retriable after restart.
- Repeated reconciliation must not duplicate the linked delete event or send another remote request.

---

### Task 1: Reproduce the restart mismatch

**Files:** `internal/downloader/cloud_management_test.go`

- [ ] Seed a pending root task and install an SQLite trigger that rejects only `task_source_deleted_remotely` inserts.
- [ ] Run a mocked synchronous delete that removes the provider item; assert the remote journal is confirmed, the task remains pending, and the mutation is called once.
- [ ] Shut down and reopen `Downloader`; run a successful empty monitor scan and assert the current behavior records `failed/task_source_missing` instead of cancellation.

### Task 2: Query exact durable deletion evidence

**Files:** `internal/taskstore/remote_operations.go`, `internal/taskstore/remote_operations_test.go`

- [ ] Add a store query for an unreconciled confirmed delete matching account, object ID, and exact source path, returning operation ID and response observation time.
- [ ] Exclude operations that already have the matching `task_source_deleted_remotely` event.
- [ ] Test account, kind, status, fs_id, source path, response time, and already-reconciled boundaries.

### Task 3: Reconcile only eligible missing tasks

**Files:** `internal/downloader/scheduler.go`, `internal/downloader/cloud_management_test.go`

- [ ] Before recording `task_source_missing`, consult the confirmed-delete journal only for missing jobs in pending/auth_wait with zero attempts/failures and no active worker or source operation; retry_wait is excluded.
- [ ] Persist `cancelled` plus `task_source_deleted_remotely` with the durable operation ID, response UTC, previous status, and original identity in one existing job/event transaction.
- [ ] Preserve normal source-missing behavior for nonmatches; verify retries do not duplicate events and provider calls remain one.

### Task 4: Verify and update the existing preview

- [ ] Run focused downloader/taskstore regressions, full serial Go tests/vet, WebUI/userscript suites, and `git diff --check`.
- [ ] Update v0.8.30-preview.114 docs and validation record, then run `scripts/package.ps1 -Version v0.8.30-preview.114 -AllowDirty -UpdatePreview` and verify source snapshot, package hashes, three targets, and Windows version.

## Completion

This fixes one local recovery result within the delivered `.114` batch; it does not create a new preview item or version. Do not change any nonmatching queue task or call Baidu during recovery.
