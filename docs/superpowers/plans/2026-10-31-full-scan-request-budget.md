# Full Scan Request Budget Implementation Plan

- [x] Add failing SQLite tests for durable full-scan request reservations, no over-budget reservation, and close/reopen persistence.
- [x] Add `request_budget`/`requests_used` to full-scan checkpoint state and schema 28→29 migration; persist a reservation before each provider list GET.
- [x] Add API budget validation and fresh-run/continuation behavior. Exhaustion pauses with the existing cursor and queue intact; continuation adds quota.
- [x] Add full-scan page controls and status usage to the WebUI, with mock-backed UI coverage.
- [x] Run focused tests, full Go tests, vet, WebUI/userscript checks, and `git diff --check`; record local/mock acceptance boundaries. Do not package until five independent outcomes are complete.

**Verification:** `go test ./... -count=1`, `go vet ./...`, `node --test scripts/webui.test.cjs` (117/117), `node --test scripts/userscript.test.cjs` (3/3), targeted taskstore/downloader/API tests, and `git diff --check` passed. All Baidu listing was mocked; no real account, NAS/SMB, packaging, commit, or deployment was used.

No worktree, real account calls, release packaging, commit, or deployment.
