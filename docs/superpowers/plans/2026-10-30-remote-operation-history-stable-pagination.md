# Remote Operation History Stable Pagination Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent new remote operation records at the top of the list from shifting offset-based history pages.

**Architecture:** Add an account-store-bound keyset page method ordered by `(created_at_utc, operation_id)` descending. Expose an optional cursor API while preserving legacy offset requests, then move the read-only operations page to a cursor stack for next/previous navigation.

**Tech Stack:** Go, SQLite, embedded HTML/JavaScript, `node:test`.

**Spec:** `docs/superpowers/specs/2026-10-30-remote-operation-history-stable-pagination.zh-CN.md`

## Global Constraints

- History browsing remains local SQLite-only and read-only.
- Cursor and offset parameters are mutually exclusive; existing offset callers remain supported.
- Cursors bind to the existing descending `(created_at_utc, operation_id)` order.
- `total` remains a current live count, not a frozen multi-page snapshot.
- Unknown results remain quarantined; no cloud mutation controls are added.

## Review Focus

- A new record inserted before page two must not shift the cursor page boundary.
- Equal creation timestamps must use operation ID as a deterministic tiebreaker.
- Invalid or partial cursors and cursor-plus-offset requests must return 400.
- Changing filters, refreshing, and going backward must not reuse stale cursors.
- Totals may change between pages and must be labeled as live.

### Task 1: Store keyset pagination

**Files:** `internal/taskstore/remote_operations.go`, `internal/taskstore/remote_operations_test.go`

- [x] Add failing tests for descending cursor order, equal timestamps, empty/last pages, filtering, and insertion at the head between page requests.
- [x] Add `RemoteOperationCursor` and a bounded cursor-page method using a read transaction, `limit+1`, and the strict tuple predicate.
- [x] Run focused Store tests and confirm old offset tests remain unchanged.

### Task 2: API cursor compatibility

**Files:** `internal/downloader/cloud_operations.go`, `internal/intakeapi/cloud_operations.go`, `internal/intakeapi/server_test.go`, `main.go`

- [x] Add API tests for valid cursor, partial/repeated/malformed cursor, offset conflict, next-cursor response, and legacy offset pagination.
- [x] Wire an account-store-bound cursor page through the backend and parse/validate the two cursor fields.
- [x] Return a next cursor only when an extra row exists; preserve existing filter semantics and read-only error handling.
- [x] Run focused API/downloader tests.

### Task 3: History page cursor navigation

**Files:** `internal/webui/res/cloud_operations.js`, `scripts/webui.test.cjs`, `internal/webui/index_operations.html`

- [x] Add tests for next/previous cursor stack, reset after every filter/refresh, stale response handling, and a new top record between pages.
- [x] Replace page offset state with cursor history while preserving page size and filter serialization.
- [x] Display current live filtered count distinctly from the visible page range.
- [x] Run WebUI tests and `git diff --check`.

### Task 4: `.83` release and roadmap

**Files:** `docs/ai/progress.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/release-versioning.zh-CN.md`

- [x] Run full Go tests, vet, WebUI/userscript, and all three package targets.
- [x] Verify ZIP checksums, source snapshot, private-data exclusions, version, and architectures.
- [x] Record final hash, tests, and the live-count/status-change limitations in the release validation note outside the repository.
