# Refresh Detail Workbench Implementation Plan

> **For agentic workers:** Implement in the primary checkout; do not use a worktree, commit, push, or deploy. Use local SQLite/mock for code and package gates. Any separate real-account acceptance must stay within the user's authorization and remain read-only; retain only the single authorized `.nas-downloader` copy in the latest release. Keep all five user-visible items in one preview batch.

**Goal:** Make directory refresh change details searchable, filterable, countable, exportable, and easy to copy for manual remote verification.

**Architecture:** Extend the existing account-scoped change-list query with event-local kind/search filters and stable paging. Compute unfiltered event totals and filtered totals from SQLite, add a bounded read-only export on a single snapshot, then wire controls into the existing on-demand detail panel.

**Design:** `docs/superpowers/specs/2026-10-30-refresh-detail-workbench.zh-CN.md`

## Five release items

- [x] Server-side kind filter for added/removed/metadata_changed/identity_changed.
- [x] Server-side search across old/new path, name and fs_id with a 128-code-point bound.
- [x] Unfiltered per-kind totals plus filtered hit count in the detail view.
- [x] Explicitly requested, filtered, single-directory JSON export; 5,000-row all-or-nothing limit.
- [x] Copy old/new path and fs_id controls with truthful clipboard error handling.

## Implementation sequence

1. Add Store tests for filter/search semantics, stable cursor and invariant totals; run RED.
2. Add API tests for parameter validation/privacy and JSON export bounds/snapshot behavior; run RED.
3. Add UI tests for conditions, reset/stale handling, visible counts, download and clipboard behavior; run RED.
4. Implement Store/Downloader/API changes without schema migration; run focused GREEN suites.
5. Implement the detail-panel controls and run focused WebUI suite.
6. Run full Go tests, vet, WebUI/userscript tests and diff checks.
7. Update the roadmap, set `.94` only after all five items pass, package preview.94, then verify three targets, ZIP, payload hashes, source snapshot and private data exclusion. **Completed:** all five items passed; the package contains all three targets, 13 ZIP entries, 12 verified payload hashes and no private runtime data.

## Constraints

- No filters may leak across account/run/event boundaries.
- Summary counts remain unfiltered; displayed rows and export follow active filters.
- Cursor is valid only under the exact active conditions; changing conditions restarts at page one.
- Export over 5,000 matching rows fails as a whole and returns no partial file.
- Do not change existing `.93` directory delta semantics or full-run export.
