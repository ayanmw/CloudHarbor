# Scoped Refresh Restart Reason Evidence

## Goal

Persist and display why an automatic scoped directory refresh restarted from page one, while preserving append-only history and keeping old events truthful.

## Decisions

- Record only fixed reason codes: `page_boundary_mismatch`, `provider_duplicate_page_id`, and `staged_page_duplicate_id`.
- Do not store raw provider errors, filenames, or inferred reasons for old events.
- Add the field to the existing refresh event row; preserve local `recorded_at_utc` and cloud-observation time semantics.
- Use additive SQLite schema migration v34→v35 and show a fixed Chinese label in refresh history.

## Verification

- A regression asserts the stored and API-read restart reason for boundary mismatch and both duplicate classes.
- Taskstore migration and history tests pass; downloader drift integration tests pass.
- Full taskstore/downloader package tests, relevant `go vet`, WebUI suite, and `git diff --check` remain release-batch gates.

## Status

- [x] Fixed-code event validation and persistence for scoped refresh and full scan events.
- [x] v34→v35 scoped-refresh migration and v35→v36 full-scan migration; fresh and upgrade schema regressions pass.
- [x] Downloader classifies the scoped refresh and full scan restart triggers.
- [x] Refresh/full-scan history APIs, exports, and pages expose the fixed reason.
- [x] Live listing API and page expose the fixed reason for the existing page-zero reanchor.
- [x] Full repository release verification passed; package/source snapshot integrity and working diff reviewed.

