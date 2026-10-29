# Cloud Inventory Persistent Work Queue Implementation Plan

> **For agentic workers:** This plan is being executed inline. Use superpowers:executing-plans for later changes to this plan. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Keep full-cloud scan progress durable without serializing or returning the entire pending-directory list on every page and status poll.

**Architecture:** Replace the `pending_json` worklist with a SQLite FIFO table keyed by account, run, and sequence. Commit a final directory page, its discovered child directories, removal of the completed queue head, and scan counters/cursor in one transaction. Keep the legacy column for migration compatibility, backfill it in v12→v13 migration, and expose only a pending-directory count to the web UI.

**Tech Stack:** Go, SQLite, existing taskstore and downloader scan worker.

**Spec:** `docs/superpowers/specs/2026-10-29-cloud-management-cache.zh-CN.md`

## Global Constraints

- Preserve one provider page as the cache checkpoint and keep directory listings marked complete only after their final page.
- Preserve resumable pagination offsets, retry/backoff state, account scoping, and low-frequency cloud API requests.
- A crash must not lose discovered child directories or advance scan progress without committing that page.
- Do not return cloud paths or the complete worklist in periodic scan status responses.
- Keep v12 stores and independent preview packaging supported.

## Review Focus

- v12 `pending_json` migration preserves FIFO order, current directory, and offset.
- Replaying a committed final page cannot duplicate queued paths or lose child directories.
- A failed cache transaction cannot advance the work queue or counters.
- Pause/restart resumes the current directory and offset from SQLite.
- Empty and complete queues report correct status and pending count.

---

### Task 1: Add the durable work queue and v12→v13 migration

**Files:** `internal/taskstore/sqlite.go`, `internal/taskstore/store.go`, `internal/taskstore/migration.go`, `internal/taskstore/migration_test.go`, `internal/taskstore/cloud_inventory.go`.

- [x] Add a queue table with per-account/run FIFO sequence and unique path constraints.
- [x] Backfill existing `pending_json` records transactionally and validate queue state before publishing schema v13.
- [x] Add store operations to initialize a run, read the head/count, save retry/pause state, and atomically commit an inventory page with its scan advancement.
- [x] Test migration ordering, empty state, duplicate prevention, rollback, and restart recovery.

### Task 2: Switch the cloud scanner and status UI

**Files:** `internal/downloader/cloud_inventory_scan.go`, `internal/downloader/cloud_inventory_scan_test.go`, `internal/taskstore/cloud_inventory_test.go`, `internal/intakeapi/server_test.go`, `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`.

- [x] Remove the in-memory full worklist from scan execution and consume one durable queue head at a time.
- [x] Enqueue children and remove the completed directory only in the same SQLite transaction as its final inventory page.
- [x] Return `pending_count` instead of serializing pending paths; update the progress display.
- [x] Verify queue count/order, API response fields, pagination, pause/restart, rollback and remote mutation locking.

### Task 3: Verify and release

- [x] Run focused taskstore/downloader/API/UI regressions and the full package release gate.
- [x] Package a new independent preview and verify its manifest, payload hashes, and Windows version command.
- [x] Record exact package evidence and the remaining live Baidu/NAS acceptance boundary.
