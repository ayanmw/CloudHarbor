# Duplicate Scan Live Inventory Reconciliation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reconcile successful live file metadata into the local cloud inventory during duplicate hashing, invalidate derived evidence, and pause safely until affected cache paths are complete.

**Architecture:** Add one transactional taskstore operation that conditionally updates a candidate by stable `fs_id`, invalidates old/new parent coverage and SHA evidence, clears the active checkpoint, pauses the duplicate scan, and appends timestamped identity evidence. Treat a response without one uniquely matching object as unavailable: retain cached object/hash, invalidate its old parent, and pause without interpreting the response as deletion. Resume reuses the existing full-coverage guard and recalculates totals.

**Tech Stack:** Go, `modernc.org/sqlite`, existing Baidu API metadata reads, embedded WebUI JavaScript and Node tests.

**Spec:** `docs/superpowers/specs/2026-10-31-duplicate-scan-live-inventory-reconciliation.zh-CN.md`

## Global Constraints

- Do not delete a cached or remote object based on an absent/ambiguous metadata response.
- Keep provider observation UTC separate from local SQLite record UTC.
- Do not advance `next_fs_id` or completed counters for a changed candidate.
- Preserve source-operation unknown-result quarantine and all existing account isolation.
- No worktree, commit, push, cloud mutation, large real-account hash scan, or NAS/SMB acceptance claim.
- Publish one preview only after at least five independent user outcomes and all release gates pass.

## Review Focus

- Candidate changes between cached enumeration and first metadata check: no content download starts; cache and state transition atomically.
- Candidate changes after full content read but before hash commit: the digest must not enter the verified-hash table.
- Same-parent metadata change versus cross-parent move: invalidate the correct parent subtrees and size generation.
- Missing or ambiguous metadata result: preserve cached inventory and digest while making the old parent incomplete.
- Active inventory scan overlap: do not race a cache reconciliation against a queued inventory page; preserve a resumable paused scan.
- Refresh completion before resume: recompute remaining file/byte estimate and revisit the unchanged `next_fs_id` candidate.

---

### Task 1: Atomic taskstore reconciliation and event evidence

**Files:**
- Modify: `internal/taskstore/cloud_duplicates.go`
- Modify: `internal/taskstore/cloud_inventory.go`
- Test: `internal/taskstore/cloud_duplicates_test.go`
- Test: `internal/taskstore/cloud_inventory_test.go`

**Interface:** `ReconcileCloudDuplicateCandidateChange(account string, state CloudDuplicateScanState, expected CloudInventoryEntry, observed *CloudInventoryEntry, providerObservedAt, recordedAt time.Time) error`.

- [x] Add RED tests for successful identity update, old hash removal, source/destination parent invalidation, generation advance, state pause/checkpoint clearing, and append-only evidence with distinct UTC timestamps.
- [x] Add RED tests for unavailable identity preserving inventory/hash while invalidating the old parent, stale old identity rollback, and overlapping inventory queue conflict.
- [x] Implement a single transaction with conditional old identity/checkpoint predicates; use no schema migration by extending the existing event evidence JSON.
- [x] Run focused taskstore duplicate/cache tests and verify failure rollback leaves no partial changes.

### Task 2: Detect and reconcile live identity drift

**Files:**
- Modify: `internal/downloader/cloud_duplicate_scan.go`
- Test: `internal/downloader/cloud_duplicate_retry_test.go`
- Test: `internal/downloader/downloader_test.go`

- [x] Add RED tests for drift before content download, drift after content read, and missing/ambiguous metadata; assert no incomplete hash is verified and completed counters/cursor stay fixed.
- [x] Carry the current provider `FileInfo` in a typed drift error; distinguish a successful but non-unique metadata response from a retryable/authorization/network error.
- [x] Convert an observed file snapshot to the taskstore inventory shape and atomically reconcile it; for unavailable identity pass no snapshot and never infer deletion.
- [x] On scan-queue conflict, clear the active checkpoint and persist a paused diagnostic without changing inventory; allow the existing full-coverage and resume guard to govern continuation.
- [x] Run focused downloader and taskstore suites.

### Task 3: Resume and estimate from refreshed cache

**Files:**
- Modify: `internal/downloader/cloud_duplicate_scan.go`
- Test: `internal/downloader/cloud_duplicate_retry_test.go`

- [x] Add RED integration coverage that rejects resume while affected coverage is stale, accepts it after refresh completes, recomputes remaining totals, and retries the current candidate from byte zero.
- [x] Preserve completed candidates while leaving the drifted candidate at the same `next_fs_id`; use the existing resume estimate path after coverage is complete.
- [x] Verify a changed candidate is re-counted from the refreshed inventory and that its stale SHA is removed before the rescan estimate is used.

### Task 4: Explain the pause and evidence in the page

**Files:**
- Modify: `internal/webui/index_cloud.html`
- Test: `scripts/webui.test.cjs`

- [x] Add RED page coverage for `inventory_changed` paused status, localized refresh guidance, old/new path and UTC evidence rendering, and inert HTML-like filenames.
- [x] Keep the UI read-only for this condition; link or name the existing directory refresh workflow without automatically starting it.
- [x] Verify event list pagination and existing Retry-After/current-file rendering remain unchanged.

### Task 5: Release candidate and route evidence

**Files:**
- Modify: `docs/ai/experience-plan.md`
- Modify: `docs/ai/cloud-management-roadmap.zh-CN.md`
- Modify: `docs/ai/release-versioning.zh-CN.md`
- Modify: `docs/ai/progress.md`
- Modify: `release/TESTING.md`
- External artifact: releases validation record for the next available preview.

- [x] Run full `go test -count=1 ./...`, `go vet ./...`, WebUI/userscript tests, `git diff --check`, and Windows AMD64/Linux AMD64/Linux ARM64 package gates.
- [x] Verify ZIP payload/source snapshot hashes, version output, no private runtime data or staging residue.
- [x] Verify five independent outcomes from the spec with separate evidence before publishing `.100`.
- [x] Record local/mock verification and explicitly retain Baidu live Range and NAS/SMB limitations.
- [x] Inspect final diff and preserve all unrelated dirty and untracked work.
