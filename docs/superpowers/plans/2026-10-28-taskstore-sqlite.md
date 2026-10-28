# SQLite Taskstore Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace per-job JSON files with a transactionally consistent SQLite job projection and append-only evidence history while preserving restart isolation and legacy records.

**Architecture:** Keep `internal/taskstore.Store` as the single-writer façade and preserve sidecar APIs for intake, credentials, account binding, and settings. Store jobs and events in SQLite, with each projection update and evidence append in one transaction. Import existing v1 job files through a verified temporary database and atomically switch the format manifest only after validation.

**Tech Stack:** Go 1.22, `database/sql`, pure-Go SQLite driver, SQLite rollback journal.

**Spec:** `docs/superpowers/specs/2026-10-28-taskstore-sqlite-design.zh-CN.md`

## Global Constraints

- Keep Windows AMD64, Linux AMD64, and Linux ARM64 builds without CGO.
- Keep the existing `writer.lock` single-writer behavior and sidecar file formats/APIs.
- Use `journal_mode=DELETE`, `synchronous=EXTRA`, foreign keys enabled, one database connection, and bounded busy timeout; verify effective values at open.
- Keep current task projection and its event append in one transaction.
- Keep local observation time, provider timestamps, nullable external occurrence times, and future retry deadlines semantically distinct.
- Persist `sending` before network I/O; recover persisted `sending` as `unknown`, without replay or manual release.
- Never fabricate historical transitions during v1 import; preserve existing job JSON and timestamps.
- Fail closed on unsupported format, scope mismatch, corruption, incomplete migration, or failed durability settings.
- Do not merge intake journal, credentials, account binding, or settings into the job database.

## Review Focus

- Crash at every v1-to-SQLite publication boundary: startup must select exactly one authoritative format or fail closed.
- A failed transaction must leave both the old projection and no corresponding new event.
- A saved `sending` operation must remain `unknown` after reopen and never be replayed.
- Clock rollback must not reorder the append sequence; absent provider occurrence time must remain NULL.
- Sidecar intake/credential/settings operations must continue to work under the same writer lock during and after migration.

---

### Task 1: SQLite schema, pragmas, and event-aware store operations

**Files:**
- Modify: `internal/taskstore/store.go`
- Create: `internal/taskstore/sqlite.go`
- Test: `internal/taskstore/sqlite_test.go`

**Interfaces:**
- Preserve `Open`, `Save`, `SaveBatch`, `LoadEach`, `LoadEachRaw`, `Load`, and `Close` signatures.
- Add `SaveWithEvent(id int64, data []byte, event Event) error` and `SaveBatchWithEvents(records []Record, events []Event) error`; ordinary existing callers remain source-compatible and receive a generated generic state-change event.
- Define event fields for monotonically increasing sequence, job ID, event kind, local `recorded_at_utc`, optional `occurred_at_utc`, optional `cloud_observed_at_utc` and `response_observed_at_utc`, run/operation/attempt IDs, evidence category, and sanitized JSON evidence.

- [x] Write tests for fresh schema/pragmas, projection plus event atomicity, ordered callback loading, duplicate IDs, corruption, and closed-store errors.
- [x] Run `go test ./internal/taskstore -run 'SQLite' -count=1`; verify new tests fail because SQLite store is absent.
- [x] Implement schema creation, open-time scope/version/integrity/pragmas verification, one-connection configuration, transactional Save/SaveBatch, and event query helpers.
- [x] Run targeted taskstore tests and existing taskstore suite; require all pass.

### Task 2: v1 import and authoritative format switch

**Files:**
- Modify: `internal/taskstore/store.go`
- Create: `internal/taskstore/migration.go`
- Test: `internal/taskstore/migration_test.go`

**Interfaces:**
- `Open` detects existing v1 manifest and imports under the already-held `writer.lock`.
- SQLite manifest version becomes authoritative only after temporary DB close/reopen, integrity checks, and per-ID payload equivalence checks.

- [x] Write migration tests for multi-record legacy import, sidecar preservation, malformed record, interrupted pre-switch import, and authoritative SQLite corruption.
- [x] Run migration tests and confirm expected failure before implementation.
- [x] Implement staged import preserving raw job payloads and original `Created`/`Updated` values in `legacy_import` event evidence; missing values remain explicit `null`, with no fabricated transition time/history.
- [x] Verify pre-switch failure leaves v1 intact, post-switch failure never silently falls back, and SQLite corruption fails closed.
- [x] Run taskstore suite and migration interruption tests.

### Task 3: Downloader transition evidence and sidecar compatibility

**Files:**
- Modify: `internal/downloader/scheduler.go`
- Modify: `internal/downloader/source_operation.go`
- Test: `internal/downloader/crash_source_test.go`
- Test: `internal/downloader/scheduler_test.go`
- Test: `internal/credentials/manager_test.go`

**Interfaces:**
- `scheduler.persist` atomically saves the queue job and event; callers identify transition kind and operation metadata.
- `beginSourceOperation` persists `sending` evidence before the HTTP request; `finishSourceOperation` records confirmed, explicit failure, or unknown evidence with only timestamps actually observed.

- [x] Add failure-injection tests for projection/event all-or-none persistence, missing provider occurrence time, source response lost, startup recovery, and download/source operation ID linkage.
- [x] Verify key new assertions fail before the corresponding event writes are added.
- [x] Route downloader writes through transactional event-aware Store APIs while preserving current lifecycle and recovery semantics.
- [x] Confirm intake, credentials, account binding, and settings still use their existing sidecars and lock; add reopen compatibility assertions.
- [x] Run downloader, taskstore, credential, and intake suites.

### Task 4: Platform, crash, and package verification

**Files:**
- Modify: `docs/ai/experience-plan.md`
- Create: `docs/ai/progress.md` entry and release validation record
- Create: immutable independent preview package and SHA256 manifest

- [x] Run full `go test -count=1 ./...` and `go vet ./...` on Windows; run full `go test -race -count=1 ./...` on Go 1.22.2 WSL; run Node UI tests (17 passed).
- [x] Build Windows AMD64, Linux AMD64, and Linux ARM64 with CGO disabled.
- [x] Run package startup/auth/stop smoke; verify release payload hashes and Linux ELF architectures. SQLite migration/crash failure paths are covered by the taskstore and downloader regression suites.
- [x] Record target NAS/SMB locking and power-loss behavior as unverified until tested on the actual deployment filesystem; do not claim local tests prove NAS durability.
- [x] Publish immutable `v0.8.23-preview.1` with exact commit, SHA256, run instructions, and evidence boundaries.
