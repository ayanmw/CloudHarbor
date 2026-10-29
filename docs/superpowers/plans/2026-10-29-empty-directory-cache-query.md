# Empty Directory Cache Query Implementation Plan

> **For agentic workers:** This plan is being executed inline. Use superpowers:executing-plans for later changes to this work.

**Goal:** Avoid constructing every root-to-descendant directory pair when previewing empty cloud directories from the full local inventory cache.

**Architecture:** Replace the recursive closure with indexed path-range anti-existence checks for descendant files and incomplete or untracked descendant directories. Keep only complete directory roots whose entire known subtree is complete and file-free, ordered deepest first for safe leaf-to-root execution.

**Tech Stack:** Go, SQLite, `database/sql`.

**Spec:** [Cloud management and inventory cache](../specs/2026-10-29-cloud-management-cache.zh-CN.md)

## Constraints

- Preserve candidate semantics: a candidate and all descendant directories are complete, no descendant file exists, and candidates sort leaf-first.
- Treat a descendant directory with no directory-cache row as incomplete.
- Use `(account_fingerprint,path)` ranges to exclude sibling prefixes such as `/a` versus `/a0`.
- Keep live provider directory recheck before each irreversible delete unchanged.

## Tasks

### Task 1: Query-plan regression

**Files:** `internal/taskstore/cloud_inventory_test.go`

- [x] Assert empty-directory preview uses indexed path ranges and no recursive subtree closure; current code failed to compile with the new query-plan regression before implementation.

### Task 2: Replace recursive closure

**Files:** `internal/taskstore/cloud_inventory.go`, `internal/taskstore/cloud_inventory_test.go`

- [x] Add a query using account-scoped descendant path ranges for files and incomplete/untracked directory rows.
- [x] Preserve existing root eligibility, output fields and deepest-first ordering.
- [x] Verify complete nested empty folders, non-empty subtrees, missing/stale cache rows and sibling-prefix boundaries.

### Task 3: Verify and release

- [x] Taskstore full tests and downloader empty-directory checks pass; full release gate, three-platform builds, 12 payload hashes and packaged Windows version command pass for preview.40.
