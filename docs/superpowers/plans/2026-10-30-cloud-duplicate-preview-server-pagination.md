# Cloud Duplicate Preview Server Pagination Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bound duplicate-preview responses and browser-held groups while retaining safe full-preview deletion.

**Architecture:** Add an account-scoped SQLite page query with stable SHA-256/size group ordering and count metadata from a single read transaction. The API exposes bounded pages. The browser stores only the current page and temporary keeper overrides; batch deletion repeatedly processes the first group and reloads page zero so shrinking results cannot skip groups.

**Tech Stack:** Go, SQLite, embedded HTML/JavaScript, `node:test` WebUI harness.

**Spec:** `docs/superpowers/specs/2026-10-30-cloud-duplicate-preview-server-pagination.zh-CN.md`

## Global Constraints

- Page size is bounded to 1–50.
- Each request reads current page and counts in one SQLite read transaction.
- Remote deletion remains user-confirmed and revalidates identity and SHA-256 through the existing mutation path.
- Batch deletion stops on the first failure or unknown result.
- No real-account deletion is performed for validation.

## Review Focus

- Empty pages and offsets at/beyond end return stable totals and no invalid next offset.
- Account filters cannot leak groups between Baidu accounts.
- Groups with three or more copies count every redundant item and batch processing does not skip after a group disappears.
- Cross-page keeper choices survive navigation, but stale choices are ignored when the selected file is absent.
- Concurrent preview refresh or deletion failures stop batch work rather than continuing with stale candidates.

### Task 1: Store page query

**Files:** `internal/taskstore/cloud_duplicates.go`, `internal/taskstore/cloud_duplicates_test.go`

- [x] Add failing tests for stable group pagination, total groups, total redundant candidates, boundary validation, and account isolation.
- [x] Implement page/count queries in one read transaction with deterministic order and bounded group rows.
- [x] Run focused store tests and confirm page retrieval is bounded.

### Task 2: API and backend contract

**Files:** `internal/downloader/cloud_duplicate_scan.go`, `internal/intakeapi/server.go`, `internal/intakeapi/cloud_operations.go`, relevant tests and fake backends.

- [x] Add failing API tests for defaults, valid pages, invalid/duplicate parameters, max page size, and metadata.
- [x] Replace full-preview backend method with paginated query and wire the authenticated GET route.
- [x] Run focused API/downloader tests.

### Task 3: Bounded UI navigation and deletion

**Files:** `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`

- [x] Replace full-array UI tests with paged fetch, bounds, keep-selection, stale selection, and refresh tests.
- [x] Render one page only and retain keeper overrides keyed by SHA-256/size.
- [x] Implement single-delete refresh and first-group-at-a-time batch deletion with stop-on-error behavior.
- [x] Run focused WebUI tests and userscript tests.

### Task 4: Documentation and `.81` package

**Files:** `docs/ai/progress.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/release-versioning.zh-CN.md`

- [x] Update `.81` scope and record evidence and limitations.
- [x] Run full Go tests, vet, WebUI/userscript, `git diff --check`, and package `v0.8.30-preview.81` for Windows AMD64/Linux AMD64/Linux ARM64.
- [x] Verify ZIP manifests, version, source snapshot, no private runtime data, and SHA-256; save validation evidence.
