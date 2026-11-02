# Full Scan History Evidence Export Plan

- [x] Add a taskstore regression proving summary and chronological events are exported from one SQLite read-only snapshot; reject evidence over 10,000 events in full.
- [x] Add downloader/API export wiring with strict run ID validation, no-store attachment headers, and a 413 response for oversized evidence.
- [x] Add a browser download control that runs only after explicit user selection and click.
- [x] Ensure the exported DTO excludes internal account fingerprints; add an API regression using a sentinel fingerprint and observe the old output fail before the fix.
- [x] Run focused taskstore/API/WebUI tests, full Go tests/vet, WebUI/userscript suites, and `git diff --check`.

**Verification:** Full `go test ./... -count=1`, `go vet ./...`, WebUI 120/120, userscript 3/3, focused taskstore/API/WebUI checks, and `git diff --check` passed. Release cross-builds and package inspection remain part of the preview gate. No real Baidu mutation or NAS/SMB acceptance was performed.
