# Directory-Scoped Cloud Refresh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refresh the selected Baidu directory and its descendants into the SQLite inventory without running a full account scan.

**Architecture:** Keep partial refresh state and queue in dedicated SQLite tables so the account-wide scan checkpoint remains independent. Serialize both scan kinds with the existing inventory scan mutex and cloud mutex; commit inventory pages, queue advancement, progress, and directory coverage atomically.

**Tech Stack:** Go, modernc SQLite, existing Baidu provider and authenticated management API, embedded HTML/JavaScript.

**Spec:** `docs/superpowers/specs/2026-10-30-directory-scoped-cloud-refresh.zh-CN.md`

## Global Constraints

- Do not use a worktree or alter preview.53 runtime state.
- Do not claim account-wide completion from a scoped refresh.
- Do not mark a directory complete before its final provider page is durably committed.
- Persist provider observations and local cache times distinctly; never treat a partial local cache as confirmed cloud state.
- Do not introduce concurrent directory scans or unbounded API fan-out.
- Failed, paused, or unknown coverage must remain visible as incomplete and must not authorize duplicate/empty cleanup.

## Review Focus

- A crash between page write and cursor advancement must not skip provider entries: test store page+queue atomicity and reopening.
- A scoped scan must not change `cloud_inventory_scan_state`: test both taskstore and downloader boundaries.
- A failed page in a paged directory must leave that directory incomplete and preserve its resume offset.
- A remote moved/replaced directory identity must not retain the old cached descendants as a complete subtree.
- A cloud mutation overlapping the refresh queue must be rejected before the provider call; test both queued descendant and current page.

---

### Task 1: Persist subtree refresh state and transactional pages

**Files:**
- Modify: `internal/taskstore/sqlite.go`
- Modify: `internal/taskstore/migration.go`
- Modify: `internal/taskstore/cloud_inventory.go`
- Test: `internal/taskstore/migration_test.go`
- Test: `internal/taskstore/cloud_inventory_test.go`

**Interfaces:**
- Add `CloudInventoryRefreshState` with account, run ID, root path, status, phase, current directory, next offset, pending count, page/item counts, start/update timestamps, and last error.
- Add `StartCloudInventoryRefresh`, `SaveCloudInventoryRefreshState`, `LoadCloudInventoryRefreshState`, `CloudInventoryRefreshQueueHead`, and `UpsertCloudInventoryRefreshPage` store methods.
- Add schema 16 tables `cloud_inventory_refresh_state` and `cloud_inventory_refresh_queue`; keep the existing full-scan tables and APIs unchanged.

- [x] Write a schema 15→16 migration/reopen test proving the existing full-scan state, queue, and cached inventory survive and the new tables validate.
- [x] Write failing tests for scoped state isolation, subtree-only invalidation, recursive queue progress, final-page prune/complete, and incomplete paged directories.
- [x] Verify failures are caused by missing migration/store behavior.
- [x] Implement schema 16 and atomic refresh state/queue operations; commit each page, directory coverage, generation bump, queue changes, and state cursor together.
- [x] Add trigger-injected rollback and process-reopen tests; assert a page cannot advance the persisted offset without its inventory rows.
- [x] Run focused taskstore migration and cloud inventory tests (`go test ./internal/taskstore -count=1` → pass, 55.957s).

### Task 2: Run a resumable scoped scanner

**Files:**
- Modify: `internal/downloader/cloud_inventory_scan.go`
- Modify: `internal/downloader/downloader.go`
- Test: `internal/downloader/cloud_inventory_scan_test.go`
- Test: `internal/downloader/cloud_management_test.go`

**Interfaces:**
- Add `StartCloudInventoryRefresh(ctx context.Context, directory string) (taskstore.CloudInventoryRefreshState, error)` and `CloudInventoryRefreshStatus() (taskstore.CloudInventoryRefreshState, error)`.
- Add a worker that lists only the selected subtree at existing page size/rate, uses the existing listing validation/provider pacing/cloud mutex, and calls the atomic taskstore page method.
- Share the existing inventory scan mutex between full and scoped work. Reject a second scan kind while either persisted queue is resumable.
- Extend mutation scan conflict detection to include paths queued by scoped refresh.

- [x] Write a failing test that refreshes a nested directory recursively, updates its size and children, leaves a sibling untouched, and preserves full account scan state.
- [x] Write tests for same-scope resume after pause/reopen, different-scope rejection while a queue remains, invalid paths, and cloud mutation overlap before provider write.
- [x] Implement the scanner using the taskstore checkpoint and preserve the full-account scanner's current behavior.
- [x] Run full taskstore/downloader tests plus focused API, cloud mutation and scanner checks.

### Task 3: Expose scoped refresh in the API and page

**Files:**
- Modify: `internal/intakeapi/server.go`
- Modify: `internal/intakeapi/server_test.go`
- Modify: `main.go`
- Modify: `internal/webui/index_cloud.html`
- Modify: `scripts/webui.test.cjs`

**Interfaces:**
- Add `GET`, `POST`, and `DELETE` `/api/cloud-inventory/refresh`; POST body is `{ "directory": "/path" }`.
- Return the latest persisted scoped state without changing `/api/cloud-inventory/scan` response semantics.
- Add a current-directory refresh button and scoped progress view; completion reloads current listing and cached size.

- [x] Add API tests for status, start, strict request fields and pause; scanner tests cover invalid paths and mutual exclusion with full scan.
- [x] Add WebUI script checks for scoped endpoint, root confirmation, progress/pause controls, and completion reload behavior.
- [x] Implement handlers, route registration, and browser controls; keep browsing usable while the refresh runs.
- [x] Run API, WebUI, and userscript checks.

### Task 4: Verify and package preview.55

**Files:**
- Modify: `docs/ai/cloud-management-roadmap.zh-CN.md`
- Modify: `docs/ai/release-versioning.zh-CN.md`
- Modify: `docs/ai/progress.md`

- [x] Run full Go tests, vet, diff checks, and Windows/Linux AMD64/Linux ARM64 builds.
- [x] On an isolated authenticated copy, refresh one small real-account directory subtree read-only; verify completion, byte/file totals, and that outside paths/full-scan state stay unchanged.
- [x] Package `v0.8.30-preview.55` using the release script; verify ZIP hashes, metadata, sidecar checksum, and private runtime exclusions.
- [x] Document scan counts, API/page tests, package hash, and remaining NAS/SMB acceptance.

## Plan Review

- The selected refresh root and descendants are the only directories invalidated and re-observed.
- Account-wide scan state remains authoritative for full scans; scoped status does not substitute for it.
- A paused scoped queue is persisted and can only resume the same root path; a new path requires prior completion.
- Existing confirmed-mutation scan-queue safeguards apply to scoped queue entries.
