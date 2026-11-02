# Global Task Event History Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an authenticated, read-only global task event history page with fixed-snapshot pagination, combined filters, type counts, and bounded redacted export.

**Architecture:** Query immutable `job_events` in SQLite. A first request captures the maximum event sequence and returns page/count/type counts from one read-only transaction; later pages retain that upper sequence. Add indexes in schema v39 and a standalone WebUI route and asset.

**Tech Stack:** Go, `database/sql` with SQLite, `net/http`, embedded HTML/JS, Node `node:test`.

**Spec:** `docs/superpowers/specs/2026-11-02-global-task-event-history.zh-CN.md`

## Global Constraints

- Keep all endpoints behind the existing management session gate.
- The history page must not call Baidu APIs or mutate jobs, queue state, cache, or remote files.
- Time filters use local `recorded_at_utc`, UTC, left-closed/right-open; date and exact-time ranges are mutually exclusive.
- Keep export all-or-nothing at 1,000 events and recursively redact evidence before JSON serialization.
- Schema v39 adds indexes only; preserve all historical task rows and events.
- Do not use a Git worktree or touch unrelated dirty files.

## Review Focus

- A new event is appended between page requests: ensure the fixed `as_of_sequence` keeps rows and totals stable.
- Unknown historical event types: aggregate them under `other` without exposing an untrusted filter option.
- Invalid cursor values and cross-snapshot cursors: reject before query and return no partial data.
- Filters combined with export: use the same parser and sequence upper bound as the displayed query.
- Authentication loss or late UI responses: use existing session-expired handling and request IDs.

---

### Task 1: SQLite v39 indexes and global history queries

**Files:** `internal/taskstore/sqlite.go`, `internal/taskstore/store.go`, `internal/taskstore/migration.go`, new `internal/taskstore/task_event_history.go`, new `internal/taskstore/task_event_history_test.go`, `internal/taskstore/migration_test.go`.

- [x] Add failing tests for combined filters, unknown type counting, first/last pages, an appended event between pages, invalid bounds, and export cap behavior.
- [x] Add migration tests for v38→v39, manifest recovery after SQLite commit, schema/index validation, and unchanged event rows.
- [x] Implement migration and index allowlist for task ID/sequence, event type/sequence, operation ID/sequence, attempt ID/sequence, and recorded time/sequence. Confirm `EXPLAIN QUERY PLAN` uses the intended indexes for isolated filters.
- [x] Add `GlobalTaskEventFilter`, page/count DTO and methods `TaskEventHistoryPage` and `TaskEventHistoryExport`. Capture/read `as_of_sequence`, total, type totals, and events within one read-only transaction; reject future or mismatched cursors, including pinned empty snapshot 0.
- [x] Run taskstore and migration tests.

### Task 2: Authenticated API and redacted export

**Files:** `main.go`, `main_test.go`, `internal/downloader/downloader.go`.

- [x] Add API tests for GET-only behavior, combined filters, fixed snapshot pagination, invalid parameters, redaction, 1,000/1,001 export boundary and `no-store`.
- [x] Implement `/api/task-events` and `/api/task-events/export`; require the existing global gate, validate decimal query fields, share filter parsing, and recursively redact evidence.
- [x] Verify events, counts and export contain no account fingerprint/scope fields and have stable export metadata.

### Task 3: Standalone task event history page

**Files:** new `internal/webui/index_task_events.html`, new `internal/webui/res/task_event_history.js`, `internal/webui/webui.go`, `main.go`, `internal/webui/index_poll.html`, `internal/webui/index_ws.html`, `scripts/webui.test.cjs`.

- [x] Add UI tests for the standalone page controls, escaping untrusted task IDs/evidence, and both navigation links.
- [x] Implement task ID, event kind, operation ID, attempt ID, UTC date and exact UTC filters; preserve conditions for type shortcuts and export.
- [x] Keep `as_of_sequence` and filter values across older/newer paging; use request IDs to discard stale page/context responses and show auth/read errors.
- [x] Display event UTC and separate provider/response timestamps; show bounded evidence and export status without rendering raw HTML.
- [x] Run WebUI (172/172) and userscript (3/3) checks.

### Task 4: Documentation and preview package

**Files:** `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/release-versioning.zh-CN.md`, `docs/ai/progress.md`, this spec and plan, releases validation record.

- [x] Record the five user results after delivery.
- [x] Run `go test ./...`, `go vet ./...`, UI checks and the preview packaging gate. One initial concurrent run exposed a stale v38 test assertion and a timing failure under duplicate suite contention; the assertion was corrected, the timing test passed in isolation, and the package's sequential full gate passed.
- [x] Verify package version, 13 ZIP entries, all 12 payload hashes, source snapshot, and absence of private runtime state in the package.
- [x] Verify `.146` readiness and zero tasks, stop it, move the sole real `.nas-downloader` directory into `.147`, compare all 43 file hashes, then start `.147` and perform read-only SQLite/auth/health/page checks.
- [x] Do not request a full cloud scan, download, or remote mutation during runtime handoff.
