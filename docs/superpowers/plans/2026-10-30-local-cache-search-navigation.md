# 本地缓存搜索结果导航实施计划

> **For agentic workers:** Execute inline in the existing main checkout; do not use a worktree. Preserve unrelated dirty changes.

**Goal:** Let users open a local cache search hit's parent directory without leaving cache mode or querying Baidu.

**Architecture:** Add a second navigation action to search rows. It clears the active search and opens the result's parent path in existing paginated cached-directory browsing; the current live-parent action stays unchanged.

**Tech Stack:** Embedded HTML/JavaScript, Node `node:test`, existing Go release gates.

**Spec:** `docs/superpowers/specs/2026-10-30-local-cache-search-navigation.zh-CN.md`

## Constraints

- Cached navigation calls only `/api/cloud-inventory`; live navigation continues to call `/api/cloud`.
- Cached rows remain read-only and keep existing coverage and observation labels.
- No schema, endpoint, dependency, or real-account writes.

## Review Focus

- Clicking a cached result must clear the search field and open the correct parent at offset zero.
- Stale or incomplete inventory remains labeled by the normal cached-directory response.
- The existing live handoff remains available and fetches fresh provider data.

### Task 1: Cache parent navigation

**Files:**
- Modify: `internal/webui/index_cloud.html`
- Test: `scripts/webui.test.cjs`

- [x] Add regressions that open a file result's parent or a directory result itself in cache mode, clear the search, call the paginated inventory endpoint, and never call the live endpoint.
- [x] Run the focused WebUI test and confirm the missing cache navigation fails.
- [x] Add the cache parent/directory actions and event handling while preserving live navigation.
- [x] Run the focused WebUI test and confirm both navigation paths pass.

### Task 2: Release `.84`

**Files:**
- Modify: `docs/ai/progress.md`
- Modify: `docs/ai/release-versioning.zh-CN.md`
- Modify: `docs/ai/cloud-management-roadmap.zh-CN.md`

- [x] Run WebUI/userscript, full Go tests/vet, and Windows AMD64/Linux AMD64/Linux ARM64 package gates.
- [x] Build/update `v0.8.30-preview.84` in the independent releases directory; verify ZIP hashes and private-data exclusion.
- [x] Record package hash, source snapshot, evidence and unverified live-environment boundaries.
