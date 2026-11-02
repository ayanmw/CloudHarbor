# Local Directory Migration Event Timeline Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Work in the existing main checkout; do not create a worktree.

**Goal:** Browse a selected migration's ordered event history inline with bounded pages and redacted evidence.

**Architecture:** Read each ID-scoped event page from a read-only SQLite snapshot, expose a protected cursor API, and attach an on-demand timeline control to each migration summary row.

**Tech Stack:** Go, SQLite, `database/sql`, `net/http`, server-rendered HTML/JavaScript, Node `node:test`.

**Spec:** `docs/superpowers/specs/2026-10-30-local-migration-event-timeline.zh-CN.md`

## Global Constraints

- Event access must remain scoped to the requested 32-character hex migration ID.
- Page size defaults to 20 and is limited to 1..50; pages are newest-first by sequence.
- Older pages use `before_sequence` strictly less than the prior page's minimum sequence.
- SQLite reads are read-only; API evidence is recursively redacted and no-store.
- Never add event mutation, migration recovery, file IO, or Baidu API access.

## Review Focus

- A record with no events returns an empty page without confusing unknown ID.
- Equal timestamps still paginate by unique sequence, with no missing/duplicate events.
- The cursor cannot cause events from another migration to be returned.
- Secret-like nested evidence fields and HTML payloads are safe in the UI.
- A delayed page for an old selected migration cannot replace the current timeline.

---

### Task 1: Store event-page query

**Files:** `internal/taskstore/local_migrations.go`, `internal/taskstore/local_migrations_test.go`

- [x] Write failing tests for latest-page order, older cursor, ID isolation, empty event page, unknown ID, and invalid bounds.
- [x] Add a bounded read-only event-page method returning events and `has_more`/next cursor metadata.
- [x] Run focused and full `internal/taskstore` tests.

### Task 2: API and inline page UI

**Files:** `internal/downloader/local_migration.go`, `internal/intakeapi/server.go`, `internal/intakeapi/server_test.go`, `internal/intakeapi/local_migration_history_test.go`, `main.go`, `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`

- [x] Add protected ID-scoped GET route with strict query and ID validation.
- [x] Test UTC fields, redaction, no-store, unknown ID, and safe failures.
- [x] Add on-demand timeline expansion and older-event paging under the correct row.
- [x] Test path/event HTML escaping and stale response suppression after selection changes.

### Task 3: Release verification and roadmap

**Files:** `docs/ai/progress.md`, `docs/ai/release-versioning.zh-CN.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`

- [x] Run full Go tests, vet, WebUI/userscript suites, and diff checks.
- [x] Build `.70`; verify platform binaries, version, ZIP manifest, and private-data exclusions.
- [x] Record release hash and external acceptance boundaries.
