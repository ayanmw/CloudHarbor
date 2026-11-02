# Directory Refresh Delta Summary Implementation Plan

> **For agentic workers:** Implement directly in the primary checkout. Do not use a worktree, access the real Baidu account, commit, push, or deploy. Steps use checkbox syntax.

**Goal:** Persist and display added, removed, and changed item counts for each directory refresh that has a complete prior cache baseline.

**Architecture:** Add nullable counters to the append-only SQLite refresh-event table and migrate schema 20 to 21. During the existing final publish transaction, compare the complete prior direct-child inventory to the complete staged provider listing by `fs_id`; serialize the counters through the existing event API/export and render them in the current history timeline.

**Tech Stack:** Go, `database/sql`, modernc SQLite, embedded WebUI HTML/JavaScript, Node WebUI tests.

**Spec:** `docs/superpowers/specs/2026-10-30-directory-refresh-delta-summary.zh-CN.md`

## Global Constraints

- Preserve one local SQLite writer, transactionally update cache/state/events, and append-only event history.
- Keep local event-recorded UTC separate from provider listing-observed UTC.
- Use null counters when no complete prior directory baseline exists; never infer removals from incomplete data.
- Do not add per-file event rows, remote calls, remote mutations, or account fingerprint output.
- Keep the previous complete cache browseable while pages are staged; only publish after the full directory listing.
- Use the existing primary checkout and preserve all unrelated dirty files.

## Review Focus

- Existing complete empty directory vs. unknown/incomplete old cache: test zero counts versus null counters.
- Same `fs_id` with changed metadata: count one changed item, not removal plus addition.
- Cross-parent move observed in separate refreshes: old parent reports removal, new parent addition.
- SQLite event insert failure: prove the cache publish and refresh checkpoint roll back with the event.
- Restart after schema commit but before manifest update: recover schema 21 without losing prior event rows.

---

### Task 1: Define and test direct-directory delta classification

**Files:**
- Modify: `internal/taskstore/cloud_inventory.go`
- Test: `internal/taskstore/cloud_inventory_test.go`

**Interfaces:**
- Add an internal `CloudInventoryRefreshDelta` value with nullable `AddedCount`, `RemovedCount`, and `ChangedCount` fields.
- Add an internal transaction helper that compares one account/path's complete cached direct children with the supplied complete provider entries. It returns unknown counters unless the cached directory status is `complete`.

- [x] Add tests for complete cache: added/removed/changed/unchanged counts, name/size/mtime/MD5 changes, and a zero-delta listing.
- [x] Run the targeted tests and confirm failure because delta fields were absent.
- [x] Implement classification by `fs_id`; a shared ID with any tracked field difference is changed, while IDs present on one side only are added or removed.
- [x] Run targeted tests and confirm all cases pass.

### Task 2: Persist per-directory counts atomically and migrate schema 20 to 21

**Files:**
- Modify: `internal/taskstore/cloud_inventory.go`
- Modify: `internal/taskstore/migration.go`
- Modify: `internal/taskstore/store.go`
- Modify: `internal/taskstore/sqlite.go`
- Test: `internal/taskstore/cloud_inventory_test.go`
- Test: `internal/taskstore/migration_test.go`

**Interfaces:**
- Extend `CloudInventoryRefreshEvent` with nullable added/removed/changed counts.
- Add nullable non-negative integer columns to `cloud_inventory_refresh_events`; only `directory_published` rows with complete prior coverage receive values.
- Extend event insertion and list/export queries to round-trip these fields.

- [x] Add a Store regression that publishes a refresh directory and asserts counts are committed with inventory, directory status, and event in one transaction.
- [x] Inject a directory-event insert failure and assert the cache and refresh checkpoint roll back.
- [x] Add schema 20→21 migration/reopen coverage for old event preservation, null historical counters, and manifest recovery after schema commit.
- [x] Run the focused Store/migration tests and confirm the expected failures before implementation.
- [x] Implement delta computation in the final directory publish transaction, persist counters on that same `directory_published` event, and implement the atomic 20→21 migration with crash recovery.
- [x] Run focused tests, including existing schema 18→20 migration cases.

### Task 3: Show counts in refresh history and evidence export

**Files:**
- Modify: `internal/webui/index_cloud.html`
- Test: `scripts/webui.test.cjs`
- Modify if required by compiler/export assembly: `internal/intakeapi/server.go`
- Test if required: `internal/intakeapi/server_test.go`

**Interfaces:**
- Existing refresh event and per-run evidence JSON include the three optional counters through `CloudInventoryRefreshEvent` JSON fields.
- Render known counts as `新增 N · 移除 N · 变化 N`; render null counts as `无完整旧缓存基线`.

- [x] Add page assertions for populated counts, zero counts, null counters, and escaped directory paths.
- [x] Run the focused WebUI test and confirm failure before implementation.
- [x] Render counters in the event timeline; do not change existing timestamps, pagination, event selection, or export size limits.
- [x] Run the focused WebUI test and the complete WebUI/userscript suites.

### Task 4: Verify and package preview `.92`

**Files:**
- Modify: `docs/ai/cloud-management-roadmap.zh-CN.md`
- Modify: `docs/ai/progress.md`
- Modify: `docs/ai/release-versioning.zh-CN.md`
- Create: `D:/projects/Git-Projects/nas_baidupan_auto_downloader-releases/v0.8.30-preview.92-validation.md`

- [x] Run `go test -count=1 ./...`, `go vet ./...`, `node --test scripts/webui.test.cjs`, `node --test scripts/userscript.test.cjs`, and `git diff --check`.
- [x] Package `v0.8.30-preview.92` using `scripts/package.ps1`; confirm Windows AMD64/Linux AMD64/Linux ARM64 builds.
- [x] Verify the 13 ZIP entries, all 12 payload hashes, source snapshot hash, and absence of private runtime data.
- [x] Record test evidence and the local/mock-only boundary, update the `.92` roadmap, and leave all unrelated work untouched.
