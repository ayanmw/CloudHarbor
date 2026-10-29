# Cloud Inventory Page Upserts Implementation Plan

> **For agentic workers:** This plan was executed inline. Use superpowers:executing-plans for later changes to this work.

**Goal:** Reduce local SQLite statement setup cost while persisting each provider inventory page atomically.

**Architecture:** Prepare the stable file and directory upsert statements once per page transaction, execute them for each row, then close them before the existing reconciliation and commit path. Preserve the existing SQLite durability settings and page-level restart cursor.

**Tech Stack:** Go, `database/sql`, modernc SQLite.

**Spec:** [Cloud management and inventory cache](../specs/2026-10-29-cloud-management-cache.zh-CN.md)

## Constraints

- Keep one provider page as the atomic transaction and restart checkpoint.
- Do not weaken SQLite synchronous or journaling settings.
- Preserve account, fs_id, path, and last-seen conflict update semantics.

## Task: Reuse page upsert statements

**Files:** `internal/taskstore/cloud_inventory.go`, `internal/taskstore/cloud_inventory_test.go`

- [x] Add a 100-entry page benchmark reporting time and allocations.
- [x] Record the pre-change benchmark before changing the write path.
- [x] Prepare the file and child-directory upsert statements once per transaction and reuse them for page rows.
- [x] Run the taskstore tests and compare the benchmark with the baseline.
