# Cloud Inventory Batched Upserts Implementation Plan

> **For agentic workers:** Use TDD and execute this plan task by task.

**Goal:** Reduce SQLite calls and transient overhead while persisting each provider inventory page atomically.

**Architecture:** Process unique page identities in original order, batching only contiguous file runs into multi-row upserts of at most 80 rows (880 bind variables); handle each directory row and its path pruning in the original sequence. Pages with duplicate fs_id values retain the existing sequential last-write-wins path. Directory metadata, stale reconciliation, scan queue/counters and generation remain in the same transaction.

**Tech Stack:** Go, `database/sql`, modernc SQLite.

**Spec:** `docs/superpowers/specs/2026-10-29-cloud-inventory-batched-upserts.zh-CN.md`

## Global Constraints

- Keep the provider page as the atomic cache and restart checkpoint.
- Preserve path/identity pruning and stale cleanup semantics.
- Keep the batch at 80 rows maximum to remain below SQLite's 999 bind-variable limit.
- Preserve repeated `fs_id` page semantics through ordered fallback.

## Review Focus

- File-only and mixed directory pages retain all observed metadata.
- A moved/replaced directory is pruned and marked stale before its page commits.
- A duplicate fs_id within a page keeps the last row's values.
- Failure in any batch rolls back all page rows, pruning, generation and scan state.
- A 100-item page is split into bounded 80+20 row statements.

---

### Task 1: Pin the page semantics and baseline

**Files:** `internal/taskstore/cloud_inventory_test.go`

- [x] Add regressions for duplicate fs_id last-write-wins and trigger failure in the later batch rolling back the first batch.
- [x] Run focused tests and confirm the rollback case exercises the page transaction, durable scan state, queue and generation.
- [x] Record the current 100-entry benchmark baseline: 10.07/12.35/10.11 ms, about 80 KB and 1,739 allocations per page (Windows AMD64, three samples).

### Task 2: Batch unique page identities

**Files:** `internal/taskstore/cloud_inventory.go`, `internal/taskstore/cloud_inventory_test.go`

- [x] Preserve directory path/identity pruning in its original item order, batching only file runs between directory entries.
- [x] Add a multi-row upsert builder/executor capped at 80 entries and use it for unique fs_id runs.
- [x] Retain sequential ordered writes when fs_id repeats in one provider page.
- [x] Run directory move/replacement, path-overlap, duplicate identity, stale cleanup, scan rollback, and full taskstore tests.

### Task 3: Measure and release

**Files:** `docs/ai/progress.md`, `docs/ai/experience-plan.md`, `docs/superpowers/plans/2026-10-29-cloud-inventory-batched-upserts.md`.

- [x] Paired Windows AMD64 synthetic comparison (`-benchtime=30x -count=7`): batched median 4.86 ms, 99,433 B, 688 allocs; prior sequential median 9.87 ms, 79,857 B, 1,738 allocs. The observed median time was 50.8% lower and allocations 60.4% fewer, while bytes allocated increased 24.5%; individual timings varied, so these results do not establish NAS throughput.
- [x] Run WebUI/userscript, all Go tests, vet and Windows/Linux AMD64/Linux ARM64 package gates.
- [x] Package preview.51, verify ZIP and every listed payload hash plus Windows version, update the previously authorized preview.24 directory, and document live-service acceptance limits.
