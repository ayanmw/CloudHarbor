# Cloud Duplicate Preview Pagination Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render duplicate preview groups in bounded 25-group pages without changing the full preview deletion set.

**Architecture:** Keep the current complete `duplicateGroups` response in memory and add a page offset for rendering. Page controls update only that offset; keeper selections remain on group objects, while refresh replaces the response and resets the offset to zero.

**Tech Stack:** Embedded HTML/JavaScript and Node `node:test` VM page harness.

**Spec:** `docs/superpowers/specs/2026-10-30-cloud-duplicate-preview-pagination.zh-CN.md`

## Global Constraints

- Render at most 25 groups at a time.
- Batch deletion and its confirmation continue to use all loaded preview groups.
- Page changes make no network requests and preserve page-local keeper choices.
- Refreshing the preview fetches the existing endpoint, resets to page zero, and replaces prior selections.

## Review Focus

- Exactly 25 and 26 groups show correct previous/next states and ranges.
- 51 groups have a final single-group page and return to the preceding page correctly.
- Page navigation preserves a keeper selection stored in an off-screen group.
- The visible item actions keep absolute group indexes into the complete array.
- Empty results display a valid empty state and disable both navigation buttons.

### Task 1: Add failing pagination regressions

**Files:** `scripts/webui.test.cjs`

- [x] Test 0, 25, 26, and 51 groups, range text, maximum 25 rendered groups, and navigation bounds.
- [x] Test page navigation performs no fetch and preserves keeper choices across pages, with absolute group indexes.
- [x] Test batch deletion count still includes off-page candidates.
- [x] Run focused tests and confirm they fail because preview pagination controls are missing.

### Task 2: Add page-bounded duplicate rendering

**Files:** `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`

- [x] Add previous/next controls and a status label to the duplicate preview card.
- [x] Add `duplicatePageSize = 25` and page-offset navigation with clamping.
- [x] Render only the page slice but preserve absolute group indexes on action buttons.
- [x] Reset to page zero after a newly fetched preview; keep `duplicateDeleteCount` and batch deletion on the complete array.
- [x] Run focused pagination and keeper-selection regressions.

### Task 3: Release `.80`

**Files:** `docs/ai/progress.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/release-versioning.zh-CN.md`

- [x] Run full Go tests, vet, WebUI/userscript suites, and `git diff --check`.
- [x] Package `v0.8.30-preview.80` for Windows AMD64, Linux AMD64, and Linux ARM64.
- [x] Verify ZIP version/checksums/source snapshot and private runtime data exclusions.
- [x] Record SHA-256 and clarify that pagination bounds DOM rendering only; response data remains complete and no live deletion was tested.
