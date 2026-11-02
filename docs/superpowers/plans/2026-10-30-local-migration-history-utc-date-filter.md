# Local Migration History UTC Date Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Work in the existing checkout; do not create a worktree.

**Goal:** Filter local directory migration history by UTC start-date bounds and reuse those bounds for paging and summary export.

**Architecture:** Extend the existing migration filter through SQLite, API and UI. Normalize canonical UTC calendar dates once, compile exclusive SQL bounds against `started_at_utc`, and keep the existing stable order and single-snapshot list/export behavior.

**Tech Stack:** Go, SQLite, `database/sql`, `net/http`, server-rendered HTML/JavaScript, Node `node:test`.

**Spec:** `docs/superpowers/specs/2026-10-30-local-migration-history-utc-date-filter.zh-CN.md`

## Global Constraints

- Date parameters are canonical `YYYY-MM-DD` UTC calendar days.
- `started_from_utc` is inclusive at UTC midnight; `started_through_utc` is inclusive by day, implemented as an exclusive next-day bound.
- Reject duplicate, impossible, noncanonical, or reversed dates before querying SQLite.
- Preserve status/path filters, stable pagination, export limit, read-only SQLite, and no-store behavior.
- No local migration file operations or Baidu API calls.

## Review Focus

- End-of-day inclusivity and exact next-day exclusion at microsecond precision.
- Leap days, impossible dates, and reversed ranges.
- Filters remain consistent across list count/page, page navigation, clear, refresh, and export.
- Missing dates retain unfiltered behavior and current result ordering.

### Task 1: Store date-filter semantics

**Files:** `internal/taskstore/local_migrations.go`, `internal/taskstore/local_migrations_test.go`

- [x] Add failing tests for inclusive UTC start/end, single-sided bounds, combined status/path, stable paging, and export reuse.
- [x] Add strict date normalization and parameterized `started_at_utc` lower/upper bounds.
- [x] Run migration history Store tests and the full `internal/taskstore` package.

### Task 2: API and UI filter wiring

**Files:** `internal/intakeapi/server.go`, `internal/intakeapi/local_migration_history_test.go`, `internal/intakeapi/server_test.go`, `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`

- [x] Add strict query validation for canonical dates, duplicates, impossible days, and reversed ranges.
- [x] Test list/export pass the same conditions and preserve no-store/safe errors.
- [x] Add UTC-labeled date controls; retain values across paging/refresh, clear all filters, and use them in summary export.
- [x] Add UI tests for query generation, reset, paging/refresh retention, and export.

### Task 3: Release verification and roadmap

**Files:** `docs/ai/progress.md`, `docs/ai/release-versioning.zh-CN.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`

- [x] Run full Go tests, vet, WebUI/userscript suites, and diff checks.
- [x] Build `.71`; verify platform binaries, version, ZIP manifest, checksums, and private-data exclusions.
- [x] Record release hash and external acceptance boundaries.
