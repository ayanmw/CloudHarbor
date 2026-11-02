# 局部目录刷新分页漂移证据实施计划

> **For agentic workers:** Use `superpowers:executing-plans` inline in the primary checkout. Do not create/use a worktree, and do not commit or push.

**Goal:** Complete a five-result preview batch for safe scoped-refresh duplicate rechecks and durable, searchable evidence.

**Architecture:** Keep the existing two-restart transaction and request-budget rules. Make both duplicate sources return typed IDs, persist the original offset and optional ID with the append-only restart event in SQLite v40, then expose a fixed-reason filter and the new evidence through the existing authenticated history page/API/export.

**Tech Stack:** Go, SQLite, `net/http`, embedded HTML/JS, Node `node:test`.

**Spec:** `docs/superpowers/specs/2026-11-02-scoped-refresh-drift-evidence.zh-CN.md`

## Global Constraints

- A directory may automatically restart no more than twice per run; event history remains the source of truth.
- Reserve every Baidu list GET durably before sending it.
- Keep the last published inventory visible until the full refreshed directory publishes.
- Never infer new evidence for old events; keep new nullable fields NULL in v39 history.
- Preserve event sequence, append-only triggers, indexes, account isolation, and redaction.
- Preserve the existing dirty checkout; no worktree, commit, or push.

## Review Focus

1. Duplicate ID extraction must not parse error strings or break `errors.Is` classification.
2. Failed restart transaction must not clear staging or advance an event independently.
3. Third restart must stop before another GET; budget exhaustion must preserve the first-page checkpoint.
4. Migration must not fabricate old offset/ID evidence or change immutable event rows.
5. Event filter must not change full-run evidence export semantics or expose account fingerprint.

---

### Task 1: Typed duplicate identities

**Files:** `internal/baiduapi/client.go`, `internal/baiduapi/client_test.go`, `internal/taskstore/cloud_inventory.go`, `internal/taskstore/cloud_inventory_test.go`.

- [x] Add tests that provider-page duplicate and cross-staging duplicate errors expose the repeated positive fs_id and still satisfy `errors.Is` for their existing sentinel.
- [x] Run the focused tests and confirm they fail because the typed accessors are missing.
- [x] Add typed wrapped errors without changing existing sentinel matching or public error summaries.
- [x] Run Baidu API and taskstore focused tests.

### Task 2: SQLite v40 restart evidence

**Files:** `internal/taskstore/sqlite.go`, `internal/taskstore/store.go`, `internal/taskstore/migration.go`, `internal/taskstore/migration_test.go`, `internal/taskstore/cloud_inventory.go`, `internal/taskstore/cloud_inventory_test.go`.

- [x] Add RED tests for v39→v40, restart offset/ID persistence, old event NULLs, reason/ID validation, append-only behavior, and rollback on failed event insert.
- [x] Add nullable `restart_offset` and `restart_fs_id` columns and update schema validation/migration recovery transactionally.
- [x] Extend event insertion, scanning, pagination, and evidence export; make the boundary restart transaction accept original offset and optional typed ID.
- [x] Verify migration preserves pre-existing event sequence/index/trigger and passes integrity check after close/reopen.
- [x] Run migration and taskstore suites.

### Task 3: Scoped refresh duplicate recheck integration

**Files:** `internal/downloader/cloud_inventory_scan.go`, `internal/downloader/cloud_inventory_scan_test.go`.

- [x] Extend integration assertions for provider and staged-page duplicates: exact cause, fs_id, original offset, durable request reservation, restart count, and old cache visibility before final publish.
- [x] Add a pure boundary mismatch case with `restart_fs_id=NULL`.
- [x] Verify third drift fails without an additional provider request and quota pause/reopen resumes from page one.
- [x] Pass typed offset and duplicate ID to the store restart transaction; run focused tests, then downloader suite.

### Task 4: Filtered read-only history API and full export

**Files:** `internal/downloader/downloader.go`, `internal/intakeapi/server.go`, `internal/intakeapi/cloud_operations.go` or the existing cloud-history handler file, relevant API tests, `main.go`, `main_test.go`.

- [x] Add RED API tests for combined `kind` + `restart_reason`, invalid/repeated reason values, stable pagination, new fields, and export redaction.
- [x] Add a fixed enum restart-reason filter to the run event query; old rows without reason remain eligible only when no reason filter is set.
- [x] Ensure list DTO and whole-run export carry nullable offset/fs_id while excluding fingerprint/scope.
- [x] Run API and exporter focused tests.

### Task 5: Event history workbench UI

**Files:** `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`.

- [x] Add UI RED tests for reason filtering across pages, clearing to the full timeline, safe offset/fs_id rendering, legacy NULL labels, and export retaining complete-run semantics.
- [x] Add reason selector and preserve it through older-page requests; clear it when changing run and reset to the first event page.
- [x] Render recorded offset and duplicate fs_id only when recorded; keep page text escaped and keep the whole-run export button explicit.
- [x] Run WebUI and userscript checks.

### Task 6: `.148` release gate and real state handoff

**Files:** `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/release-versioning.zh-CN.md`, `docs/ai/progress.md`, `release/TESTING.md`, this spec/plan, releases validation record.

- [x] Record all five user outcomes and identify the next unnumbered candidate.
- [x] Run `go test ./... -count=1`, `go vet ./...`, WebUI/userscript, and `scripts/package.ps1 -Version v0.8.30-preview.148 -AllowDirty`.
- [x] Check ZIP entries, payload hashes, source snapshot, release documentation, and absence of private runtime state.
- [x] Confirm `.147` account ready and queue idle, then move the single `.nas-downloader` directory to `.148`; compare all pre/post file hashes and start `.148` for local read-only checks.
- [x] Do not trigger a full cloud scan, download, or remote mutation during handoff.
