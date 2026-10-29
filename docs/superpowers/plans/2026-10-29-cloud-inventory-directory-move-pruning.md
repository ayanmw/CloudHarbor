# Cloud Inventory Directory Move Pruning Plan

> **For agentic workers:** Execute this plan with TDD. Steps use checkbox syntax for tracking.

**Goal:** Keep the full cloud inventory and directory-size cache accurate when a directory is renamed, moved, or replaced between complete scans.

**Architecture:** When an existing directory `fs_id` is observed at a different path, prune cached descendants and directory states under both the old path and the destination path within the inventory page transaction, then leave the destination directory stale until it is scanned. When a previously cached directory at a path is absent by `fs_id` but a new directory is observed at the same path, delete the stale root by its old `fs_id`, remove old descendants, and preserve/reset the new path's directory state as stale.

**Tech Stack:** Go, `database/sql`, modernc SQLite.

**Spec:** `docs/superpowers/specs/2026-10-29-cloud-management-cache.zh-CN.md`

## Global Constraints

- Inventory page and its path pruning remain one SQLite transaction; failure rolls back both.
- The current page's new directory identity at a reused path must survive stale cleanup.
- Old cached descendants and old path directory states must not contribute to later sizes or completeness.
- Existing account scoping, fs_id identity, scan run IDs, and UTC observation times remain unchanged.

## Review Focus

- A directory fs_id renamed from `/old` to `/new` loses `/old` descendants and metadata before `/new` completes.
- A new fs_id replacing a directory at the same path survives final-page stale cleanup.
- A moved directory destination with previous stale metadata remains incomplete until its new listing is scanned.
- Failure during page persistence rolls back subtree pruning and page observations together.

---

### Task 1: Reproduce stale subtree and same-path replacement defects

**Files:** `internal/taskstore/cloud_inventory_test.go`

- [x] Added a full refresh where directory fs_id 1 moves from `/old` to `/new`; the expected root size is zero and only `/new` remains after its empty listing.
- [x] Added a refresh where fs_id 3 replaces old fs_id 1 at `/folder`; the new root entry must remain and its contents must be incomplete until scanned.
- [x] Both tests fail before the production fix: the moved directory leaves 100 stale bytes/two directory states, and same-path replacement deletes the newly observed folder.

### Task 2: Prune old path state transactionally

**Files:** `internal/taskstore/cloud_inventory.go`, `internal/taskstore/cloud_inventory_test.go`

- [x] Before upserting a directory row, read any existing path for that account/fs_id. If changed, clear descendant inventory rows and directory metadata under both old and new path prefixes, then insert/reset destination directory metadata as stale.
- [x] During final-page stale cleanup, delete the removed directory root by its fs_id, prune descendants separately, and preserve a new directory at the same path while resetting its metadata to stale.
- [x] Add a failing-trigger regression proving page errors roll back directory pruning and the inventory generation together.
- [x] Run the focused directory-move regressions and the full taskstore suite.

### Task 3: Verify and publish

**Files:** `docs/ai/progress.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/experience-plan.md`.

- [x] Run WebUI/userscript and all Go tests, vet, and Windows/Linux AMD64/Linux ARM64 release builds.
- [x] Verify the independent package payloads and Windows version; update the previously authorized preview.24 directory and verify every payload hash.
- [x] Record results and retain the real Baidu/NAS acceptance boundary.
