# Read-only Cloud Retry Reliability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Pace every metadata GET and make retry deadlines for inventory scans survive restarts with auditable UTC evidence.

**Architecture:** Keep the existing per-Downloader shared pacer and move its call to the shared metadata GET helper. Extend API errors and SQLite scan checkpoints with bounded retry timing; scanners resume from persisted UTC deadlines and expose the schedule through their existing status surfaces.

**Tech Stack:** Go, `net/http`, SQLite (`modernc.org/sqlite`), embedded WebUI JavaScript.

**Spec:** `docs/superpowers/specs/2026-10-31-readonly-cloud-retry-reliability.zh-CN.md`

## Global Constraints

- Never automatically replay a remote write or change `unknown` handling.
- Keep Baidu observation time separate from local retry scheduling time.
- Persist retry timing before sleeping and preserve it across cancellation/restart.
- Bound server-provided waits and keep request pacing cancellation-aware.
- Do not use a Git worktree; preserve existing dirty changes.

## Review Focus

- Metadata GET cancellation while queued at the pacer must not send a request — test at `internal/baiduapi`.
- File content download must not consume metadata API pacing — test request paths independently.
- Malformed, past, and extreme Retry-After values must not create negative or unbounded waits — test parser and cap.
- A restart between saving retry state and waiting must not bypass the persisted UTC deadline — test both scan workers with reopened SQLite.
- Existing schema-22 databases and interrupted manifest/version switches must migrate or fail closed — test migration recovery and integrity.

### Task 1: Apply shared pacing to all metadata GET calls

**Files:** `internal/baiduapi/client.go`, `internal/baiduapi/client_test.go`

- [x] Add failing test asserting `GetFileInfo`, `GetDownloadLink`, and `UserID` each invoke one shared pacer.
- [x] Verify the test fails because the pacer count is zero.
- [x] Move pacing from `listDirPage` into `Client.get`; keep file data download outside this helper.
- [x] Run focused `go test ./internal/baiduapi -run 'TestClientUsesSharedRequestPacer' -count=1`.

### Task 2: Preserve and apply Retry-After

**Files:** `internal/baiduapi/client.go`, relevant API tests, `internal/downloader/cloud_inventory_scan.go`.

- [x] Add failing parser tests for integer seconds, HTTP date, malformed, past and capped values.
- [x] Parse `Retry-After` on retryable HTTP status errors without persisting response bodies or credentials.
- [x] Add a bounded delay helper taking the greater of exponential and provider delay, capped at one hour.
- [x] Verify scanner retry calculations with deterministic injected time or a pure helper.

### Task 3: Persist full-scan retry schedule

**Files:** `internal/taskstore/cloud_inventory.go`, `internal/taskstore/migration.go`, `internal/taskstore/migration_test.go`, `internal/downloader/cloud_inventory_scan.go`, corresponding tests.

- [x] Add failing store tests for saving/loading retry delay and UTC deadline across `Close`/`Open`.
- [x] Add failing worker tests proving future deadline blocks provider request and elapsed deadline resumes immediately.
- [x] Implement schema 22→23 transactional migration with nullable deadline and nonnegative bounded delay.
- [x] Preserve prior failed cause and retry schedule when pausing/restarting; clear schedule only after successful page publication.

### Task 4: Persist local-refresh retry schedule

**Files:** same SQLite state/migration files and local-refresh worker/tests.

- [x] Add equivalent but refresh-run-isolated persistence tests using the refresh account/run identity.
- [x] Ensure refresh checkpoint state and retry deadline update together in the same SQLite row update.
- [x] Test resume after database reopen and verify no API call occurs before deadline.

### Task 5: Expose retry timing to operators

**Files:** `internal/taskstore/cloud_inventory.go`, authenticated status API/UI and tests.

- [x] Add JSON fields for delay milliseconds and optional `retry_at_utc` on both status models.
- [x] Show scheduled UTC retry time and remaining local wait in scan status views; label it as a local schedule.
- [x] Verify unknown/error redaction, account isolation, stale-response protection, and empty-state behavior.

### Task 6: Clear recovered transient errors

**Files:** `internal/taskstore/cloud_inventory.go`, `internal/taskstore/cloud_inventory_test.go`

- [x] Add a regression test where a successful page follows a persisted retry error and deadline.
- [x] Clear stale `LastError` and retry schedule in the same page publication transaction for both scan types.
- [x] Verify state and UI no longer report a recovered failure after successful publication.

### Final verification and packaging

- [x] Run `go test -count=1 ./...` and `go vet ./...`.
- [x] Run WebUI/userscript checks and three target builds.
- [x] Package only after six independent deliverables pass; verify archive hash list, source snapshot, no private runtime data, and Windows `--version`.
- [x] Write a release validation record with local-vs-external acceptance boundaries.
