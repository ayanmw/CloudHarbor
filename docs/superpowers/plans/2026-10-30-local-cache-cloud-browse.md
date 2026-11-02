# Local Cache Cloud Browse Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an explicit, read-only local inventory browsing mode to the cloud management page, with cache coverage and observation time visible.

**Architecture:** Reuse the protected `GET /api/cloud-inventory` endpoint and SQLite listing; leave `GET /api/cloud` as the live provider path. Keep live mode as the default, request only the selected source, and hide remote mutations while cached mode is selected.

**Tech Stack:** Go HTTP API, embedded HTML/JavaScript, Node `node:test` UI checks, existing SQLite inventory.

**Spec:** `docs/superpowers/specs/2026-10-30-local-cache-cloud-browse.zh-CN.md`

## Global Constraints

- No schema or dependency changes.
- Cached mode never falls back to a Baidu request.
- Cached mode is read-only; remote mutations continue to use fresh live listings.
- Cached directory rows include locally aggregated size and coverage; never replace provider size used for remote identity.
- Missing, stale, or scanning cache must not be presented as a complete empty directory.
- Keep full-account scan and directory-refresh semantics unchanged.
- Do not use a worktree; preserve existing unrelated dirty changes.

## Review Focus

- Directory with no cache metadata: show “no cache record”, not an empty directory.
- Stale/scanning directory with retained rows: show rows with an incomplete warning and no write controls.
- Complete empty directory: show an empty result with complete status and observation time.
- Switching source or directory during pagination resets offset and requests only the selected source.
- Returning to live mode restores existing remote mutation controls and identity data.

---

### Task 1: Add source selector and cached-mode behavior

**Files:**
- Modify: `internal/webui/index_cloud.html`
- Test: `scripts/webui.test.cjs`

**Interfaces:**
- Selector `#browse-source` has `live` (default) and `cache` values.
- `browse()` uses `/api/cloud` in live mode and `/api/cloud-inventory` in cache mode.
- Cached response metadata is `metadata.status` and `metadata.observed_at_utc`.

- [x] Add failing UI tests for default live selection, cached endpoint selection, status/observation display, and cached-only mutation suppression.
- [x] Run `node scripts/webui.test.cjs` and verify failures are due to missing cached-mode behavior.
- [x] Implement source-specific browsing and reset `offset` when the source changes.
- [x] Render distinct complete, stale/scanning, and missing-cache messages; keep pagination, directory navigation, and local size view working.
- [x] Run `node scripts/webui.test.cjs` and verify the new behaviors pass.

### Task 2: Include local aggregate size on cached directory rows

**Files:**
- Modify: `internal/taskstore/cloud_inventory.go`
- Test: `internal/taskstore/cloud_inventory_test.go`
- Modify: `internal/webui/index_cloud.html`
- Test: `scripts/webui.test.cjs`

**Interface:** Cached directory rows add optional `directory_size_bytes` and `directory_size_complete` fields, computed through the existing generation-scoped SQLite directory size cache. File `size` and provider identity fields remain unchanged.

- [x] Add a Store regression proving complete directory size is returned and stale coverage retains bytes but marks the size incomplete; verify it fails before implementation.
- [x] Add WebUI assertions for complete and incomplete cached directory sizes; verify they fail before implementation.
- [x] Enrich cached list directory entries locally and render size/coverage in cache mode without extra provider requests; calculate all directory aggregates for one page in a shared SQLite transaction.
- [x] Run focused Store and WebUI regressions.

### Task 3: Verify contract and update preview.56

**Files:**
- Test if needed: `internal/intakeapi/server_test.go`
- Modify: `docs/ai/cloud-management-roadmap.zh-CN.md`
- Modify: `docs/ai/release-versioning.zh-CN.md`
- Modify: `docs/ai/progress.md`

- [x] Run full Go tests, vet, WebUI/userscript checks, and Windows AMD64/Linux AMD64/Linux ARM64 builds.
- [x] Update `v0.8.30-preview.56`; verify ZIP hashes, metadata, and private runtime exclusions.
- [x] Record cache size behavior and evidence boundaries without promoting local fixtures to external acceptance.

## Completion Criteria

- Live mode remains default and unchanged.
- Cache mode uses the local inventory endpoint only, shows timestamp/coverage, is paginated and read-only, and never silently falls back to live.
- A complete empty cache is distinguishable from missing/stale/scanning cache.
- Cached directory rows display local aggregate size and mark incomplete coverage as incomplete.
- `.56` package gates pass and a testable package is delivered.
