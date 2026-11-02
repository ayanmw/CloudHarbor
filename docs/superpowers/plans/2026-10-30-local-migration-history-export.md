# Local Directory Migration History Summary Export Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Work in the existing main checkout; do not create a worktree.

**Goal:** Export all migration summaries matching the current history filters from one bounded SQLite snapshot.

**Architecture:** Reuse the validated local migration filter and SQL predicate from `.68`; add a 5,000-row all-or-nothing Store export, protected JSON attachment endpoint, and a user-click export control using current filters.

**Tech Stack:** Go, SQLite, `database/sql`, `net/http`, server-rendered HTML/JavaScript, Node `node:test`.

**Spec:** `docs/superpowers/specs/2026-10-30-local-migration-history-export.zh-CN.md`

## Global Constraints

- Reuse `.68` status and path query semantics exactly.
- Use one SQLite read-only snapshot; stable order is `started_at_utc DESC, migration_id DESC`.
- Export at most 5,000 summaries; the 5,001st match rejects the entire export.
- Do not include per-migration events; keep ID-specific event export unchanged.
- No Baidu API or migration-directory IO.

## Review Focus

- Count and export rows come from one read-only snapshot.
- Exactly 5,000 rows are allowed; 5,001 are rejected without partial output.
- Filter semantics and SQL wildcard escaping match `.68`.
- Export button reads current status/query controls, independent of current page.
- Error responses do not leak internal SQLite details and carry no-store.

---

### Task 1: Bounded Store summary export

**Files:** `internal/taskstore/local_migrations.go`, `internal/taskstore/local_migrations_test.go`

- [x] Add failing tests for filtered export ordering, empty results, exact 5,000 limit, and 5,001 rejection.
- [x] Add an all-or-nothing bounded export method reusing the existing filter predicate.
- [x] Run focused tests and all `internal/taskstore` tests.

### Task 2: API attachment and UI action

**Files:** `internal/downloader/local_migration.go`, `internal/intakeapi/server.go`, `internal/intakeapi/server_test.go`, `internal/intakeapi/local_migration_history_test.go`, `main.go`, `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`

- [x] Add protected GET `/api/settings/local-migrations/export` using the same status/query validation.
- [x] Test success attachment, UTC metadata, no-store, invalid filters, oversize rejection, and sanitized errors.
- [x] Add a click-only export action; send current filter controls and download the JSON response.
- [x] Test that changing a filter affects the export URL and that failures stay visible.

### Task 3: Release verification and roadmap

**Files:** `docs/ai/progress.md`, `docs/ai/release-versioning.zh-CN.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`

- [x] Run full Go tests, vet, WebUI/userscript suites, and diff checks.
- [x] Build `.69`; verify platform binaries, version, ZIP manifest, and private-data exclusions.
- [x] Record release hash and external acceptance boundaries in `v0.8.30-preview.69-validation.md`.
