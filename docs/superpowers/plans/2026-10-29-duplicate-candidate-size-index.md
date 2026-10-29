# Duplicate Candidate Size Index Implementation Plan

> **For agentic workers:** This plan is being executed inline. Use superpowers:executing-plans for later changes to this work.

**Goal:** Reduce repeated work when the duplicate-content scanner groups the complete cloud inventory by file size.

**Architecture:** Add a covering order beginning with account, directory flag and size, then filesystem ID. Upgrade SQLite v9 to v10 in one database transaction and publish the manifest only after integrity and index-state checks; recover a committed database whose manifest still says v9.

**Tech Stack:** Go, `database/sql`, modernc SQLite.

**Spec:** [Cloud management and inventory cache](../specs/2026-10-29-cloud-management-cache.zh-CN.md)

## Global Constraints

- Keep duplicate candidates limited to files under complete cached directories and preserve stable `fs_id` keyset pagination.
- Do not prefetch candidate batches into scanner state; each candidate remains revalidated against current inventory by existing scanner flow.
- Preserve cached inventory during schema upgrades and fail closed when schema scope or migration index state is inconsistent.

## Review Focus

- Duplicate-size grouping uses the new index without a temporary grouping B-tree.
- Existing v9 inventory survives migration and remains readable.
- A restart after database commit but before manifest publication finalizes v10 only when the index is present.
- Candidate pagination across page boundaries remains stable by filesystem ID.

## Tasks

### Task 1: Pin duplicate candidate query behavior

**Files:** `internal/taskstore/cloud_duplicates_test.go`

- [x] Add query-plan assertion that groups account-scoped non-directory entries by size using the dedicated index without `TEMP B-TREE`.
- [x] Add multi-page candidate pagination regression across 125 duplicate-sized files.
- [x] Run the query-plan assertion against the previous schema and observe the expected missing-index failure.

### Task 2: Migrate schema v9 to v10

**Files:** `internal/taskstore/sqlite.go`, `internal/taskstore/store.go`, `internal/taskstore/migration.go`, `internal/taskstore/migration_test.go`

- [x] Add the index to fresh schemas and require it during schema validation.
- [x] Transactionally create the index and update SQLite schema version to 10; atomically publish manifest v10 after integrity validation.
- [x] Chain migration from older supported schemas and recover the committed-database/pre-manifest state.
- [x] Preserve inventory in migration tests and verify prior migration chains finish at v10.

### Task 3: Verify and package

- [x] Run focused candidate and migration regressions; package gate passed all Node tests, `go test ./...`, `go vet ./...` and Windows/Linux AMD64/ARM64 builds.
- [x] Produce an independent preview ZIP, verify all 12 payload SHA256 entries and the packaged Windows version command; active runtime directory was not updated.
