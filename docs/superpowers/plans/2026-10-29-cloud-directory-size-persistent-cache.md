# Cloud Directory Size Persistent Cache Implementation Plan

> **For agentic workers:** Execute this plan task by task with TDD. Steps use checkbox syntax for tracking.

**Goal:** Reuse computed cloud directory sizes across repeated requests and restarts while invalidating them whenever the local cloud inventory changes.

**Architecture:** Store one monotonically increasing inventory generation per account and one derived size row per account/path in SQLite. Successful inventory page commits and explicit inventory invalidations increment the account generation transactionally. `CloudDirectorySize` serves a cache row only when its generation matches; otherwise it calculates from the existing inventory tables and persists the result. Keep separate local computation and remote observation timestamps.

**Tech Stack:** Go, `database/sql`, modernc SQLite.

**Spec:** `docs/superpowers/specs/2026-10-29-cloud-management-cache.zh-CN.md`

## Global Constraints

- SQLite schema v14 upgrades transactionally to v15 and remains bound to the existing queue scope.
- A generation change and its inventory update/invalidation commit in the same SQLite transaction.
- A stale-generation size row must never be returned as current.
- Keep cloud `ObservedAtUTC` separate from local `CachedAtUTC`, both in UTC.
- Preserve existing bytes/files/directories/completeness semantics and account isolation.
- Do not skip or weaken SQLite integrity, schema, scope, or synchronous checks.

## Review Focus

- Inventory page failure rolls back both data and generation so old valid size rows stay valid.
- A page for another account does not invalidate this account's size cache.
- Remote invalidation changes completeness and forces recomputation after a prior cache hit.
- Cache rows survive restart and remain usable only for their persisted generation.
- A v14 database migrates and recovers safely if schema commit precedes manifest publication.

---

### Task 1: Add regression tests for persistent size cache behavior

**Files:** `internal/taskstore/cloud_inventory_test.go`

- [x] Added a root/child regression asserting same-generation reads retain `CachedAtUTC` while preserving the provider `ObservedAtUTC`.
- [x] Closed and reopened SQLite; the same size result and local cache timestamp read back unchanged.
- [x] Changed a child file size in a committed inventory page; root bytes/files refresh under a newer generation.
- [x] Invalidated a child path; root completeness changes to false and the old cache timestamp is replaced.
- [x] RED run failed because `CloudDirectorySize.CachedAtUTC` did not exist; the implemented focused regression passes.

### Task 2: Add v15 schema and migration

**Files:** `internal/taskstore/sqlite.go`, `internal/taskstore/store.go`, `internal/taskstore/migration.go`, migration tests.

- [x] Added both account generation and directory-size cache tables to fresh SQLite stores.
- [x] Added `migrateV14`, which transactionally creates the tables and changes schema version 14 to 15; after commit it verifies integrity/tables before publishing manifest v15.
- [x] Routed v1-v13 migration chains through v14 after v13 and updated the required schema-object count.
- [x] Migration tests preserve existing inventory and recover the committed-v15/unpublished-v14-manifest state.
- [x] Targeted v1-v14 migration tests and the complete taskstore suite pass.

### Task 3: Invalidate by account generation

**Files:** `internal/taskstore/cloud_inventory.go`, `internal/taskstore/cloud_inventory_test.go`

- [x] Added one helper to increment account generation with a local UTC microsecond timestamp inside the caller's transaction.
- [x] Inventory page commits and explicit path invalidation increment generation transactionally.
- [x] Failed page trigger regression proves the old generation and cached size remain valid; another account's page does not affect it.
- [x] Cloud inventory unit tests pass.

### Task 4: Serve and persist derived directory sizes

**Files:** `internal/taskstore/cloud_inventory.go`, `internal/taskstore/cloud_inventory_test.go`, `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`.

- [x] Added `cached_at_utc` to `CloudDirectorySize` for the separate local computation time.
- [x] Size reads return a generation-matching row or aggregate and persist the result in one serialized SQLite transaction.
- [x] Kept provider observation and local cache times distinct and displayed both in the management page.
- [x] Regressions cover hits, recomputation, invalidation, account isolation, transaction rollback and UI labels.
- [x] `go test ./internal/taskstore -count=1` and `node scripts/webui.test.cjs` pass; full Go suite is included in the release gate.

### Task 5: Measure and package

**Files:** `internal/taskstore/cloud_inventory_test.go`, `docs/ai/experience-plan.md`, `docs/ai/progress.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`.

- [x] 50,000-file subtree benchmark separated aggregation/persist from same-generation hit. Across three 5-iteration samples, aggregation was 53.4–61.2 ms, 6,579–7,398 B and 169–171 allocations; cache hit was 0.172–0.196 ms, 2,824–3,924 B and 72–75 allocations. These Windows local-disk results are not NAS/SMB estimates.
- [x] `go vet ./...`, the package release gate, payload/extracted hashes, Windows version, and authorized preview.24 target directory verification pass for v0.8.30-preview.49.
- [x] Recorded schema migration, benchmark, package version/hash, and external acceptance limits in `docs/ai/progress.md` and the Chinese cloud management roadmap.
