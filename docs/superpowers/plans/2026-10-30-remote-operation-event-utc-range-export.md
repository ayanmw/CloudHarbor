# Remote Operation Event UTC Range and Export Plan

> **For agentic workers:** Work in the existing checkout; do not create a worktree.

**Goal:** Filter one operation's event timeline by UTC calendar dates and export the filtered, redacted evidence as one bounded snapshot.

**Architecture:** Extend the existing operation-scoped status filter with UTC date bounds in Store/API/UI. Add a read-only filtered evidence export using one SQLite transaction and the existing 100-event cap. Preserve the current unfiltered export endpoint.

**Spec:** `docs/superpowers/specs/2026-10-30-remote-operation-event-utc-range-export.zh-CN.md`

## Constraints

- UTC `recorded_at_utc` dates: inclusive start day, inclusive end day represented as exclusive next midnight.
- Operation ID is always required; page size remains 1..50 and `sequence < before_sequence`.
- Allowed statuses remain `prepared`, `confirmed`, `unknown`, `failed`.
- Filtered export is a consistent read-only snapshot, ordered by sequence, max 100 matching events, all-or-nothing.
- Keep no-store, recursive sensitive evidence redaction, and unknown isolation. No provider/network writes or schema changes.
- Preserve unrelated dirty changes; no commits or worktrees.

## Task 1: Store and API

- [x] Add failing Store tests for date boundaries, status combination, cursor, invalid range, filtered export snapshot, and filtered-event export cap.
- [x] Add a validated Store filter model and apply it to bounded event reads.
- [x] Add filtered evidence export in one SQLite read transaction; keep existing unfiltered export unchanged.
- [x] Add strict date query parsing, duplicate handling, export route and API tests for errors, redaction, and no-store.
- [x] Run focused Store/downloader/API tests.

## Task 2: Event timeline UI

- [x] Add failing UI tests for UTC date filter application, cursor reset/retention, clearing, filtered export, stale response suppression, and failed-read controls retention.
- [x] Add UTC date controls and filtered evidence download to each operation's event panel.
- [x] Keep status/date state, pages, and request generations keyed by operation ID.
- [x] Run full WebUI/userscript suites.

## Task 3: Release and roadmap

- [x] Run full Go tests, vet, WebUI/userscript, and diff checks.
- [x] Package `.74`; verify all three binaries, version, ZIP manifest, checksums, and private-data exclusions.
- [x] Record package hash, scope, and external acceptance boundaries; update roadmap and version table.
