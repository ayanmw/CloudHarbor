# Local Directory Migration History Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Work in the existing main checkout; do not create a worktree.

**Goal:** Browse all persisted local directory migrations and export evidence for the selected migration.

**Architecture:** Add stable paginated SQLite summary reads and ID-scoped evidence export. Expose protected GET endpoints and render an on-demand history list beside the current migration progress panel.

**Tech Stack:** Go, SQLite, `database/sql`, `net/http` ServeMux path values, server-rendered HTML/JavaScript, Node `node:test`.

**Spec:** `docs/superpowers/specs/2026-10-30-local-migration-history.zh-CN.md`

## Global Constraints

- SQLite only; do not touch source/target directories or Baidu APIs.
- Stable order is `started_at_utc DESC, migration_id DESC`.
- Maximum page size is 50; retain the 100-event all-or-nothing export limit.
- Export evidence for the requested ID exactly; never substitute the latest migration.
- No history mutation, deletion, retry, or unknown-result release endpoint.

## Review Focus

- Tied timestamps still produce stable pagination.
- Offset/limit validation rejects negative and over-limit input.
- Unknown migration IDs return 404 and never export another record.
- An older selected record remains selected if a newer migration is created concurrently.
- Sensitive evidence fields stay masked and export remains no-store.

---

### Task 1: Store list and ID-scoped evidence

**Files:** `internal/taskstore/local_migrations.go`, `internal/taskstore/local_migrations_test.go`

- [x] Add failing tests for stable descending pagination and selected-ID evidence export.
- [x] Add Store methods to page summaries and load a bounded read-only evidence snapshot by ID.
- [x] Cover empty history, invalid page arguments, unknown ID, and over-limit event history.

### Task 2: Protected API and history UI

**Files:** `internal/downloader/local_migration.go`, `internal/intakeapi/server.go`, `internal/intakeapi/server_test.go`, `internal/intakeapi/local_migration_export_test.go`, `main.go`, `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`

- [x] Add `GET /api/settings/local-migrations?offset=&limit=` with stable summaries and page metadata.
- [x] Add `GET /api/settings/local-migrations/{id}/export`; test exact ID, unknown ID, redaction, and attachment headers.
- [x] Add manual history refresh/paging and per-row export controls; verify response ordering and stale responses.

### Task 3: Release verification

**Files:** `docs/ai/progress.md`, `docs/ai/release-versioning.zh-CN.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`

- [x] Run full Go tests, vet, WebUI/userscript suites, and diff checks.
- [x] Build `.67` and verify all platform binaries, ZIP checksums, version, and private-data exclusions.
- [x] Record release hash and unresolved NAS/SMB acceptance in `v0.8.30-preview.67-validation.md`.
