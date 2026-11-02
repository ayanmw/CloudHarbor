# 本地缓存全盘搜索实施计划

> **For agentic workers:** Execute this plan inline in the existing main checkout; do not use a worktree. Track steps with checkbox syntax and preserve unrelated dirty changes.

**Goal:** Add a bounded, read-only search over the current account's local Baidu inventory cache, with explicit coverage status and a safe path back to live browsing.

**Architecture:** Add a paginated SQLite search scoped to the authorized account, a protected read-only HTTP endpoint, and a cache-mode UI search form. Search rows never carry forward as mutation identities; locating a result switches to a fresh live listing of its parent directory.

**Tech Stack:** Go, existing SQLite taskstore, protected HTTP API, embedded HTML/JavaScript, Node `node:test`.

**Spec:** `docs/superpowers/specs/2026-10-30-local-cache-search.zh-CN.md`

## Global Constraints

- No schema, dependency, remote request, or worktree changes.
- Bind every search query to the currently authorized account fingerprint.
- Cap pages at 100 entries and reject empty or over-128-character terms.
- Surface incomplete cache coverage; zero results never claim remote absence unless the complete full-account inventory is current.
- Cached results are read-only; all subsequent remote work must use a fresh live listing.
- Preserve existing dirty changes and the real `.56` runtime sidecar.

## Review Focus

- Wildcard characters are literal, not an unbounded SQL pattern: test `%` and `_` against multiple names.
- A result from another account never leaks: seed the same matching name in two accounts and assert each sees only its own row.
- Empty/no-match results on incomplete coverage are not reported as proof of absence: test scanning, stale directory, no scan state, and complete state.
- Search responses remain bounded and page consistently: test limit, offset, total, and `next_offset`.
- Search results cannot become mutation identities: UI exposes only live-parent navigation, which resets source and offset and issues `/api/cloud`.

---

### 任务 1：SQLite 搜索与覆盖证据

**Files:**
- Modify: `internal/taskstore/cloud_inventory.go`
- Test: `internal/taskstore/cloud_inventory_test.go`

**Interfaces:**
- Add `SearchCloudInventory(account, query string, offset, limit int) (entries []CloudInventoryEntry, total int, coverage CloudInventorySearchCoverage, err error)`.
- Add coverage fields `Status`, `IncompleteDirectories`, and optional `ScanUpdatedAtUTC`.

- [x] Add RED tests for literal substring results, account scope, paging, directory sizes, and coverage states.
- [x] Run focused tests and confirm missing method/type fails as expected.
- [x] Implement parameterized SQL substring search and coverage calculation from full-scan state and incomplete directories.
- [x] Run focused Store tests and confirm green.

### 任务 2：受保护的只读搜索 API

**Files:**
- Modify: `internal/intakeapi/server.go`
- Test: `internal/intakeapi/server_test.go`
- Modify: `internal/downloader/cloud_inventory_scan.go`
- Modify: `main.go`

**Interfaces:**
- Add `SearchCachedCloud(context.Context, string, int, int) (taskstore.CloudInventorySearchResult, error)` to the downloader and `Backend` contracts.
- Add `GET /api/cloud-inventory/search` with query and pagination validation; response is taskstore search result.

- [x] Add RED API tests for successful results, invalid input, wrong method, and zero live-list calls.
- [x] Implement the handler, account-scoped Store delegation, and GET route.
- [x] Run focused intake API tests.

### 任务 3：缓存模式搜索与实时列表跳转

**Files:**
- Modify: `internal/webui/index_cloud.html`
- Test: `scripts/webui.test.cjs`

- [x] Add RED UI checks for cache-only search, coverage warning, safe result rendering and live-parent navigation.
- [x] Confirm checks fail before implementation, then implement cache search and fresh live-parent navigation.
- [x] Run `node scripts/webui.test.cjs` and confirm 24 tests pass.

### 任务 4：发布 `.57`

**Files:**
- Modify: `docs/ai/release-versioning.zh-CN.md`
- Modify: `docs/ai/cloud-management-roadmap.zh-CN.md`
- Modify: `docs/ai/progress.md`

- [x] Run full Go tests, vet, WebUI/userscript checks, Windows/Linux AMD64/Linux ARM64 builds, ZIP hash checks, and private-runtime exclusion checks.
- [x] Build `.57` as an independent preview and preserve the running `.56` service and runtime data.
- [x] Record package hash, test coverage, and the fact that this feature has not had real-account UI acceptance.
