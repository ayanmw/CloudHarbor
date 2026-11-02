# Live Cloud Browse Duplicate Page Reanchor Implementation Plan

> **For agentic workers:** Execute inline in the primary checkout; preserve existing dirty files, do not use a worktree, and do not commit or push.

**Goal:** Let live cloud browsing recover to the first page when a nonzero offset request returns duplicate object identities.

**Architecture:** Reuse the existing page-zero reanchor response and `BoundaryChanged` UI path, but only when the provider returns `baiduapi.ErrDuplicatePageItem` for a nonzero offset. Do not retry other read failures or a duplicate on page zero, and do not issue a second reanchor when the duplicate error already caused one.

**Tech Stack:** Go, existing intake API, live cloud listing backend.

**Spec:** `docs/superpowers/specs/2026-11-01-cloud-page-boundary-auto-recheck.zh-CN.md`

## Global Constraints

- Only nonzero offset, duplicate-page errors may trigger a page-zero read.
- Normal pages add no provider request; fallback is read-only and visible through `boundary_changed`.
- Preserve expected-boundary validation and do not repeat the fallback twice.
- Do not create/use a worktree or commit/push.

## Review Focus

- Duplicate page on a later page reanchors exactly once and returns offset zero with the existing visible change signal.
- Duplicate on page zero does not retry because there is no prior page anchor to recover from.
- Network, authorization, and other read errors remain errors and do not trigger hidden requests.

---

### Task 1: Reanchor live listing after duplicate page identity

**Files:**
- Modify: `internal/intakeapi/server.go`
- Test: `internal/intakeapi/server_test.go`

**Interfaces:**
- Consumes: `baiduapi.ErrDuplicatePageItem`, `Backend.ListCloud`, and `downloader.CloudListing.BoundaryChanged`.
- Produces: A single page-zero fallback for duplicate identities on nonzero offsets.

- [x] **Step 1: Write RED API regressions** for duplicate-on-later-page reanchor plus no-retry cases for page-zero duplicate and unrelated provider errors.
- [x] **Step 2: Verify the failure.** Before production changes, the later-page duplicate returned HTTP 502 instead of the page-zero result.
- [x] **Step 3: Implement the bounded read fallback.** On nonzero offset only, match `ErrDuplicatePageItem`, read offset zero once, mark `BoundaryChanged`, and skip the normal expected-boundary recheck after that fallback.
- [x] **Step 4: Verify GREEN.** `go test ./internal/intakeapi -run '^TestCloudListing(ReanchorsWhenPageBoundaryMoved|ReanchorsAfterDuplicatePageIdentity|DoesNotReanchorFirstPageOrOtherErrors)$' -count=1` passed.
- [x] **Step 5: Run full intakeapi tests and vet; inspect final diff.** `go test ./internal/intakeapi -count=1`, `go vet ./internal/intakeapi ./internal/downloader ./internal/taskstore`, and `git diff --check` passed. This is item 2/5 in the shared pagination/cache candidate, paired with the scoped-refresh fix plan.
