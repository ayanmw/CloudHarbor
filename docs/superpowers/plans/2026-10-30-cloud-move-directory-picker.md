# Cloud Move Destination Directory Picker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Work in the existing checkout; do not create a worktree. Steps use checkbox syntax.

**Goal:** Replace hand-typed cloud move destination paths with a paginated, read-only directory picker.

**Architecture:** Add a native dialog to the embedded cloud-management page and navigate it with the existing live cloud-list GET endpoint. The final selection calls the existing mutation function exactly once; server identity checks, queue pause, result quarantine, and cache refresh remain authoritative.

**Tech Stack:** Embedded HTML/JavaScript, Go `embed`, Node `node:test`.

**Spec:** `docs/superpowers/specs/2026-10-30-cloud-move-directory-picker.zh-CN.md`

## Global Constraints

- Browse calls are GET-only; no remote mutation occurs until explicit final selection.
- Keep existing source identity recheck and unknown-result isolation.
- Keep cache mode read-only and keep move controls limited to live browsing.
- Do not add API endpoints or dependencies.
- Do not create a worktree or touch the real-account runtime directory.

## Review Focus

- A failed or stale directory-list response must not select a different destination.
- Cancel/Escape must not mutate the cloud object.
- A rapid folder navigation must ignore an older response.
- Names and paths containing HTML must be rendered as text.
- The selected source identity must remain the row originally clicked.

### Task 1: Add picker markup and interaction regressions

**Files:** `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`

- [x] Add test proving move opens a picker at the current live directory and fetches its first page.
- [x] Add tests for folder navigation, up navigation, pagination, target confirmation, cancel, escaping, and stale response rejection.
- [x] Verify the tests fail because move currently uses a free-text prompt.

### Task 2: Implement the read-only picker and explicit move action

**Files:** `internal/webui/index_cloud.html`

- [x] Add accessible native dialog with source path, current directory, folder list, previous/next, up, choose-current, and cancel controls.
- [x] Load directories with the existing `/api/cloud` GET, use request generations to ignore stale responses, and render names with `escapeHTML`.
- [x] Replace the move prompt with picker opening; call existing `mutate('move', source, {destination_directory})` only after explicit confirmation.
- [x] Run the focused picker tests and complete WebUI suite.

### Task 3: Verify and package `.78`

**Files:** `docs/ai/progress.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/release-versioning.zh-CN.md`

- [x] Run full Go tests, `go vet ./...`, WebUI/userscript suites, and `git diff --check`.
- [x] Build `v0.8.30-preview.78`; verify Windows AMD64/Linux AMD64/Linux ARM64 binaries, package version, ZIP checksums, source snapshot, and private-data exclusions.
- [x] Keep the existing `.77` runtime unchanged; acceptance here is UI/API-mock coverage and never moves a real cloud object.
- [x] Record package SHA-256, test evidence, and external acceptance boundary.
