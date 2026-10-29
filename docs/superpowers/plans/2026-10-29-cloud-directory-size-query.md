# Cloud Directory Size Query Optimization Plan

> **For agentic workers:** This plan was executed inline. Use superpowers:executing-plans for later changes.

**Goal:** Reduce repeated SQLite subtree reads when refreshing a cached cloud directory's size while preserving its coverage and observation-time semantics.

**Architecture:** Keep file-byte aggregation on the existing account/path index. Combine directory existence/completeness, descendant directory count, and oldest complete observation into one account-scoped path-range aggregate instead of four separate directory queries.

**Tech Stack:** Go, `database/sql`, modernc SQLite.

**Spec:** `docs/superpowers/specs/2026-10-29-cloud-management-cache.zh-CN.md`

## Global Constraints

- Keep cloud inventory account-scoped and local-only; do not turn cached values into claims of current remote state.
- Preserve root/current-directory inclusion for completeness and observation time, while excluding the current directory from its descendant count.
- Preserve incomplete-coverage behavior, sibling-prefix boundaries, and UTC observation timestamps.
- Do not add schema/migration state for a query-only consolidation.

## Review Focus

- Missing current-directory cache row remains incomplete.
- Any incomplete descendant keeps the result incomplete.
- Sibling paths such as `/docs0` stay outside `/docs`.
- Directory count excludes the requested directory itself.
- Observation time remains the oldest complete observation in the queried subtree, including the current directory when complete.

## Task 1: Combine directory metadata reads

**Files:** `internal/taskstore/cloud_inventory.go`, `internal/taskstore/cloud_inventory_test.go`

- [x] Add a single indexed aggregate over the requested directory and its bounded descendant path range.
- [x] Replace the separate current-state, incomplete count, descendant count, and observation-time queries while keeping file byte aggregation unchanged.
- [x] Verify result semantics with existing complete, incomplete, persistence, account-isolation, and sibling-prefix tests.
- [x] Add a query-plan test requiring the directory aggregate to use the `(account_fingerprint,path)` range rather than a table scan.
- [x] Compare old five-query behavior with the two-query implementation on a synthetic cache containing 50,000 directories and 50,000 files. Three runs measured 71–77 ms / 3,328–3,329 B / 103 allocs versus 52–53 ms / 2,440–2,448 B / 58 allocs on this Windows host. This is a local comparative benchmark, not NAS performance evidence.
- [x] Run full release gates and package the independent preview. v0.8.30-preview.46 ZIP SHA-256: `EB07E398FA4353FFC0662C336FA07B56B5433320E0FB60A90C64450879AE016E`.
- [x] Update the user-authorized `v0.8.30-preview.24` target directory to preview .46 and verify its Windows executable version and hash.
