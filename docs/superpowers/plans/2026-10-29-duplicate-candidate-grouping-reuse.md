# Duplicate Candidate Grouping Reuse Implementation Plan

> **For agentic workers:** This plan is being executed inline. Use superpowers:executing-plans for later changes to this work.

**Goal:** Avoid calculating the same repeated-size groups separately for duplicate candidate totals and candidate pages.

**Architecture:** Use one SQLite statement with a `MATERIALIZED` repeated-size CTE, referenced by both the complete-candidate count and the bounded keyset page. Return the count alongside page rows, including an empty-page sentinel, while retaining the existing account, complete-directory and stable-`fs_id` rules.

**Tech Stack:** Go, SQLite (`modernc.org/sqlite`), `database/sql`.

**Spec:** [Cloud management and inventory cache](../specs/2026-10-29-cloud-management-cache.zh-CN.md)

## Constraints

- Preserve `ListCloudDuplicateHashCandidates` inputs, total-count meaning, stable cursor and result order.
- The size grouping must execute once per call, use `cloud_inventory_duplicate_size`, and avoid a temporary grouping B-tree.
- Empty pages still return the total candidate count; they must not fabricate a candidate.

## Tasks

### Task 1: Query plan regression

**Files:** `internal/taskstore/cloud_duplicates_test.go`

- [x] Assert the candidate query materializes the repeated-size grouping once, uses the size index and avoids a temporary grouping B-tree.
- [x] Run the regression against the existing separate-count/separate-page implementation and observe failure because the shared query was not defined.

### Task 2: Share the materialized grouping

**Files:** `internal/taskstore/cloud_duplicates.go`, `internal/taskstore/cloud_duplicates_test.go`

- [x] Implement one CTE query for grouping, count and keyset page; parse the empty-page sentinel into zero result rows plus total.
- [x] Preserve complete-directory filtering, account isolation, ordering and current pagination behavior.
- [x] Verify candidate set, pagination boundaries and empty-page total.

### Task 3: Verify and release

- [x] Taskstore duplicate grouping, complete-directory, stable pagination and empty-page regressions pass; full package gate, three-platform builds, 12 payload hashes and packaged Windows version command pass for preview.38.
