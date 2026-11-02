# Confirmed Cloud Cache Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Reflect confirmed Baidu rename, move, mkdir, and delete operations in a complete SQLite inventory so directory lists and sizes update without waiting for another full scan.

**Architecture:** The downloader supplies the verified before/after provider identities to one taskstore transaction. The store applies the change only when all affected parents and directory subtrees are complete and no active scan is paging an affected parent; otherwise it reports not-applicable and the current stale-and-rescan behavior remains. Unknown operations never apply a delta and remain isolated.

**Tech Stack:** Go, modernc SQLite, existing Baidu API client and embedded management UI.

**Spec:** docs/superpowers/specs/2026-10-30-confirmed-cloud-cache-refresh.zh-CN.md

## Global Constraints

- Never replay an uncertain Baidu mutation.
- Confirmed remote outcome and local cache outcome remain separate; cache failure must not rewrite a confirmed remote result as unknown.
- Update inventory records, directory coverage and the account generation in one SQLite transaction.
- Preserve per-entry cloud observation timestamps for descendants whose metadata was not re-read.
- Unknown operations continue to invalidate affected paths and quarantine the object.
- Preserve existing task queue, authentication and user working-tree changes; do not use a worktree.

## Review Focus

- A path or fs_id mismatch must prevent a delta and stale the affected cache.
- A move or rename of a directory must rebase every cached descendant without losing directory completeness or size totals.
- A directory with an incomplete descendant must not be declared complete after a parent operation.
- A mutation that touches the active scan page or a queued old directory path must be rejected before the provider write, avoiding stale offset cursors.
- A failed SQLite transaction after a confirmed provider write must leave no partial rewrite and return the confirmed result with a cache refresh warning.

---

### Task 1: Add transactional confirmed-mutation application to taskstore

**Files:**
- Modify: internal/taskstore/cloud_inventory.go
- Test: internal/taskstore/cloud_inventory_test.go

**Interface:**
- Add CloudInventoryMutation with Operation, Before, After, and ResponseObservedAtUTC fields.
- Add Store.ApplyConfirmedCloudMutation(account string, mutation CloudInventoryMutation) (bool, error). A false result means required coverage/identity preconditions were absent; an error means storage failed.
- Add Store.CloudMutationScanConflict(account string, paths []string) (bool, error) for the downloader to call while holding cloudMutex before sending a provider mutation. Return conflict when the active scan queue overlaps an affected parent or moved/deleted subtree.
- The transaction checks complete source and destination parents, complete directory descendants, current fs_id/path/type identity, destination collisions, and active scan queue overlap before changing rows.
- File rename/move updates the entry path and parent path. Directory rename/move rebases its cached subtree. mkdir inserts a verified new directory with complete empty contents. Delete removes the verified file or directory subtree.
- Every successful delta updates affected direct-parent observation time and bumps inventory generation once in the same transaction.

- [x] Write a failing taskstore test proving CloudMutationScanConflict detects an active queue path that overlaps an affected directory.
- [x] Write failing tests for a renamed file changing the cached parent list and size without marking it stale.
- [x] Run the focused test and confirm it fails because the mutation transaction does not exist.
- [x] Add the minimum transaction implementation for file rename and rerun the focused test.
- [x] Add failing tests for file move, mkdir, file delete, empty-directory delete, and complete directory subtree move/rename/delete.
- [x] Verify those tests fail for the missing mutation behavior.
- [x] Implement the remaining transaction branches, returning false without partial writes when any coverage, identity, collision, or scan-queue precondition fails.
- [x] Add trigger-injected transaction rollback coverage and assert paths, directory statuses and generation remain unchanged.
- [x] Run the taskstore cloud inventory tests.

### Task 2: Integrate verified provider observations in downloader mutations

**Files:**
- Modify: internal/downloader/cloud_management.go
- Test: internal/downloader/cloud_management_test.go

**Interface:**
- For rename/move, read the current object by fs_id after synchronous confirmation and require its path/name/type to match the requested destination.
- For mkdir, list the destination parent after confirmation and use the unique new directory identity; if it cannot be proven, retain stale fallback.
- For delete, use the identity rechecked immediately before the confirmed delete.
- Call ApplyConfirmedCloudMutation only after the remote operation journal is persisted as confirmed.
- If the delta is not applicable or provider observation fails, invalidate the affected paths as today and return the confirmed operation with a cache refresh warning where necessary.
- If the active full scan is paging an affected parent or queued beneath a directory being moved/deleted, reject the mutation before sending it and tell the caller to pause the scan.
- Evaluate scan conflict after cloudMutex.Lock and before creating the remote operation journal; this serializes the decision against the in-flight scan page and prevents a provider write with an invalid cursor.

- [x] Add failing downloader tests proving a confirmed rename updates local listing and size immediately.
- [x] Run the focused test and confirm the stale cache behavior causes the expected failure.
- [x] Add tests for post-confirmation metadata mismatch, mkdir identity lookup failure, unknown provider results and scan overlap; assert fallback or pre-send rejection and no delta.
- [x] Implement the smallest provider observation and taskstore integration needed for these cases.
- [x] Verify remote-operation event evidence remains confirmed and timestamped when only cache refresh fails.
- [x] Run downloader cloud-management and inventory-scan tests.

### Task 3: Update user-facing behavior and package preview.54

**Files:**
- Modify: internal/management handlers only if existing response fields cannot express a confirmed operation with a cache refresh warning
- Modify: internal/webui/index_cloud.html only if existing UI does not show the refresh warning or new local size
- Modify: docs/ai/cloud-management-roadmap.zh-CN.md
- Modify: docs/ai/release-versioning.zh-CN.md
- Modify: docs/ai/progress.md

- [x] Verify existing UI refreshes the current listing and size after each confirmed remote operation; add only the missing behavior.
- [x] Run WebUI and userscript tests, full Go tests, vet, and git diff --check.
- [x] On the authorized real Baidu account, create uniquely named empty test directories and verify confirmed mkdir, rename, move, and empty-directory delete; confirm the test prefix is absent afterward. A post-CRUD inventory rescan is still running.
- [x] Package v0.8.30-preview.54 with the release script after all tests pass.
- [x] Verify ZIP payload hashes, build metadata and package exclusion of runtime credentials and SQLite state.
- [x] Run preview.54 in an isolated test directory; do not replace preview.53 or its authorized runtime data until the package verification succeeds.
- [x] Finish the post-CRUD full inventory rescan (983 pages/8,050 items, 976/976 directory states complete, SQLite integrity ok), then document final source snapshot, package hash, test evidence, scan counts and remaining NAS/SMB acceptance. Repackage .54 after documentation edits.

## Plan Review

- Scope is one user-visible feature: complete-cache directories reflect confirmed remote CRUD immediately. Incomplete coverage and unknown outcomes retain the established stale/rescan and quarantine behavior.
- Scanner safety is tested at the downloader/store boundary; unrelated scan pages remain serialized by the existing cloud mutex.
- No automatic hash scan or deletion of existing user content is part of this batch.
