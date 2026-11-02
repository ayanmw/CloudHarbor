# Full Scan History Implementation Plan

- [x] Write taskstore tests first for run summary retention, stable pagination, key event append-only behavior, and transaction rollback.
- [x] Add schema 29→30 run/event tables with UTC integer timestamps and append-only triggers; leave legacy data without fabricated history.
- [x] Integrate run/event writes with full-scan start, checkpoint status transitions, and finalization in the same SQLite transactions.
- [x] Add read-only API methods/routes and a paginated history/events panel to cloud management; ensure history reads use SQLite only.
- [x] Run taskstore/API/downloader/WebUI focused tests, then full Go tests/vet/WebUI/userscript/diff check. Record only local/mock evidence; do not package before the five-item batch gate.

**Verification:** `go test ./... -count=1`, `go vet ./...`, WebUI 118/118, userscript 3/3, focused taskstore/API/downloader tests, and `git diff --check` passed. No real Baidu API or NAS/SMB was used; no package, commit, or deployment was made.

No worktree, real account calls, release packaging, commit, or deployment.
