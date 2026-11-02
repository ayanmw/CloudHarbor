# 重复文件扫描字节进度实施计划

> **For agentic workers:** Execute inline in the existing main checkout; do not use a worktree. Preserve unrelated dirty changes.

**Goal:** Show durable, accurate file and byte progress for duplicate hashing, including pause and process restart.

**Architecture:** Add checked byte counters to the SQLite duplicate-scan state, migrate schema 16 to 17 atomically, update counters only after a fully verified file, reconcile the same run's total against completed plus remaining candidates on resume, and render both measures in the existing progress view.

**Tech Stack:** Go, existing SQLite taskstore/migrations, embedded HTML/JavaScript, Node `node:test`.

**Spec:** `docs/superpowers/specs/2026-10-30-duplicate-scan-byte-progress.zh-CN.md`

## Global Constraints

- Preserve taskstore manifest/SQLite atomic migration and account scope.
- Partial, failed, or retrying reads do not count as completed bytes.
- Existing run identity and unknown remote mutation semantics do not change.
- No worktree, live Baidu large scan, remote delete, or new dependency.

## Review Focus

- A resumed run retains cumulative processed values and does not display processed greater than total.
- Failures and retries cannot double count bytes.
- Zero-size and zero-total runs render finite, truthful values.
- Schema migration interruption leaves either the prior v16 state or valid v17 state and preserves manifest evidence.
- Inventory changes while paused reconcile the remaining work without resetting already completed counts.

### Task 1: Durable taskstore fields and schema migration

**Files:** `internal/taskstore/cloud_duplicates.go`, `internal/taskstore/migration.go`, `internal/taskstore/sqlite.go`, `internal/taskstore/store.go`, tests in `internal/taskstore/`.

- [x] Add RED tests for byte counters persistence, negative counter validation, and reopening a schema-16 queue through migration to 17.
- [x] Implement `total_bytes`/`processed_bytes` with checked constraints and v16→v17 migration.
- [x] Verify focused taskstore tests and interrupted/reopened migration evidence.

### Task 2: Scanner accounting and API evidence

**Files:** `internal/downloader/cloud_duplicate_scan.go`, `internal/downloader/*duplicate*test.go`, `internal/taskstore/cloud_duplicates.go`.

- [x] Add RED tests for new-run estimates, successful-only increments, retry/pause resume, and dynamic remaining-candidate reconciliation.
- [x] Persist files and bytes in one state transition after hash save; include byte fields in append-only event evidence.
- [x] Verify focused scanner/store tests including reopening the actual taskstore.

### Task 3: Progress UI

**Files:** `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`.

- [x] Add RED WebUI cases for file and byte progress, zero-byte state, and invalid zero-total percent handling.
- [x] Render verified bytes with existing `formatBytes`, retain collapsible details, and keep summary useful while collapsed.
- [x] Verify WebUI/userscript suites.

### Task 4: Release `.85`

**Files:** `docs/ai/progress.md`, `docs/ai/release-versioning.zh-CN.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`.

- [x] Run full Go tests/vet, WebUI/userscript, cross-platform package gates, then inspect ZIP manifest and private-data exclusion.
- [x] Build `.85`, record package and source snapshot hashes, migration semantics, tests, and external acceptance limits.
