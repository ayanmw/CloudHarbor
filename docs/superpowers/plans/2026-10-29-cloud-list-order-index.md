# Cloud Listing Order Index Implementation Plan

> **For agentic workers:** This plan was executed inline. Use superpowers:executing-plans for later changes to this work.

**Goal:** Make cached cloud directory pagination use an ordered SQLite index and upgrade existing v8 stores without losing inventory.

**Architecture:** Replace the parent-only index with `(account_fingerprint,parent_path,name,fs_id)`, which supports both parent filtering and the existing stable name/fs_id ordering. Apply it in schema v9; the v8 migration commits the index and schema version together, then atomically publishes the manifest and recovers if a restart occurs between those steps.

**Tech Stack:** Go, `database/sql`, modernc SQLite.

**Spec:** [Cloud management and inventory cache](../specs/2026-10-29-cloud-management-cache.zh-CN.md)

## Global Constraints

- Preserve account isolation, listing order, page size, offset behavior, and all cached inventory rows.
- Use the existing SQLite transaction and manifest publication patterns.
- A migration restart must either retry the v8 transaction or finish the v9 manifest publication after verifying the committed index state.

## Review Focus

- Existing v8 inventory remains readable after migration: migration integration test checks listing, count and status.
- SQLite commit succeeds but manifest publication is interrupted: recovery test verifies next open finalizes v9.
- Equal names retain deterministic order: `fs_id` remains the final index key and query ordering term.
- Account scoping remains the leading index key; query plan must use the new composite index.
- Large directory pagination must avoid a temporary sort: query plan test rejects `TEMP B-TREE`.

## Tasks

### Task 1: Verify v8 migration and ordered pagination

**Files:** `internal/taskstore/migration_test.go`

- [x] Add migration test proving v8 inventory survives, migration action is `migrated_v8`, and ordered listing uses the new index without a temporary B-tree.
- [x] Add restart-recovery test for a committed v9 database whose manifest still says v8.
- [x] Run both tests against the old code and observe failure before implementing migration.

### Task 2: Add schema v9 index migration

**Files:** `internal/taskstore/sqlite.go`, `internal/taskstore/store.go`, `internal/taskstore/migration.go`

- [x] Bump the schema version and create the composite index in fresh databases.
- [x] Add v8→v9 migration that transactionally replaces the old index and persists schema version 9.
- [x] Chain migration for v2 through v7 stores and preserve `migrated_v8` startup diagnostics for v8 stores.
- [x] Recover the post-commit/pre-manifest restart state only when schema scope and exact index presence match; otherwise fail closed.

### Task 3: Verify

- [x] Run migration, pagination query-plan and inventory preservation tests.
- [x] Run full package release gate and validate the independent preview ZIP.
