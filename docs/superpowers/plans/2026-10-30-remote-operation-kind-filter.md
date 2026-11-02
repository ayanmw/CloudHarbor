# Remote Operation History Kind Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Keep implementation in the existing main checkout; do not create a worktree.

**Goal:** Add a bounded, local-only operation-kind filter to remote operation history and its summary export.

**Architecture:** Add a validated `Kind` field to `taskstore.RemoteOperationFilter`, filter by `remote_operations.operation_kind`, and parse the same four values at the API boundary. The history page sends the selected type for list and export requests and resets it when filters are cleared.

**Tech Stack:** Go, SQLite (`database/sql`), server-rendered HTML, browser JavaScript, Node `node:test`.

**Spec:** `docs/superpowers/specs/2026-10-30-remote-operation-kind-filter.zh-CN.md`

## Global Constraints

- Use only the existing SQLite operation history; do not call Baidu APIs.
- Accepted kinds are `mkdir`, `move`, `rename`, and `delete`; empty means all kinds.
- Keep SQL parameterized and do not add a schema migration.
- Preserve existing filter behavior and export the active filter metadata.
- Do not add a worktree, remote-operation replay, or unknown-result release path.

## Review Focus

- Unsupported kinds must be rejected by both Store and API before becoming SQL input.
- List, pagination and export must use the same selected kind.
- Clearing filters must remove the kind parameter.
- The SQL column is `operation_kind`; the API/JSON field name is `kind`.
- Type filtering must not add provider reads or mutation calls.

---

### Task 1: Store and API filter

**Files:**
- Modify: `internal/taskstore/remote_operations.go`
- Test: `internal/taskstore/remote_operations_test.go`
- Modify: `internal/intakeapi/cloud_operations.go`
- Test: `internal/intakeapi/cloud_operations_test.go`

**Interfaces:**
- `RemoteOperationFilter.Kind string`
- Accepted values: `mkdir`, `move`, `rename`, `delete`
- SQL column: `operation_kind`

- [x] Add Store coverage to `TestRemoteOperationFilteredPageSearchesLiteralFieldsAndTimeBounds` and `TestRemoteOperationExportUsesFilteredStableSummary`; reject an unsupported type.
- [x] Run focused Store tests; initially they failed because `Kind` was not part of the filter.
- [x] Add API list/export assertions for type propagation and reject an unsupported type in `TestCloudOperationHistoryAPIIsReadOnlyAndValidatesPages`.
- [x] Implement Store validation/parameterized SQL and API parsing/export metadata.
- [x] Run Store/API focused tests; an initial query used a nonexistent `kind` column, and the failing test identified the schema's `operation_kind` name. Corrected and verified green.

### Task 2: History page controls

**Files:**
- Modify: `internal/webui/index_cloud.html`
- Modify: `internal/webui/res/cloud_operations.js`
- Test: `scripts/webui.test.cjs`

- [x] Extend list and export UI tests to require the selected kind and verify clear removes it; tests failed before the UI was wired.
- [x] Add a type selector and include it in list/export filters, auto-refresh on selection, and clear behavior.
- [x] Run `node scripts/webui.test.cjs`; 32/32 passed.

### Task 3: Release verification

**Files:**
- Update: `docs/ai/progress.md`
- Update: `docs/ai/release-versioning.zh-CN.md`

- [x] Run the WebUI/userscript suites and `git diff --check`; the release script also completed full `go test ./...`, `go vet ./...`, and all three target builds.
- [x] Build `v0.8.30-preview.65`; verify three platform binaries, all 12 payload checksums, package version, and private-data exclusions.
- [x] Record ZIP SHA-256 and remaining external acceptance in the release validation note.
