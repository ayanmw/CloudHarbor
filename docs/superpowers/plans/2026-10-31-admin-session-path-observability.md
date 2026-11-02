# Admin Session and Path Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans. Steps use checkbox syntax for tracking.

**Goal:** Complete the five reviewed admin-login and path-maintenance fixes as one testable preview batch.

**Architecture:** Keep the current HTTP and SQLite contracts. Fix path normalization at both browser and downloader boundaries, keep login failure throttling process-local but reclaim expired entries before checking capacity, and make UI state/error reporting explicit without retrying user mutations.

**Tech Stack:** Go, SQLite taskstore, embedded HTML/JavaScript, Node `node:test`, PowerShell preview package script.

**Spec:** `docs/superpowers/specs/2026-10-31-admin-session-and-path-observability.zh-CN.md`

## Global Constraints

- Do not create or use a Git worktree.
- Preserve all pre-existing dirty and untracked files; do not stage, commit, push, or clean unrelated data.
- Keep admin sessions persistent until explicit logout; do not trust forwarded IP headers.
- Do not automatically retry or replay remote mutations.
- Deliver no preview until at least five independent results and all package gates pass.

## Review Focus

- Cloud directory names with leading/trailing spaces must survive the form and server normalization exactly.
- Expired login throttling entries must be removed without deleting active lockouts.
- A 401 response must be understandable in the browser while preserving the no-replay rule.
- A transient status API error must not erase or mislabel previously known state as absent.
- Slow migration status responses must not create concurrent polling requests or let stale responses overwrite current state.

---

### Task 1: Preserve managed path input

**Files:** `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`.

- [x] Write a failing Web UI regression proving the posted cloud queue value is not silently trimmed.
- [x] Run the focused Node test and confirm the queue path is changed only because current UI calls `.trim()`.
- [x] Submit the original non-empty queue path value without trimming it; keep the existing local-path normalization.
- [x] Re-run the focused Node test and confirm it passes.

### Task 2: Reclaim stale login-throttle entries

**Files:** `internal/management/auth.go`, `internal/management/auth_test.go`.

- [x] Add `TestExpiredLoginFailuresDoNotPermanentlyFillRateLimitMap`: seed 1024 expired entries, then assert a new address is admitted and stale entries are removed.
- [x] Run the focused test and confirm it fails with the existing “too many login attempts” condition.
- [x] Prune all entries outside the 15-minute window under `loginMu` before capacity evaluation.
- [x] Re-run the focused test and existing `TestLoginRequiresSameOriginAndBoundsFailedAttempts`.

### Task 3: Explain expired admin login in cloud management

**Files:** `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`.

- [x] Add `cloud management redirects expired admin sessions to login with a return path` with a mocked 401 from the page API helper.
- [x] Run the focused Node test and confirm the raw middleware response is shown today.
- [x] Map cloud-management 401 responses to the localized re-login instruction, including the direct download actions in this page; do not retry or replay the request.
- [x] Re-run the focused Node test.

### Task 4: Distinguish unavailable scan state from absent history

**Files:** `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`.

- [x] Add regressions for scan and directory-refresh status endpoints returning 503; assert neither summary claims that there is no record.
- [x] Run the focused Node tests and confirm both currently render the no-record label.
- [x] Render a read-error state separately from a confirmed empty state while retaining the detailed error and last successful state evidence.
- [x] Re-run the focused tests and related cloud-status UI tests.

### Task 5: Prevent overlapping local migration polls

**Files:** `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`.

- [x] Add `migration status polling never overlaps requests`: keep the first status request pending, call the poller again, and assert only one request is active.
- [x] Run the focused Node test and confirm the current `setInterval` poller issues two requests.
- [x] Guard the migration status request against overlap and ensure completion/failure clears the in-flight state so later polling can proceed.
- [x] Re-run the focused test and active/completed migration UI tests.

### Task 6: Full verification and preview release

**Files:** update the three AI roadmap/progress documents and this plan; create validation record next to release artifacts.

- [x] Run `go test -count=1 ./...`, `go vet ./...`, `node scripts/webui.test.cjs`, `node scripts/userscript.test.cjs`, and `git diff --check`.
- [x] Run `scripts/package.ps1 -Version v0.8.30-preview.102 -AllowDirty` only after all tests pass; inspect ZIP payload, hashes, source snapshot and private-data exclusion.
- [x] Record test results, package SHA-256, source snapshot and unverified external boundaries; update the roadmap and progress docs.
- [x] Do not commit or push.
