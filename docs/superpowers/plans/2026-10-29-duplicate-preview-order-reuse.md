# Duplicate Preview Ordering Reuse Plan

> **For agentic workers:** This plan was executed inline. Use superpowers:executing-plans for later changes.

**Goal:** Remove redundant per-group sorting while assembling verified duplicate previews.

**Architecture:** The SQLite query already orders rows by SHA-256, path, and fs_id. Keep using that deterministic order to choose the first path as the canonical item and append remaining group members in deletion order; do not sort each group's deletion slice again in Go.

**Tech Stack:** Go, `database/sql`, modernc SQLite.

**Spec:** `docs/superpowers/specs/2026-10-29-cloud-management-cache.zh-CN.md`

## Global Constraints

- The keep item remains the lexicographically smallest path, with fs_id as tie breaker.
- Every delete candidate remains ordered lexicographically by path.
- Group identity remains SHA-256 plus file size, and only current inventory identities in complete directories are eligible.
- Do not rely on insertion order; SQL's explicit `ORDER BY` must remain the ordering source.

## Task 1: Reuse the SQL result order

**Files:** `internal/taskstore/cloud_duplicates.go`, `internal/taskstore/cloud_duplicates_test.go`

- [x] Remove redundant `sort.Slice` from per-group preview assembly.
- [x] Extend regression data with files inserted in non-path order and verify canonical keep and sorted deletion candidates.
- [x] Benchmark 3,000 groups with 3 files each, three runs: previous path sorting used 7,509,475–7,509,606 B and 212,857–212,858 allocations; direct ordered append used 7,004,705–7,005,939 B and 203,855–203,861 allocations. Elapsed time varied and is not claimed as a stable speedup.
- [x] Run full release gates and package the independent preview as v0.8.30-preview.48; WebUI 22, userscript 3, all Go tests, vet, and Windows/Linux AMD64/Linux ARM64 builds pass. Verify 12 payload hashes and extracted ZIP files, run the Windows binary version check, and update the authorized preview.24 directory with matching hashes. ZIP SHA-256: `6F7D357C53DF0D47DCD66903D208868BAA6F13992F78E4CE03E60FA30F3F4E60`.
