# Standalone Read-only Remote Operation History Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Work in the existing checkout; do not create a worktree.

**Goal:** Give the existing read-only remote operation history its own `/operations` page without duplicating its API or controls.

**Architecture:** Move the history markup from the cloud-management page into a dedicated embedded HTML page, retain `cloud_operations.js`, and add a direct link from cloud management. Keep all data reads on the existing protected SQLite-only endpoints.

**Tech Stack:** Go `embed` and `net/http`, HTML, browser JavaScript, Node `node:test`.

**Spec:** `docs/superpowers/specs/2026-10-30-remote-operation-history-standalone.zh-CN.md`

## Global Constraints

- `/operations` is read-only and uses the current management session.
- Reuse existing SQLite-only API; do not add provider calls or remote mutations.
- Keep existing filters, pagination, exports, evidence redaction, HTML escaping, and unknown isolation.
- The page must not contain cloud file browser or mutation controls.
- Do not add a worktree or touch the real-account runtime directory.

## Review Focus

- The embedded route and navigation link resolve to the same history feature.
- Removing the history section from `/cloud` does not remove unrelated inventory cleanup handlers or scripts.
- The standalone page initializes history without requiring a hidden panel interaction.
- Existing history tests run against the dedicated page; XSS and unknown-result behavior remain covered.

### Task 1: Embedded page and route

**Files:** `internal/webui/index_operations.html`, `internal/webui/webui.go`, `main.go`, `internal/webui/index_cloud.html`

- [x] Add a failing page/route test for `/operations` and its read-only content.
- [x] Embed the page and register the route.
- [x] Move history markup and its script from `/cloud`; add navigation links both ways.
- [x] Verify page is self-contained and contains no cloud mutation controls.

### Task 2: Reuse history interactions and test them on the standalone page

**Files:** `internal/webui/res/cloud_operations.js`, `scripts/webui.test.cjs`

- [x] Add a failing test for automatic first-page load on `/operations`.
- [x] Initialize history on the standalone route while preserving lazy loading on `/cloud` if any history foldout remains.
- [x] Move history interaction tests to the standalone page and retain filter/paging/export/event/XSS coverage.
- [x] Run the complete WebUI and userscript suites.

### Task 3: Release verification and roadmap

**Files:** `docs/ai/progress.md`, `docs/ai/release-versioning.zh-CN.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`

- [x] Run full Go tests, vet, WebUI/userscript suites, and diff checks.
- [x] Build `.72`; verify platform binaries, version, ZIP manifest, checksums, and private-data exclusions.
- [x] Record release hash and external acceptance boundaries.
