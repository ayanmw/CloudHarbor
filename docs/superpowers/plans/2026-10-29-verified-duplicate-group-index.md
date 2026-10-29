# Verified Duplicate Group Index Implementation Plan

> **For agentic workers:** This plan is being executed inline. Use superpowers:executing-plans for later changes to this work.

**Goal:** Reduce temporary sorting during the final grouping of verified duplicate content hashes.

**Architecture:** Use SQLite query-plan evidence to choose an index and, if required, adjust the eligible-hash grouping query to preserve account/hash/size index order while counting only entries whose cached file identity and containing directory remain current. Add an atomic v10→v11 index migration with manifest recovery.

**Tech Stack:** Go, `database/sql`, modernc SQLite.

**Spec:** [Cloud management and inventory cache](../specs/2026-10-29-cloud-management-cache.zh-CN.md)

## Constraints

- Duplicate groups must contain more than one currently eligible file with the same SHA256 and size.
- Account isolation and exact cached identity checks remain part of eligibility.
- Migrations preserve existing hashes, inventory and schema-scope fail-closed behavior.

## Tasks

### Task 1: Capture current query plan

**Files:** `internal/taskstore/cloud_duplicates_test.go`

- [x] Assert the verified duplicate query avoids temporary grouping sort; the old plan failed with `USE TEMP B-TREE FOR GROUP BY`.

### Task 2: Add index and migration

**Files:** `internal/taskstore/sqlite.go`, `internal/taskstore/store.go`, `internal/taskstore/migration.go`, `internal/taskstore/migration_test.go`, `internal/taskstore/cloud_duplicates.go`

- [x] Add the digest/size ordered index and include it in schema verification.
- [x] Add transactional v10→v11 migration and post-commit/pre-manifest recovery.
- [x] Make eligible grouping use the index order where the plan confirms it helps, without grouping stale hashes as current duplicates.
- [x] Verify exact groups, cache changes, data preservation and empty/no-duplicate accounts.

### Task 3: Verify and release

- [x] Focused query-plan, exact group, cache identity and v8/v9/v10 migration tests pass; full release gate, three-platform builds, 12 payload hashes and packaged Windows version command pass for preview.39.
