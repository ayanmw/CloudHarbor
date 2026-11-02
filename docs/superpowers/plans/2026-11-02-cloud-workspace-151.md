# .151 Cloud Workspace Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans in the primary checkout. Implement inline; one fresh-context review at the end. Do not create a worktree.

**Goal:** Deliver the five cohesive cloud-management improvements in one tested preview.

**Architecture:** Keep the existing embedded cloud page and APIs. Isolate new browser interactions in focused scripts. Preserve existing pagination, cache generations, remote identity checks and unknown-operation isolation.

**Tech Stack:** Go, SQLite, browser JavaScript, existing shared CSS, Node/JSDOM tests.

**Spec:** ../specs/2026-11-02-cloud-workspace-151.zh-CN.md

## Global Constraints

- Five user-facing outcomes in one preview; .150 stays the current deployed release until .151 gates pass.
- No additional real authorization copy, worktree, push, real-account writes or stress scans.
- No new unverified provider frequency policy; shared 250ms pacing and error backoff remain.
- Persist past evidence separately from new observations. Legacy request_budget=0 stays compatible.

## Tasks

- [x] 1. Browsing history and refresh restoration.
  - Add res/cloud_browse_route.js for strict browse-parameter parsing and URL updates.
  - Wire initial restoration, popstate, explicit navigation and internal replace-only recovery in index_cloud.html; retain ui_tab.
  - Extend complete-page JSDOM and legacy VM loading to cover actual route integration, Unicode/special names, invalid parameters, cache searches, paging, back/forward and read-only restoration.
- [x] 2. File and directory properties drawer.
  - Add a focused properties script/dialog using current list snapshots, observed UTC and provider metadata; no additional provider read just to open it.
  - Disable stale actions after navigation/refresh, escape names and support keyboard close/copy path feedback.
- [x] 3. Friendly mkdir/rename/delete dialogs.
  - Keep expected identity and remote operation API unchanged; retain input on definitive validation failures.
  - Distinguish uncertain transport/provider results from safe local validation failures, block blind resubmit and link operation history.
  - Verify actual submit/cancel/error flows with mocked provider and no real cloud writes.
- [x] 4. Minimisable background progress panel.
  - Reuse existing full/scoped status polling outputs. Display measured counters and phases, never invented completion percentages.
  - Keep panel usable across cloud tabs; minimising only changes presentation. Pause/continue use current explicit handlers.
- [x] 5. Explicit scan request-limit modes.
  - Add optional validated limit mode without changing legacy request_budget=0 behavior.
  - Apply explicit finite/unlimited transitions and resume evidence atomically; preserve RequestsUsed and restart semantics.
  - Default UI to scan-to-completion, keep numeric limits in advanced controls and expose finite-to-unlimited explicitly.
- [x] 6. Review, release and verification.
  - Review the whole change with fresh context, resolve findings, run complete gates once final source stabilises.
  - Produce .151 only after all five outcomes pass; ZIP/private-state checks and Chinese validation record.
  - Verify read-only desktop/mobile UI, clean temporary test services and restore temporary viewport settings.
  - Update roadmap with accurate source/package/runtime evidence; plan the next cohesive batch only after this batch completes.

## Execution ledger

- 2026-11-02: .150 completion confirmed from source and latest validation. .151 planned at five outcomes; starting browsing history. NAS/SMB and real remote-write acceptance remain external, not blockers to local implementation.

- Implementation: all five outcomes are connected. Frontend complete-page tests 17/17 and legacy WebUI 172/172 pass; downloader scan-family tests 41.726s, intakeapi full package, new SQLite atomic-cap rollback/reopen test and vet pass. No new real-account operations.
- Review: one P2 found and fixed: limit choices were page-global instead of checkpoint-specific. Added runID-bound intent, unedited same-run cap synchronisation, and scan/refresh regressions. Independent reviewer reran the three mode tests and closed the finding.
- Release gate pending: final full-repository tests, three builds, synthetic browser visual QA, package hashes/private-state check. Keep current .150 runtime/state untouched.

- First complete release gate: full Go test suite passed (downloader 213.479s, taskstore 134.833s), vet, frontend 17/17, legacy 172/172, userscript 3/3 and three-platform builds. Candidate 12816f1 produced a clean-source package.
- Native browser QA used an explicitly labelled synthetic-data fixture, no Baidu calls: path refresh/back, properties, rename form, minimise/tab switch, pause and finite-mode chooser. Fixed reset-induced top-left modal placement and negative mobile overlay margins; measured mobile properties/progress x=12..363 in a 375px client viewport. Restored viewport and closed the test tab/service.
- Poll focus regression fixed and independently reviewed: identical polls retain nodes, changing counters retain equivalent keyboard focus. Targeted frontend and legacy suites passed again.
- A separate no-credentials executable startup command was automatically rejected (blocked by policy); it created neither process nor state directory. Replaced available coverage with in-process embedded HTTP module/MIME checks and main authentication-route coverage. Independent packaged executable startup remains an explicit validation boundary, not a claimed result.
- Final repack uses the UI fixes above; source and final ZIP/hash records are in the releases sibling validation file. Real .150 service and unique account state stay unchanged.
