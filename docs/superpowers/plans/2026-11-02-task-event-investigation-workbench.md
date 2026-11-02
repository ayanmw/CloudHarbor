# Task Event Investigation Workbench Implementation Plan

> **For agentic workers:** execute inline in the primary checkout; do not create a Git worktree. Use TDD for each behavior.

**Goal:** Improve read-only per-task event investigation with exact UTC bounds, bounded event context, type totals, quick day filtering, and safe evidence copying.

**Architecture:** Extend existing task-scoped SQLite event queries with parameterized UTC bounds and same-snapshot type summaries. Add a fixed-size sequence-context endpoint and UI actions that reuse the current task event panel, date/ID filters, and recursive evidence redaction.

**Tech Stack:** Go, SQLite, `net/http`, embedded HTML/JavaScript, Node WebUI tests.

**Spec:** `docs/superpowers/specs/2026-11-02-task-event-investigation-workbench.zh-CN.md`

## Global Constraints

- Use only the existing primary checkout and preserve unrelated dirty work.
- All new event reads use task ID scope and SQLite read-only transactions.
- UTC event filters apply only to `recorded_at_utc`.
- Context response is fixed at no more than 11 events and cannot be expanded by callers.
- Sensitive event evidence uses existing recursive redaction.
- No schema change, Baidu call, download, or remote mutation.

## Review Focus

- Exact time boundary and date/time filter mixing: Store/API tests reject ambiguity and prove half-open UTC behavior.
- Event context crosses task boundaries or grows unbounded: Store/API tests seed adjacent tasks and oversized histories.
- Type totals drift from the displayed page: one SQLite snapshot test checks counts while writers are blocked/transactions are scoped.
- UI stale responses overwrite a newly selected context: Node test delays an old context response.
- Clipboard rejection leaks into state or breaks filtering: Node test covers rejected clipboard API and visible error.

---

### Task 1: Exact UTC time bounds and type totals

**Files:** `internal/taskstore/task_event_filter.go`, `internal/taskstore/task_event_filter_test.go`, `internal/downloader/downloader.go`, `main.go`, `main_test.go`.

- [x] Add tests for left-closed/right-open RFC3339 UTC bounds, invalid and mixed bounds, and type counts under the same non-kind filters.
- [x] Run focused tests and verify the new cases fail for missing fields/results.
- [x] Extend the filter parser and read query. Keep existing date-only query behavior. Return type counts with the filtered page from one read-only transaction.
- [x] Verify Store and API tests, including exact time filters on evidence export.

### Task 2: Bounded event context API

**Files:** `internal/taskstore/task_event_filter.go`, `internal/taskstore/task_event_filter_test.go`, `internal/downloader/downloader.go`, `main.go`, `main_test.go`.

- [x] Add tests for anchor plus at most five older/newer events, sequence order, first/last event edges, missing anchor, and task isolation.
- [x] Run focused tests and verify they fail before implementation.
- [x] Add one task-scoped API route and a single read-only SQLite transaction; reject invalid IDs and do not accept a caller-supplied context size.
- [x] Verify API status/body and sensitive evidence masking.

### Task 3: Investigation UI actions

**Files:** `internal/webui/res/shared.js`, `internal/webui/res/shared.css`, `scripts/webui.test.cjs`.

- [x] Add UI cases for exact UTC bounds, preserving them during paging/refresh/export, and no mixing with date bounds.
- [x] Add UI cases for type-count quick filtering, per-event UTC-day shortcut, bounded context display, and safe evidence copy success/failure.
- [x] Implement minimal UI state/actions. Context results use a separate request sequence so stale responses cannot replace newer context.
- [x] Run WebUI and userscript checks.

### Task 4: Documentation and release candidate

**Files:** `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/release-versioning.zh-CN.md`, `docs/ai/progress.md`, this spec and plan, releases validation record.

- [x] Record five independently testable outcomes and their acceptance evidence.
- [x] Run `go test ./... -count=1`, `go vet ./...`, WebUI/userscript tests, and `scripts/package.ps1 -Version v0.8.30-preview.146`.
- [x] Verify ZIP payload hashes, source snapshot, version output, and SHA-256; confirm package contains no `.nas-downloader` state.
- [x] Stop `.145` and move the sole real state directory to `.146`; verify pre/post file hashes, database integrity, auth/health, and local UI only. No full cloud scan or remote write was run as part of the switch.
