# Local Directory Migration Evidence Export Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Implement in the existing checkout; do not create a worktree.

**Goal:** Export the latest local directory migration summary and its durable events as a bounded JSON attachment.

**Architecture:** Add a read-only SQLite snapshot method that loads the latest migration and up to 100 append-only events in one transaction. Expose it through the authenticated intake API and a user-clicked download button in the path settings panel.

**Tech Stack:** Go, SQLite, `net/http`, server-rendered HTML, browser JavaScript, Node `node:test`.

**Spec:** `docs/superpowers/specs/2026-10-30-local-migration-evidence-export.zh-CN.md`

## Global Constraints

- SQLite reads only; no Baidu API or local directory I/O.
- One transaction for migration summary and event rows.
- At most 100 events; reject the whole export above that limit.
- No migration record returns 404; API is GET-only and admin-protected.
- No worktree, unknown-operation release path, or mutation route.

## Review Focus

- Events remain sequence-ascending with explicit UTC timestamps.
- Summary and events must come from one SQLite snapshot.
- Event count 101 is rejected, never truncated.
- The export contains no OAuth/admin credentials or account fingerprint.
- Browser downloads only after a direct user click and surfaces server errors.

---

### Task 1: Store evidence snapshot

**Files:** `internal/taskstore/local_migrations.go`, `internal/taskstore/local_migrations_test.go`

- [x] Add tests creating a migration and two phase events; assert summary, ascending sequence, UTC times and evidence after closing/reopening the SQLite store.
- [x] Verify the test fails before the export method exists.
- [x] Implement a read-only transactional export method and `ErrLocalMigrationEvidenceTooLarge` at 100 events.
- [x] Test no rows, 101 events, and closed store behavior.

### Task 2: API and download UI

**Files:** `internal/downloader/local_migration.go`, `internal/intakeapi/server.go`, `internal/intakeapi/server_test.go`, `main.go`, `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`

- [x] Add downloader façade and GET export handler; test attachment, 404, 413, credential masking, and no mutation/provider calls.
- [x] Add authenticated `/api/settings/local-migration/export` route behind the existing management gate.
- [x] Add click-triggered JSON download beside migration progress; test same-origin credentials and error rendering.

### Task 3: Release verification

**Files:** `docs/ai/progress.md`, `docs/ai/release-versioning.zh-CN.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`

- [x] Run full Go tests, vet, WebUI/userscript, and diff checks.
- [x] Build `.66` and verify three platform binaries, ZIP checksums, package version, and private-data exclusions.
- [x] Record artifact hash and note that real NAS/SMB semantics remain unverified.
