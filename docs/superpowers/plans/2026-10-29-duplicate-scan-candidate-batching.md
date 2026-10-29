# Duplicate Scan Candidate Batching Implementation Plan

> **For agentic workers:** This plan is being executed inline. Use superpowers:executing-plans for later changes to this work.

**Goal:** Avoid repeating full duplicate-size grouping and candidate counting for every file hashed during one duplicate scan.

**Architecture:** Fetch candidate rows in bounded, stable `fs_id` pages and hold only the current page in memory. Persist progress after every successfully hashed file; on restart, refetch from the last durable cursor. Continue checking each candidate against live provider metadata and the complete local inventory before persisting its digest.

**Tech Stack:** Go, SQLite, existing Baidu provider and taskstore contracts.

**Spec:** [Cloud management and inventory cache](../specs/2026-10-29-cloud-management-cache.zh-CN.md)

## Constraints

- Candidate pages are bounded at 100 rows.
- Never advance durable `NextFsID` until one candidate hash and its inventory identity are durably recorded.
- Retry keeps the current candidate; cancellation/restart refetches from the last successfully persisted `NextFsID`.
- Keep live identity checks and account/completeness filters for every file.

## Tasks

### Task 1: Pin bounded pagination

**Files:** `internal/downloader/cloud_duplicate_retry_test.go`

- [x] Add a regression over 235 same-size files proving page sizes 100/100/35 and stable cursor continuation.
- [x] Run it against the current implementation and confirm it fails because the helper/page limit is absent.

### Task 2: Reuse candidate pages during scanning

**Files:** `internal/downloader/cloud_duplicate_scan.go`

- [x] Add `loadDuplicateCandidateBatch(store, account, afterFsID)` that requests 100 rows from taskstore.
- [x] Keep a page buffer in `runCloudDuplicateScan`; refill only after its current page is exhausted.
- [x] Pop a candidate only after hash verification and `SaveCloudDuplicateScanState` succeed; retain it through retry waits.

### Task 3: Verify and release

- [x] Run the targeted paging, retry, restart/backoff, and duplicate deletion identity regressions; all pass.
- [x] Full release gate and three-platform builds pass; all 12 payload hashes and packaged Windows version command verify for preview.37.
