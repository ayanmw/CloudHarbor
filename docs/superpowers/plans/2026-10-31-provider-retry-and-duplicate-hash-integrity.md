# Provider Retry and Duplicate Hash Integrity Plan

**Goal:** Complete five independently verifiable improvements for Baidu read-only request backoff, inventory retry timing, duplicate content integrity, account verification pacing and retry diagnostics; deliver them together as preview `.98` only after the complete release gate passes.

**Spec:** `docs/superpowers/specs/2026-10-31-provider-retry-and-duplicate-hash-integrity.zh-CN.md`

**Constraints:** Use the existing primary checkout and preserve its unrelated dirty changes. Do not use a worktree, commit, push, or perform remote write operations. Keep the `.97` account data directory in place. Real-account checks are read-only only.

**Release rule:** Exactly five independent user outcomes are in scope. If an outcome is incomplete or any release gate fails, keep iterating `.97` source work without creating `.98`; once all five and the full gate pass, package `.98` and keep future work in an unnumbered batch until it has five items.

## Files and responsibilities

- `internal/downloader/cloud_inventory_scan.go`: one reusable UTC retry deadline wait loop, Retry-After selection for duplicate scan, and scan wait cancellation.
- `internal/downloader/cloud_inventory_scan_test.go`, `internal/downloader/cloud_duplicate_retry_test.go`: clock deadline and retry persistence regressions.
- `internal/downloader/cloud_duplicate_scan.go`, `internal/downloader/cloud_management.go`, their existing tests: provider MD5 evidence checks around duplicate hashing and existing delete verification.
- `internal/taskstore/cloud_duplicates.go`, `internal/taskstore/cloud_duplicates_test.go`: transaction that replaces only a stale cached MD5 and removes its invalid SHA-256; hash save precondition against a concurrently changed cached MD5.
- `internal/credentials/manager.go`, `internal/credentials/manager_test.go`: optional shared RequestPacer injection into the default BaiduProvider account verification.
- `internal/downloader/auth.go`, `internal/downloader/downloader.go`, `main.go`: expose and wire the Downloader-owned pacer into default credential verification.
- `internal/intakeapi/server.go`, `internal/intakeapi/server_test.go`: read-only error-body wait hint and `Retry-After` response header.
- `docs/ai/release-versioning.zh-CN.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/progress.md`, `release/TESTING.md`: candidate scope and evidence.
- External release directory: `.98` ZIP, extracted preview and `.98-validation.md`; preserve `.97` and its runtime data.

## Task 1 — Duplicate scan uses provider Retry-After

- [x] Add a worker regression with a retryable provider 429 and persisted deadline; prove state retains the actual delay, UTC deadline and failure cause.
- [x] Run the focused test and confirm it fails because duplicate scan currently saves only the 5-second local retry delay.
- [x] Use `max(local backoff, APIError.RetryAfter)`, cap at one hour, and wait on the persisted deadline. The production API parser already caps provider values; helper also caps synthetic values.
- [x] Run the duplicate scan retry tests; expected duplicate completion and all transition assertions pass.

**Expected:** Focused RED identifies the local-only delay; GREEN shows no early API request and preserved retry evidence after SQLite reopen.

## Task 2 — UTC deadline waits respond to wall-clock changes

- [x] Add deterministic tests for the retry wait policy: far deadlines are capped at 30 seconds, elapsed deadlines return immediately, and backward adjustment uses the current remaining interval.
- [x] Route persisted and in-run retry waits for full inventory, scoped refresh and duplicate hashing through the shared UTC recheck loop.
- [x] The pre-change helper had no bounded recheck; the new focused test confirms the wall-clock calculation policy.
- [x] Keep UTC deadline persistence and retry budgets unchanged; run focused inventory and duplicate retry tests, including cancellation and no-early-request recovery.

**Expected:** Far deadlines produce a maximum 30-second timer step; advancing the test clock causes the next calculation to become due; context cancellation returns without waiting for the deadline.

## Task 3 — Duplicate hashes follow live provider MD5

- [x] Add a downloader regression proving equal fs_id/path/size/mtime but different nonempty provider MD5 is detected and cannot leave a usable old duplicate hash.
- [x] Add taskstore regressions proving a live MD5 refresh with stable identity updates the cached provider value and UTC observation while atomically deleting its prior SHA-256, and stale concurrent candidates cannot reattach it.
- [x] Confirm the old identity check ignored provider MD5; then implement case-insensitive MD5 comparison. This precise mismatch rebases only the MD5 evidence and stops the current scan; other identity mismatches still fail closed.
- [x] Run focused duplicate, taskstore duplicate and inventory regressions. Full release gate remains pending.

**Expected:** Stale MD5 never remains attached to a duplicate group; provider MD5 wins in local cache with local observation time recorded; path/size/mtime conflicts are not silently rebased.

## Task 4 — Credential account verification shares the Downloader pacer

- [x] Add a test invoking `BaiduProvider.Account` with a cancellation-aware pacer and prove `UserID` waits through it before network I/O.
- [x] Inject the existing Downloader pacer into the default `BaiduProvider` from `main`; custom Provider implementations are untouched.
- [x] Keep the caller context on the request so cancellation stops at the pacer before network I/O.
- [x] Run focused credentials, Downloader, and Baidu API tests.

**Expected:** Exactly one pacer wait for one default account verification; a pre-canceled context stops before network I/O; injected providers preserve existing behavior.

## Task 5 — Read-only errors expose Retry-After safely

- [x] Extend the read-only cloud API regression for a structured 429 with retry advice; assert unchanged public status, sanitized text and the response header/body hint.
- [x] Add a `Retry-After` header and localized body hint only for positive structured retry durations. Keep provider bodies, URLs, tokens and write-operation errors out of this branch.
- [x] Run intake API tests; WebUI regression confirms the safe server diagnostic renders without automatic retry.

**Expected:** The HTTP status stays 502 and text adds a safe `N` second hint only when a parsed provider delay exists; remote mutation errors remain unchanged.

## Final verification and package

- [x] Run `git diff --check` and inspect the task diff. Preserve pre-existing unrelated modified/untracked paths; no cleanup or worktree used.
- [x] Run `go test -count=1 ./...`, `go vet ./...`, WebUI tests (83/83), and userscript tests (3/3).
- [x] Verify `.97` path and healthy read-only runtime; task, downloading, and unknown-source counts were zero before stopping its exact executable.
- [x] Package `v0.8.30-preview.98`; verify version output, all three architecture builds, 13 ZIP entries, 12/12 SHA-256 entries, 346 source files individually against the snapshot, no private payload, and no staging directory.
- [x] Preserve the single authorized account directory and launch `.98`; verify schema 23/integrity ok, health/OAuth/account binding, zero tasks/downloads/source_unknown, live root and monitor reads, complete inventory, and executable path.
- [x] Record the final package hash, counts, tests, runtime checks, and limits in the external `.98-validation.md`.
- [x] No cloud mutation, duplicate cleanup, download, real rate-limit injection, NAS/SMB acceptance, Git commit or push.

**Expected:** Preview `.98` is present only if all five scoped outcomes and every listed gate are verified. Release validation explicitly says local/mock only for rate-limit behavior and no NAS/SMB acceptance.
