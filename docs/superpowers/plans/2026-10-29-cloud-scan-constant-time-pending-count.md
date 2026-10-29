# Cloud Scan Constant-Time Pending Count Implementation Plan

> **For agentic workers:** This plan is being executed inline. Use superpowers:executing-plans for later changes. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Make cloud scan status polling independent of the number of queued directories.

**Architecture:** Persist `pending_count` on the cloud scan state row. Initialize it with the root entry, update it in the same transaction as queue insert/delete and page progress, and read it directly for status. Migrate v13 stores by deriving the count once from the durable queue; check queue-head consistency while advancing to fail closed without recounting all rows.

**Tech Stack:** Go, SQLite, existing taskstore cloud scan queue.

**Spec:** `docs/superpowers/specs/2026-10-29-cloud-management-cache.zh-CN.md`

## Constraints

- Keep queue mutation, scan counters/cursor and pending count in one SQLite transaction.
- Do not run a full queue count during status polls or page commits.
- Never report completion if the persisted count says empty while a queue head remains, or vice versa.
- Preserve v13 worklist migration and restart semantics; update fresh stores to schema v14.

## Tasks

### Task 1: Persist and migrate the count

**Files:** `internal/taskstore/sqlite.go`, `internal/taskstore/store.go`, `internal/taskstore/migration.go`, `internal/taskstore/cloud_inventory.go`, taskstore tests.

- [x] Add `pending_count` to fresh schema and v13→v14 migration; verify manifest recovery and count reconciliation.
- [x] Read the count directly from scan state; update it from actual inserted queue rows and one removed head in the page transaction.
- [x] Reject missing/extra queue heads at advancement boundaries without `COUNT(*)`.
- [x] Test large counts, insert/delete/replay, rollback, and v13 migration/recovery.

### Task 2: Verify and release

- [x] Run taskstore/downloader/API/UI regressions and full package release gate.
- [x] Package a new independent preview; verify payload checksums and Windows version.
- [x] Record the release hash and live Baidu/NAS limits.
