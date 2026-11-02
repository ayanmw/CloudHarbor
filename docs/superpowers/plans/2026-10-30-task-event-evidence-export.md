# Download Task Event Evidence Export Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a user-triggered, bounded, redacted JSON export of one task's local event history.

**Architecture:** The task store reads one job's complete event history in a SQLite read-only transaction and refuses exports over 100 events. A protected GET API emits an attachment after recursive evidence masking; the task timeline adds an explicit download action.

**Tech Stack:** Go, SQLite (`modernc.org/sqlite`), `net/http`, embedded JavaScript, Node WebUI tests.

**Spec:** `docs/superpowers/specs/2026-10-30-task-event-evidence-export.zh-CN.md`

## Global Constraints

- Keep the queue SQLite append-only event semantics and use a read-only transaction for the export snapshot.
- Do not call provider APIs or mutate queue/task state during export.
- Reject the whole export when more than 100 events exist.
- Preserve existing admin session protection, recursive masking key rules, and no-store behavior.
- Do not create or use a Git worktree; preserve all existing dirty work.

## Review Focus

- Missing task versus task with no events: verify 404 versus a valid empty export.
- Event count at 100 and 101: verify full export versus all-or-nothing 413.
- Nested secrets in maps and arrays: verify all configured key markers are masked.
- Event insertion concurrent with export: verify the returned event count and rows share one snapshot.
- Browser double-click/auth expiry: verify one download request and safe visible errors.

### Task 1: Bounded SQLite task event snapshot

**Files:** Modify `internal/taskstore/store.go`; test `internal/taskstore/store_test.go`.

- [x] Add failing tests for empty/missing task, ascending events, the 100/101 boundary, and snapshot consistency.
- [x] Run the targeted taskstore tests and confirm expected failures.
- [x] Add `TaskEventEvidenceExportLimit`, `ErrTaskEventEvidenceExportTooLarge`, and `(*Store).TaskEventEvidenceExport(ctx, id)` using a read-only transaction.
- [x] Run targeted tests and confirm all store cases pass.

### Task 2: Protected evidence export API

**Files:** Modify `main.go`, `main_test.go`; add focused tests only if the integration harness cannot isolate behavior.

- [x] Add failing integration coverage for protected route, GET-only, invalid ID, 404, 100/101 boundary, attachment, no-store, recursive masking, and database failure without raw details.
- [x] Register `/api/tasks/{id}/events/export` before `/api/tasks/` and implement JSON attachment response.
- [x] Run focused integration tests and confirm they pass.

### Task 3: Timeline download action

**Files:** Modify `internal/webui/res/shared.js`, `scripts/webui.test.cjs`, and relevant timeline styling only if needed.

- [x] Add failing UI tests for click-triggered export, duplicate request blocking, successful attachment download, 401/413/generic errors.
- [x] Add the timeline button and bounded fetch/blob download behavior with safe Chinese errors.
- [x] Run WebUI tests and userscript tests.

### Task 4: Version docs and preview package

**Files:** Update `docs/ai/progress.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`, and `docs/ai/release-versioning.zh-CN.md`.

- [x] Record `.75` scope, checks, artifact path/hash and remaining external acceptance boundary.
- [x] Run full Go tests, vet, WebUI/userscript tests and the three-platform package gate.
- [x] Inspect ZIP contents/checksums, confirm private state is excluded and Windows `-version` reports `.75`.
