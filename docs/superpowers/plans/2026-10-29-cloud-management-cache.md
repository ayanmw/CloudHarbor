# Cloud Management and Inventory Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add safe web-managed download paths, Baidu cloud file operations, a resumable throttled full-drive cache, locally calculated directory sizes, and preview-first duplicate/empty-folder cleanup.

**Architecture:** Keep the existing Go web UI and SQLite taskstore. Add a dedicated cloud inventory store and persistent background worker; serialize cloud mutations behind scheduler maintenance mode and reconcile jobs from a fresh queue-directory scan. Treat cached metadata as staleable display data and preserve unknown remote operation outcomes as quarantined evidence.

**Tech Stack:** Go, `modernc.org/sqlite`, embedded HTML/JavaScript, existing Baidu REST client.

**Spec:** `docs/superpowers/specs/2026-10-29-cloud-management-cache.zh-CN.md`

## Global Constraints

- Preserve all unrelated working-tree changes, including the existing credential/admin-login work.
- Never blindly retry an uncertain Baidu mutation; persist unknown outcomes and keep affected identities isolated.
- All cache-derived sizes must expose completeness and last cloud-observation time.
- Full scans must be paginated, throttled, resumable, and must not block normal browsing.
- Local migration must never delete source data before verified completion and must never overwrite a conflicting destination.
- Keep .env precedence and existing taskstore scope/restart guarantees.

## Review Focus

- Account switch with cached fs_id values: ensure cache namespace is account-bound and old data is never used for writes.
- A directory changes during paginated scan: incomplete generations must not appear complete or feed destructive cleanup.
- Unknown response after move/delete/rename: ensure no automatic replay and affected cache/job records remain isolated.
- Crash during local directory migration: source remains recoverable and restart resumes without overwriting.
- Large tree / API 429: browser remains responsive while worker backs off and resumes from persisted cursor.

## Files and Responsibilities

- `internal/baiduapi/client.go` and new focused operation files: provider paging and confirmed CRUD endpoints.
- `internal/taskstore/*`: schema migration and durable settings, inventory, background-operation and evidence persistence.
- `internal/downloader/scheduler.go`: pause new claims, drain active jobs, reconcile after remote operations.
- new `internal/downloader/cloud_management*.go`: CRUD validation, inventory scanning, size aggregation, cleanup previews.
- `internal/webui/webui.go`, new embedded management page and assets: authenticated APIs and UI.
- `main.go`, `release/*`: route wiring, defaults, operator-facing settings and preview packaging.

## Tasks

### Task 1: Scheduler maintenance gate and restart-safe cloud operation journal

**Files:** Modify `internal/downloader/scheduler.go`; extend `internal/taskstore/migration.go`, `sqlite.go`, `store.go`; add focused tests.

- [x] Add durable operation records with run/operation IDs, account fingerprint, object fs_id/path snapshot, local/remote timestamps, status (`prepared`, `confirmed`, `unknown`, `failed`) and evidence JSON.
- [x] Add scheduler maintenance mode that blocks new claims and drains active work with context cancellation support; expose begin/end APIs.
- [x] On restart, unresolved prepared mutations become unknown and never replay.
- [x] Verify scheduler pause/resume, drain timeout, restart recovery and event ordering.

### Task 2: Baidu remote CRUD and identity recheck

**Files:** Modify `internal/baiduapi/client.go`; add operation modules and mock tests.

- [x] Implement create-directory, move, rename and delete request contracts; classify sync/async/unknown results without unsafe retry.
- [x] Add downloader-level operations that re-fetch source/parent metadata, verify fs_id/path/type and destination collisions immediately before mutation.
- [x] Run operations under maintenance gate; after confirmed operations, scan and reconcile the configured queue directory before resuming workers.
- [x] After confirmed operations, invalidate affected inventory paths and reconcile the monitored queue from a fresh listing; leave unknown outcomes isolated. Pending task paths are not rewritten by fs_id yet.

### Task 3: SQLite cloud inventory and incremental size aggregation

**Files:** Add `internal/taskstore/cloud_inventory.go`; extend schema migrations; add downloader cache APIs.

- [x] Persist account-scoped entries, directories, scan runs, per-directory page cursors and timestamps.
- [x] Upsert each page transactionally; only mark a directory complete after all pages and children finish; discard stale entries only after full directory reconciliation.
- [x] Implement local subtree aggregation returning byte size, file/folder counts, coverage state and observed-at timestamp.
- [x] Invalidate/update affected subtree and ancestors after confirmed CRUD; mark affected cache stale after unknown results.
- [x] Verify aggregation, partial coverage, account isolation, stale reconciliation and restart persistence.

### Task 4: Resumable low-frequency full-drive scanner

**Files:** Add `internal/downloader/cloud_inventory_scan.go`; API and worker tests.

- [x] Traverse recursively with bounded pages, one low-frequency worker and persisted cursors.
- [x] Back off on 429/retryable errors, retain progress on shutdown and resume safely after restart.
- [x] Expose start/pause/resume/status APIs including phase, pages/items, last response, backoff and coverage.
- [x] Keep interactive directory listing available while scan is running and coordinate scans with remote mutations. Directory-page requests from interactive browsing and full inventory scans share a cancellation-aware minimum interval; download-link acquisition retains the configured transfer concurrency.

### Task 5: Web cloud manager and local download path settings

**Files:** Modify `internal/webui/webui.go`, embed management page/assets, config persistence/startup wiring, release defaults.

- [x] Add authenticated directory browser with pagination, breadcrumb navigation and clear cache timestamp/completeness.
- [x] Add create/move/rename/delete forms with previews, destination conflict checks and per-operation status.
- [x] Manual intake remains a move into the configured queue directory and relies on normal queue discovery; verify the current UI wording/API before release.
- [x] Display/edit queue and local download paths; reject changes while tasks or remote maintenance are active, and lock paths explicitly supplied by `.env` or the process environment.
- [x] Decouple SQLite/sidecar state from the download destination, verify and retire the legacy nested queue store on first startup, and persist the state volume in the preview Compose deployment.
- [x] Add crash-safe local directory migration with no-overwrite behavior, verification-before-source-removal and restart recovery.
- [x] Add inventory scan controls, collapsible progress panel and local directory-size refresh using cached inventory only.

### Task 6: Duplicate and empty-folder cleanup

**Files:** Add cleanup preview/execute services and authenticated UI flows; tests.

- [x] Find duplicate groups only from SHA-256 calculated from streamed file bytes and size; missing/untrusted digest is ineligible.
- [x] Present canonical keep item and explicit delete candidates; retain a deterministic default but require preview confirmation.
- [x] Recheck every item before deleting and stop the affected group after unknown outcome.
- [x] Find empty directory subtrees only within complete inventory; preview leaf-first, live-check each directory before deleting, and record each remote result.

**Implementation steps:**

1. [x] Add SQLite v8 account-scoped verified SHA-256 cache, resumable duplicate scan state, append-only transition events, persisted retry delay/deadline, and migration coverage.
2. [x] Add a single low-frequency scanner that requires complete non-stale inventory, prefilters by repeated size, streams candidate bytes through SHA-256, rechecks metadata before/after each read, persists each verified identity/cursor, and resumes transient-error backoff from its stored UTC deadline after restart.
3. [x] Add authenticated status/start/pause/preview endpoints; preview groups only current matching SHA-256 and size, displays a deterministic keep item and every delete candidate with file and timestamp evidence.
4. [x] Require a matching content SHA-256 in delete requests; re-download/hash and recheck identity immediately before sending, journal each mutation, and stop after failure or unknown result.
5. [x] Add collapsible UI progress and preview/execute controls that leave browsing usable; document bandwidth/runtime and the final external-client race boundary.

**Ruling:** The current Baidu `md5` field remains metadata only. Hash every file whose size collides, rather than relying on `size + md5` as a candidate filter; this may consume more bandwidth but avoids silently missing duplicates when provider metadata is incomplete or inconsistent. A preview is ready for local simulated-provider testing; real Baidu/NAS acceptance remains pending.

### Task 7: End-to-end local verification and preview packaging

**Files:** relevant tests, `release/TESTING.md`, release package metadata.

- [x] Run focused unit/integration tests, SQLite restart tests, API mock tests, full Go tests, vet and build.
- [x] Smoke the packaged Windows app and verify startup, SQLite integrity/schema, login/API authentication and local migration status endpoint.
- [x] Build immutable preview ZIP with a content-addressed working-tree manifest, SHA256, run instructions and explicit real-Baidu/NAS acceptance boundaries. Preview .45 contains the current cloud management, managed paths, inventory sizing, and duplicate/empty-directory cleanup implementation; ZIP SHA256 `2A79E3B604E9C3BFAF39B94898BE6E0F6ABA7FB20F9819550C5FCBC57945AB8D`.
- [x] Following the user's explicit target-directory authorization, update release files in `v0.8.30-preview.24` to preview .45 and verify the target executable version and hash; do not copy authorization or queue data.
