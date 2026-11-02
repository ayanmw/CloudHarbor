# Directory Refresh Change Details Implementation Plan

> **For agentic workers:** Implement directly in the primary checkout. Do not use a worktree, access the real Baidu account, commit, push, or deploy. Steps use checkbox syntax.

**Goal:** Persist and show the specific cached entries added, removed, reidentified, or modified when a directory refresh publishes against a complete prior cache.

**Architecture:** Extend `.92`'s complete-baseline comparison to produce compact old/new snapshots. Persist them in a new append-only SQLite table with parent event sequence and local/provider UTC evidence, atomically with the directory publish. Add an account-scoped bounded API and an on-demand paginated detail view; preserve existing summary counts and full-run export behavior.

**Tech Stack:** Go, database/sql, modernc SQLite, embedded HTML/JavaScript, Node tests.

**Spec:** `docs/superpowers/specs/2026-10-30-directory-refresh-change-details.zh-CN.md`

## Global Constraints

- Only complete prior directory coverage can produce change details.
- Keep `.92` count semantics: identity replacement counts as one add plus one remove.
- Do not invent a same-path pairing if either side is ambiguous.
- Persist cache publish, refresh state, parent event and detail rows in one transaction.
- Preserve local recorded UTC separately from provider listing observed UTC.
- No cloud request/write, no account fingerprint in responses, and no automatic detail pruning.
- Use the primary checkout and preserve unrelated dirty work.

## Review Focus

- First observation and incomplete baseline never create speculative add/remove rows.
- A same-path fs_id replacement pairs only when unique; path ambiguity stays as separate add/delete rows.
- Cached SHA-256 appears only when its path/size/mtime identity matches the snapshot.
- Failure inserting a later detail row rolls back every earlier detail row and the parent publish.
- Stable detail paging and stale UI responses remain bound to the selected run and parent event.

---

### Task 1: Model and classify refresh change snapshots

**Files:** `internal/taskstore/cloud_inventory.go`, `internal/taskstore/cloud_inventory_test.go`

- [x] Add regression cases for added, removed, changed, same-path identity replacement, ambiguous path, complete empty result, and unknown baseline.
- [x] Extend the complete-baseline comparison to return typed details with old/new metadata, observed times, and only identity-matching verified SHA-256 evidence.
- [x] Keep `.92` aggregate count semantics unchanged and run focused Store tests.

### Task 2: Persist details atomically and migrate schema 21→22

**Files:** `internal/taskstore/sqlite.go`, `internal/taskstore/store.go`, `internal/taskstore/migration.go`, `internal/taskstore/migration_test.go`, `internal/taskstore/cloud_inventory.go`, `internal/taskstore/cloud_inventory_test.go`

- [x] Test migration, historical null/no-detail behavior, manifest recovery, table constraints, and append-only triggers.
- [x] Add the detail table, order index, validation, schema version 22 and crash-recoverable migration; prior migrations must safely recognize a committed schema 22 while healing an old manifest.
- [x] Insert snapshots after the parent publish event obtains its sequence, using one prepared statement and the current transaction.
- [x] Inject detail-row failure after at least one prior detail insert; assert inventory, generation, state, event and all detail rows roll back.
- [x] Run full taskstore tests.

### Task 3: Add bounded account-scoped detail reads

**Files:** `internal/taskstore/cloud_inventory.go`, `internal/downloader/cloud_inventory_scan.go`, `internal/intakeapi/server.go`, related tests

- [x] Add Store paging by account/run/event and a descending stable detail sequence cursor, limit 1–50.
- [x] Add authenticated Downloader/API routing and validate run ID, event sequence, cursor, limit, duplicate and unknown parameters.
- [x] Test account/run/event isolation, empty pages, cursor order, limit bounds, and fingerprint privacy.

### Task 4: Display details on demand

**Files:** `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`

- [x] Add an explicit details action per changed directory publication and render before/after metadata, IDs, hashes and both UTC timestamps as escaped text.
- [x] Page at most 50 detail rows; keep the cursor bound to the selected run/event and reset it on selection/filter changes.
- [x] Test empty/error states, stale responses, pagination, escaping, unknown baseline and unchanged complete-run export.
- [x] Run full WebUI and userscript suites.

### Task 5: Validate and package `.93`

**Files:** `docs/ai/progress.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/release-versioning.zh-CN.md`

- [x] Run `go test -count=1 ./...`, `go vet ./...`, WebUI/userscript suites, and `git diff --check`.
- [x] Package `v0.8.30-preview.93`; verify all three build targets, ZIP entry/checksum counts, source snapshot, and no private runtime data.
- [x] Record validation in the releases directory; preserve unrelated workspace changes.
