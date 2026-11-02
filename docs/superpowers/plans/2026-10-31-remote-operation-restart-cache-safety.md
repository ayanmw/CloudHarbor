# Remote Operation Restart Cache Safety Implementation Plan

> **For agentic workers:** Use the inline executor in the existing checkout; do not create a worktree. Steps use checkbox syntax for tracking.

**Goal:** When an interrupted remote mutation becomes `unknown` at SQLite reopen, durably invalidate the local cache paths it may have changed.

**Architecture:** Extend the existing `recoverRemoteOperations` transaction. Read the account and source/destination paths for prepared rows, convert each to unknown, append a UTC event with affected paths, mark matching directory coverage stale, and bump each affected account's inventory generation before one transaction commit. Preserve all cached inventory rows and hashes.

**Tech Stack:** Go, `modernc.org/sqlite`, existing taskstore recovery and cache invalidation helpers.

**Spec:** `docs/superpowers/specs/2026-10-31-remote-operation-restart-cache-safety.zh-CN.md`

## Global Constraints

- Keep `prepared` recovery fail-closed as `unknown`; never replay a remote mutation.
- Do not delete or rewrite cached inventory identities during uncertain recovery.
- Make operation-state, recovery event, cache coverage invalidation, and generation update one SQLite transaction.
- Preserve account isolation and distinguish UTC event recording from cloud observation; no cloud observation exists during restart recovery.
- Use the primary checkout; no worktree, commit, push, live cloud mutation, or NAS/SMB test.

## Review Focus

- File rename/delete: parent cache coverage becomes stale, but the file row/hash stays intact.
- Directory move/delete: source and target parent coverage plus existing source/target subtree coverage become stale.
- Create/mkdir: destination parent and destination directory coverage are invalidated when present.
- Multiple prepared operations for one account: generation is advanced transactionally without invalidating other account scopes.
- SQLite statement/trigger failure: status, events, coverage, and generation all roll back.

---

### Task 1: Reproduce stale-complete coverage after restart

**Files:** `internal/taskstore/remote_operations_test.go`

- [x] Seed complete directory coverage for an account and a separate account, create a prepared directory operation, close and reopen the SQLite store.
- [x] Assert the old behavior: the operation became unknown while affected directory coverage incorrectly remained complete. Confirmed the regression failed before changing production code.

### Task 2: Atomically invalidate affected coverage during recovery

**Files:** `internal/taskstore/remote_operations.go`, `internal/taskstore/remote_operations_test.go`

- [x] Extend recovery query data to include account fingerprint and source/destination paths.
- [x] Derive the affected parent and exact paths; invalidate matching existing coverage rows and descendants with the existing cloud path range helper.
- [x] Append recovery evidence containing the invalidated paths; the recovery event stores the local UTC record time. Advance inventory generation for affected accounts.
- [x] Verify cached file rows/hashes remain unchanged and another account's coverage remains complete.

### Task 3: Prove atomic rollback and reopen behavior

**Files:** `internal/taskstore/remote_operations_test.go`

- [x] Inject an SQLite trigger failure during recovery invalidation and assert the recovery transaction rolls back operation status, events, coverage, and generation.
- [x] Remove the injected failure and retry recovery; verify the prepared operation becomes unknown and affected coverage becomes stale.
- [x] Run focused recovery tests, full Go tests/vet, WebUI/userscript suites, and `git diff --check` before counting this result.

## Completion Contract

- The regression fails on current code and passes with the atomic recovery implementation.
- No remote API is called and no source operation is replayed.
- Full local verification passes. This is candidate item 1/5 only; do not package or assign a preview version until five distinct user outcomes are complete.
