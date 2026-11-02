# Cloud Duplicate Keeper Selection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user select which verified duplicate remains in each group before deleting other copies.

**Architecture:** Keep the selection in the existing page-local `duplicateGroups` array. Swap the chosen inventory entry into `keep`, move the prior keeper into `delete`, then render from that group. No API, SQLite, or remote mutation is added.

**Tech Stack:** Embedded HTML/JavaScript and Node `node:test` VM page harness.

**Spec:** `docs/superpowers/specs/2026-10-30-cloud-duplicate-keeper-selection.zh-CN.md`

## Global Constraints

- Do not add remote mutations or requests for keeper selection.
- Preserve the server-provided default keeper and reset to it on a fresh preview response.
- Preserve explicit deletion confirmation, expected object identity, and expected SHA-256 verification.
- Do not persist keeper preferences or modify SQLite/API contracts.

## Review Focus

- Selecting a candidate changes only the current preview data and makes the old keeper a delete candidate.
- A fresh preview response discards the temporary preference and displays its returned keeper.
- Batch deletion count and candidate list match the selected preview state.
- Candidate paths remain escaped, and mutation requests use only the selected preview's existing inventory entries.

### Task 1: Add preview-selection regressions

**Files:** `scripts/webui.test.cjs`

- [x] Add a test with two duplicate copies asserting the “set keeper” action performs no fetch and swaps `keep`/`delete` roles.
- [x] Assert the refresh-reset copy and batch-delete path still use only the current candidate list and expected SHA-256.
- [x] Run focused tests and confirm they fail because the selection note and handler were missing.

### Task 2: Implement and verify the UI selection

**Files:** `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`

- [x] Add a concise note that keeper choices are page-local and reset by refreshing the preview.
- [x] Add `setDuplicateKeeper(groupIndex, candidateIndex)` to swap the chosen verified entry with the current keeper and re-render.
- [x] Add a “设为保留项” button to each deletion candidate using existing escaped rendering and data indexes.
- [x] Re-run focused tests and the complete WebUI/userscript suites.

### Task 3: Release `.79`

**Files:** `docs/ai/progress.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/release-versioning.zh-CN.md`

- [x] Run full Go tests, vet, WebUI/userscript suites, and `git diff --check`.
- [x] Build the three platform binaries and package `v0.8.30-preview.79`.
- [x] Verify package version, ZIP checksums/source snapshot, and absence of private runtime data.
- [x] Record SHA-256 and explicitly state no live cloud deletion or real browser acceptance was performed.
