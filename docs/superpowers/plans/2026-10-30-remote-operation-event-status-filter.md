# Remote Operation Event Status Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Work in the existing checkout; do not create a worktree.

**Goal:** Filter one remote operation's local evidence timeline by event status and continue sequence paging within that filter.

**Architecture:** Add an allowlisted optional status to the existing ID-scoped read-only event query and API. Keep filter and request generation per operation in the standalone page; applying a filter discards that operation's old pages.

**Tech Stack:** Go, SQLite, `database/sql`, `net/http`, embedded HTML/JavaScript, Node `node:test`.

**Spec:** `docs/superpowers/specs/2026-10-30-remote-operation-event-status-filter.zh-CN.md`

## Global Constraints

- Event reads stay scoped to the validated 32-character operation ID.
- Accepted status values are `prepared`, `confirmed`, `unknown`, `failed`; empty means all.
- Keep `sequence < before_sequence`, page size 1..50, and no-store behavior.
- Keep recursive evidence redaction and current unknown isolation.
- No Baidu API, cloud mutation, event mutation, or schema change.

## Review Focus

- Filtered empty results must not be confused with unknown operation IDs.
- Older event pages use the same operation ID and status as the first page.
- Changing or clearing a filter must not mix old events into the new timeline.
- A delayed response for an old filter cannot replace the current status view.

### Task 1: Store and API

**Files:** `internal/taskstore/remote_operations.go`, `internal/taskstore/remote_operations_test.go`, `internal/intakeapi/cloud_operations.go`, `internal/intakeapi/cloud_operations_test.go`, `internal/downloader/cloud_operations.go`

- [x] Add failing Store tests for status filtering, cursor, empty result, operation-ID isolation, and invalid status.
- [x] Extend the Store event-page query and downloader façade with optional status.
- [x] Add strict API parsing and tests for filtered response, duplicate/invalid status, 404 and redaction.
- [x] Run focused Store/downloader/API tests.

### Task 2: Standalone history UI

**Files:** `internal/webui/res/cloud_operations.js`, `scripts/webui.test.cjs`

- [x] Add failing tests for applying status, resetting cursor, retaining it on older-page requests, clearing, and stale-response suppression.
- [x] Add a status selector and explicit apply/clear controls to each event panel.
- [x] Keep event cache and request generation per operation ID.
- [x] Run full WebUI/userscript suites.

### Task 3: Release verification and roadmap

**Files:** `docs/ai/progress.md`, `docs/ai/release-versioning.zh-CN.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`

- [x] Run full Go tests, vet, WebUI/userscript suites, and diff checks.
- [x] Build `.73`; verify platform binaries, version, ZIP manifest, checksums, and private-data exclusions.
- [x] Record release hash and external acceptance boundaries.
