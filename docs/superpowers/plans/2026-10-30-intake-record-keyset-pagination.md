# 手动入队记录稳定分页实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent new intake records from shifting later history pages and causing duplicate or skipped records.

**Architecture:** Preserve existing offset pagination for compatible callers and add an opaque keyset cursor based on immutable `(Created, ID)` ordering. The UI stores cursors for visited pages and resets the stack when filters or the list are refreshed.

**Tech Stack:** Go, `net/http`, existing embedded JavaScript UI, Node `node:test`.

**Spec:** `docs/superpowers/specs/2026-10-30-intake-record-keyset-pagination.zh-CN.md`

## Global Constraints

- Keep existing intake records, operation semantics, and offset API compatibility.
- Do not add a database migration or a remote write operation.
- Cursor results remain dynamically filtered; do not claim a cross-request snapshot.
- Preserve user changes elsewhere in the dirty checkout and do not use a worktree.

## Review Focus

- Same-timestamp records: deterministic ID tie-breaker and no boundary duplicates.
- Head insertions between pages: next page remains anchored to the last item returned.
- Unresolved filter changes: reset cursor stack and do not mix pages across filters.
- Malformed/forged cursor values: bounded validation and HTTP 400.
- Back navigation after refresh: use the visited cursor stack, not a computed offset.

---

### Task 1: Add stable record ordering and cursor API

**Files:**
- Modify: `internal/downloader/intake_store.go`
- Modify: `internal/intakeapi/server.go`
- Test: `internal/intakeapi/server_test.go`

- [x] Add a RED handler regression with over 20 records, request page one, insert a newer record, request page two using `next_cursor`, and assert the original boundary is stable.
- [x] Add tests for same-created-time ID ordering, malformed/duplicate/conflicting cursor rejection, and preserved offset behavior.
- [x] Implement stable `(Created DESC, ID DESC)` ordering in `GetIntakes` and the API response.
- [x] Implement a bounded, canonical base64url cursor carrying the last record's Created timestamp and ID; filter after that tuple and return `next_cursor` when more rows exist.
- [x] Run focused intake API tests; the new head-insertion regression failed before implementation and passed after.

### Task 2: Switch intake history UI to cursor navigation

**Files:**
- Modify: `internal/webui/res/intake.js`
- Test: `scripts/webui.test.cjs`

- [x] Add a RED page test proving next-page requests carry `next_cursor`; inserting a record before navigation does not change the cursor used.
- [x] Add a RED page test for previous-page cursor stack and reset after unresolved-filter change.
- [x] Add a regression showing a failed next-page request does not commit its cursor to browser history; retry and back navigation keep the original page boundary.
- [x] Implement first-page refresh, next-cursor push, and prior-cursor pop while keeping request-generation stale-response protection.
- [x] Run the focused WebUI test; it failed before implementation and passed afterward.

### Task 3: Update the unnumbered roadmap batch

**Files:**
- Modify: `docs/ai/cloud-management-roadmap.zh-CN.md`
- Modify: `docs/ai/release-versioning.zh-CN.md`
- Modify: `docs/ai/progress.md`

- [x] Record this as one distinct Bug fix in the shared unnumbered candidate batch; do not assign `.95` on its own.
- [x] Run full Go tests, vet, WebUI/userscript tests and `git diff --check` after implementation.
- [x] Continue collecting separate user-visible items until the candidate batch reaches five; only then number and package one preview. The bundled batch was delivered as `v0.8.30-preview.95`.
