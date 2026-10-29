# Task Event Inspection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Let an authenticated administrator inspect a task's durable SQLite event timeline from the existing task list.

**Architecture:** Add descending keyset pagination to the taskstore, expose it through Downloader and a GET-only administrator route, then add an on-demand expandable timeline to both task list pages. Keep the existing event schema and evidence semantics unchanged.

**Tech Stack:** Go, SQLite via `modernc.org/sqlite`, embedded HTML/JavaScript, existing Node test harness.

**Spec:** `docs/superpowers/specs/2026-10-28-task-event-inspection-design.zh-CN.md`

## Global Constraints

- Keep events append-only and do not change SQLite schema or task transition semantics.
- The API is read-only, task-scoped, administrator-authenticated, and bounded to at most 50 events per page.
- Keep machine-local record time, cloud/response observation times, and future plan times distinct.
- Never add event mutation, deletion, automatic pruning, unknown-operation release, or cloud-side actions.
- Preserve existing unrelated worktree changes.

## Review Focus

- Malformed or oversized IDs/cursors/limits must return 400; missing task must return 404.
- Anonymous requests must remain 401 and event API must stay behind the existing management gate.
- Keyset paging must have no duplicate or missing events when new events append between requests.
- Event evidence containing HTML markup must render as text, not executable markup.
- Empty pages and tasks with more than one page must expose correct cursor behavior.

### Task 1: Bounded SQLite and Downloader event paging

**Files:**
- Modify: `internal/taskstore/store.go`
- Modify: `internal/downloader/downloader.go`
- Test: `internal/taskstore/store_test.go`
- Test: `internal/downloader/*_test.go`

**Interface:** `Store.EventPage(jobID, beforeSequence int64, limit int) ([]Event, int64, bool, error)`, where cursor is exclusive, events are returned in ascending sequence order, next cursor is the oldest returned sequence only when older rows remain, and limit must be 1..50. Expose a matching task-scoped Downloader method.

- [x] Write a failing store test for descending keyset selection returned in ascending display order, empty history, invalid bounds, and append-between-pages stability.
- [x] Run the focused store test and confirm the expected missing API/behavior failure.
- [x] Implement the query using the `(job_id, sequence)` index, `beforeSequence` exclusion, and `limit+1` sentinel; verify the job exists so unknown tasks are distinguishable from empty histories.
- [x] Add and run a Downloader forwarding test against an opened queue restored from SQLite.
- [x] Run `go test ./internal/taskstore ./internal/downloader`.

### Task 2: Authenticated API and task-row timeline

**Files:**
- Modify: `main.go`
- Modify: `main_test.go`
- Modify: `internal/webui/index_ws.html`
- Modify: `internal/webui/index_poll.html`
- Test: `internal/webui` Node tests

- [x] Write HTTP regression tests for anonymous 401, GET success, POST 405, malformed parameters 400, missing task 404, task isolation at Store level, and next cursor.
- [x] Run the focused API tests and observe them fail before route implementation.
- [x] Implement `GET /api/tasks/{id}/events?before_sequence=&limit=` with default limit 20, maximum 50, strict positive task IDs, and JSON response `{events,next_before_sequence}`; register it under existing `gate.Wrap`.
- [x] Add delegated expand/page controls to both task list renderers. Request events only on expansion, preserve the current page across status refresh, retain only one page for one expanded task, display time fields separately, and render evidence through text nodes/escaped text.
- [x] Add frontend tests for on-demand loading, pagination, missing timestamps, and HTML-special evidence rendering.
- [x] Run focused Go and Node tests, then full `go test ./...`, `go vet ./...`, and Node suite.
- [x] Review final diff and run `git diff --check`.
