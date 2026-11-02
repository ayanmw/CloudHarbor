# Scoped Refresh Duplicate Page Drift Recovery Implementation Plan

> **For agentic workers:** Execute inline in the primary checkout; preserve existing dirty files, do not use a worktree, and do not commit or push.

**Goal:** Make a scoped cloud inventory refresh restart the current directory from page one when duplicate object identities reveal offset pagination drift.

**Architecture:** Reuse the existing durable `boundary_restart` checkpoint and per-directory restart limit. Detect provider page duplicates and store-level duplicate IDs across staged pages, preserving the published inventory until the restarted directory completes and continuing to reserve every provider GET in SQLite before sending it.

**Tech Stack:** Go, SQLite, existing Baidu list-page client, scoped inventory refresh worker.

**Spec:** `docs/superpowers/specs/2026-11-01-cloud-page-boundary-auto-recheck.zh-CN.md`

## Global Constraints

- A directory may automatically restart at most twice; counts derive from append-only SQLite events and survive process restart.
- Each re-read GET still requires durable request-budget reservation before the provider call.
- Keep the last published inventory visible until a complete refresh publishes; do not alter remote files or claim an atomic Baidu snapshot.
- Do not create or use a Git worktree, and do not commit/push.

## Review Focus

- A provider page repeats an `fs_id` while the current offset is nonzero; recheck and eventually publish only a stable complete directory.
- A later page repeats an ID from an earlier staging page; classify it as boundary drift before SQLite's unique constraint leaks a raw error.
- The restart event or boundary transaction fails; preserve a recoverable/fail-closed refresh state and do not consume another request without reservation.
- The same duplicate persists past the per-directory cap; stop at the cap and do not issue another GET.
- Existing published cache remains unchanged until a full staged directory succeeds.

---

### Task 1: Recheck scoped refresh after duplicate pagination IDs

**Files:**
- Modify: `internal/taskstore/cloud_inventory.go`
- Modify: `internal/downloader/cloud_inventory_scan.go`
- Test: `internal/downloader/cloud_inventory_scan_test.go`

**Interfaces:**
- Consumes: `taskstore.ErrCloudInventoryDuplicateAcrossPages`, `Store.RestartCloudInventoryRefreshBoundary`, and `baiduapi.ErrDuplicatePageItem`.
- Produces: Duplicate IDs from a provider page or previous staged page trigger the existing bounded restart and durable evidence.

- [x] **Step 1: Add RED integration cases** for a scoped refresh where a repeated provider-page ID settles before the second read and where a repeated ID across staged pages settles before retry.
- [x] **Step 2: Run the focused regression before production changes.** `TestInventoryScannersRecheckWhenPageBoundaryMoves` failed only for the two new scoped-refresh duplicate cases; each showed `failed`, `boundary_restart_count=0`, and two GET reservations.
- [x] **Step 3: Classify staged-page overlap before insert.** Query the current run/directory staging IDs in bounded batches and return `ErrCloudInventoryDuplicateAcrossPages` before the staging-table unique constraint emits an untyped SQLite error.
- [x] **Step 4: Route scoped duplicate detections through `RestartCloudInventoryRefreshBoundary`.** Include `ErrDuplicatePageItem` from the provider and the taskstore overlap sentinel; preserve the existing cap and retry/error persistence paths.
- [x] **Step 5: Verify the focused GREEN regressions.** `go test ./internal/downloader -run '^TestInventoryScannersRecheckWhenPageBoundaryMoves$/directory_refresh/duplicate' -count=1` passed.
- [x] **Step 6: Run the complete relevant Go package tests and vet, inspect diff, and record remaining batch count.** `go test ./internal/downloader -count=1` passed on rerun after one earlier package run returned nonzero with truncated output; its failing test name was not retained, so that first-run anomaly remains unexplained. The focused drift regression passed twice; `go test ./internal/taskstore -count=1`, `go vet ./internal/downloader ./internal/taskstore`, and `git diff --check` passed. This item is 1/5 in the unnumbered preview batch; do not package until five independent user outcomes are verified.
