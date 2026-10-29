# Cloud Scan Queue Insert Optimization Plan

> **For agentic workers:** This plan was executed inline. Use superpowers:executing-plans for later changes.

**Goal:** Reduce transient allocations when a completed inventory directory enqueues its child directories.

**Architecture:** Prepare the account/run/sequence/path insert once per final-page transaction and execute it for each child. Preserve one-row `INSERT OR IGNORE` behavior so each row's `RowsAffected` continues to update the durable pending count precisely.

**Tech Stack:** Go, `database/sql`, modernc SQLite.

**Spec:** `docs/superpowers/specs/2026-10-29-cloud-management-cache.zh-CN.md`

## Global Constraints

- Queue rows, pending count, inventory page, and scan progress remain in the same SQLite transaction.
- Duplicate paths remain ignored and must not inflate `pending_count`.
- Any insert or statement error rolls back the whole page and does not advance in-memory scan state.
- Preserve FIFO sequence values, including gaps when duplicate paths are ignored.

## Task 1: Reuse the child queue insert statement

**Files:** `internal/taskstore/cloud_inventory.go`, `internal/taskstore/cloud_inventory_test.go`

- [x] Prepare one queue insert statement for a non-empty child list and reuse it throughout the final-page transaction.
- [x] Keep per-row `RowsAffected` accounting and explicit statement close before queue-head lookup.
- [x] Cover duplicate child path counting, sequence order, page rollback on insert trigger failure, and final-page progress.
- [x] Compare 100 queue inserts with individual `tx.Exec` calls and a transaction-reused prepared statement. Three runs measured 34,698–34,822 B / 1,131 allocs versus 33,378–33,406 B / 1,036 allocs; elapsed time remained roughly 4.5–5.0 ms for both. This is a local SQLite allocation result, not NAS throughput evidence.
- [x] Run full release gates and package the independent preview. v0.8.30-preview.47 ZIP SHA-256: `AE1BA97EB3F143B875D038441418F908F08456C0CDC45915BD254251C1EA4BA5`.
- [x] Update the user-authorized `v0.8.30-preview.24` target directory to preview .47 and verify its Windows executable version and hash.
