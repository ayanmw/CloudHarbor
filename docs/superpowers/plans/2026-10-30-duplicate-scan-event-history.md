# Duplicate Scan Event History Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose durable duplicate-scan state transitions as a paginated read-only timeline.

**Architecture:** Reuse the existing append-only SQLite event table. Add an account-scoped newest-first Store page with a `before_sequence` cursor, expose it through an authenticated GET endpoint, then add an explicit on-demand history panel in the cloud management page.

**Tech Stack:** Go, SQLite, embedded HTML/JavaScript, `node:test` WebUI harness.

**Spec:** `docs/superpowers/specs/2026-10-30-duplicate-scan-event-history.zh-CN.md`

## Global Constraints

- Read existing local SQLite events only; do not call Baidu or alter scan state.
- Default page size is 20; maximum page size is 50.
- The first response is newest-first in pagination but the visible timeline is chronological.
- No retry, resume, delete, or remote mutation controls are added.

## Review Focus

- Sequence cursors do not duplicate or skip events when new events are appended between requests.
- Account fingerprints never appear in the response.
- Malformed JSON evidence or unsafe text cannot break the page or inject markup.
- Empty history and Store/API errors preserve a usable retry path.
- Rapidly closing/reopening or selecting another history response cannot let an older response overwrite the current panel.

### Task 1: Store cursor page

**Files:** `internal/taskstore/cloud_duplicates.go`, `internal/taskstore/cloud_duplicates_test.go`

- [x] Add failing tests for newest-first page, older cursor, has-more detection, empty results, account isolation, and bounds.
- [x] Implement account-scoped read-only event pagination with limit+1 detection.
- [x] Run focused Store tests.

### Task 2: Authenticated API

**Files:** `internal/downloader/cloud_duplicate_scan.go`, `internal/intakeapi/server.go`, `internal/intakeapi/cloud_operations.go`, `internal/intakeapi/server_test.go`, `main.go`

- [x] Add failing API tests for default/max page, cursor, duplicate/invalid parameters, and backend failure.
- [x] Wire GET `/api/cloud-cleanup/duplicate-scan/events` into the existing management auth wrapper.
- [x] Run focused API/downloader tests.

### Task 3: On-demand UI timeline

**Files:** `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`

- [x] Add failing tests for explicit load, chronological rendering, older page prepend/dedup, escaping, empty/failure states, and stale response handling.
- [x] Add a collapsed history panel with explicit read and older-event controls; do not add polling.
- [x] Run WebUI/userscript tests.

### Task 4: `.82` release

**Files:** `docs/ai/progress.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/release-versioning.zh-CN.md`

- [x] Run full tests, vet, `git diff --check`, package `v0.8.30-preview.82` across all three targets, and verify checksum/source snapshot/private-data exclusions.
- [x] Record release hash and explicitly state the feature is read-only; no real-account writes.
