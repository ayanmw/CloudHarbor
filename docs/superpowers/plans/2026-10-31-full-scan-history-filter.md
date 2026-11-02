# Full Scan History Filters Plan

- [x] Add taskstore tests for valid status and inclusive UTC start-date filters, invalid values/ranges, and stable sequence paging.
- [x] Add strict API query validation and pass supported filters to the SQLite read path.
- [x] Add UI status/date controls, clear behavior, first-page reset, and filter-preserving pagination.
- [x] Run focused taskstore/API/WebUI checks, full Go tests/vet, WebUI/userscript suites, and `git diff --check`.

**Verification:** Full `go test ./... -count=1`, `go vet ./...`, WebUI 120/120, userscript 3/3, focused taskstore/API/WebUI checks, and `git diff --check` passed. Cross-platform package builds remain in the preview gate. No live Baidu or NAS/SMB acceptance was performed.
