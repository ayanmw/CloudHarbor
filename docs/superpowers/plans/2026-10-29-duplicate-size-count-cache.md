# Duplicate Size Count Cache Implementation Plan

> **For agentic workers:** This plan is being executed inline. Use superpowers:executing-plans for later changes to this work.

**Goal:** Stop re-aggregating the entire cloud inventory for every duplicate scan candidate page.

**Architecture:** Maintain account/file-size counts transactionally from `cloud_inventory` insert, delete and identity-size/type updates. Build the index state with a v11→v12 migration and verified manifest recovery. Use the derived counts for the one-time total and scan candidates, while the scanner fetches only bounded keyset pages and commits progress per verified file.

**Tech Stack:** Go, SQLite triggers, `database/sql`, modernc SQLite.

**Spec:** [Cloud management and inventory cache](../specs/2026-10-29-cloud-management-cache.zh-CN.md)

## Constraints

- Count only non-directory cloud inventory rows, scoped by account and exact size.
- Trigger updates and inventory mutations commit in the same SQLite transaction.
- Migration backfills counts atomically and restart recovery verifies the derived table against source inventory.
- Keep complete-directory eligibility, stable `fs_id` cursors and per-file durable progress.
- Compute a full candidate total only at scan start; candidate pages do not recount the full eligible set.

## Tasks

### Task 1: Pin derived count semantics

**Files:** `internal/taskstore/cloud_inventory_test.go`

- [x] Verify file inserts, size changes, deletions, file/directory type changes and account isolation update the summary counts.
- [x] Run against current schema and observe the expected missing summary table failure.

### Task 2: Add schema and migration

**Files:** `internal/taskstore/sqlite.go`, `internal/taskstore/store.go`, `internal/taskstore/migration.go`, `internal/taskstore/migration_test.go`

- [x] Add summary table plus insert/delete/update triggers to fresh schema.
- [x] Add transactional v11→v12 backfill and schema/manifest commit recovery.
- [x] Verify migration data preservation, recovery consistency and post-migration trigger behavior.

### Task 3: Use derived counts in the duplicate scanner

**Files:** `internal/taskstore/cloud_duplicates.go`, `internal/taskstore/cloud_duplicates_test.go`, `internal/downloader/cloud_duplicate_scan.go`, `internal/downloader/cloud_duplicate_retry_test.go`

- [x] Read the initial total from the summary and add a count-free bounded candidate-page method.
- [x] Switch scan page refills to the count-free method while retaining cursor/retry semantics.
- [x] Verify account, complete-directory and page boundaries; full restart/retry checks remain in the verification gate.

### Task 4: Verify and release

- [x] Run taskstore/downloader regressions, full release gate, and package a validated independent preview.
