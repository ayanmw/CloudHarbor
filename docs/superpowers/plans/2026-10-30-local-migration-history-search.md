# Local Directory Migration History Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Work in the existing main checkout; do not create a worktree.

**Goal:** Search local directory migration history by status and source/target path while preserving bounded stable pagination and exact-ID evidence export.

**Architecture:** Add optional validated filters to a read-only SQLite page query; keep count and rows in one snapshot. Extend the protected API and the existing migration history controls without changing the selected-ID export route.

**Tech Stack:** Go, SQLite, `database/sql`, `net/http`, server-rendered HTML/JavaScript, Node `node:test`.

**Spec:** `docs/superpowers/specs/2026-10-30-local-migration-history-search.zh-CN.md`

## Global Constraints

- Allowed statuses are exactly `running`, `failed`, and `completed`.
- Trim `q`; reject invalid UTF-8 or more than 256 bytes; empty means no path filter.
- Match literal text in source or target path; escape `%`, `_`, and `\` for SQL LIKE.
- Preserve `started_at_utc DESC, migration_id DESC`, limit 1..50, and exact-ID export.
- Read SQLite only; no schema, filesystem, provider, retry, resume, or history mutation.

## Review Focus

- SQL wildcard characters in `q` match literally.
- A status and path query combine with AND while source/target use OR.
- Count and page stay in one read-only snapshot and retain stable ordering.
- Filter changes reset the UI to offset 0 while stale responses cannot replace current results.
- Export remains bound to the clicked row's migration ID.

---

### Task 1: Filtered SQLite history page

**Files:** `internal/taskstore/local_migrations.go`, `internal/taskstore/local_migrations_test.go`

- [x] Write failing Store tests for status, source/target path substring, combined filters, literal wildcard characters, empty results, and stable filtered pagination.
- [x] Add filter type and validation; extend list query so count and rows share one read-only transaction and existing sort/page bounds.
- [x] Run focused Store tests and full `internal/taskstore` tests.

### Task 2: Protected API and history controls

**Files:** `internal/downloader/local_migration.go`, `internal/intakeapi/server.go`, `internal/intakeapi/server_test.go`, `internal/intakeapi/local_migration_history_test.go`, `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`

- [x] Add `status` and `q` to the existing `GET /api/settings/local-migrations` query; reject invalid status, invalid UTF-8, and more than 256 bytes with HTTP 400.
- [x] Test filter forwarding, validation, no cloud calls, and response metadata.
- [x] Add status and path search controls; filter submits reset offset, while refresh and paging preserve current filters.
- [x] Verify stale result suppression and that per-row evidence export keeps the selected ID.

### Task 3: Release verification and roadmap

**Files:** `docs/ai/progress.md`, `docs/ai/release-versioning.zh-CN.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`

- [x] Run full Go tests, vet, WebUI/userscript suites, and diff checks.
- [x] Build `.68`; verify platform binaries, version, ZIP manifest, and private-data exclusions.
- [x] Record release hash and external acceptance boundaries in `v0.8.30-preview.68-validation.md`.
