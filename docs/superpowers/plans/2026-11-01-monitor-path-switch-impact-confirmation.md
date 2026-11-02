# Monitor Path Switch Impact Confirmation Implementation Plan

> **For agentic workers:** Implement inline in the primary checkout, following the user's explicit instruction. Do not create or use a Git worktree.

**Goal:** Show pending queue work that may fall outside a new monitoring directory and require confirmation against a fresh local snapshot before saving that directory.

**Architecture:** Add a read-only downloader preview that derives affected root jobs from the current persisted queue and returns a stable digest. Revalidate the digest while path maintenance is held; expose preview and conflict details through the existing authenticated path settings API; let the page show the consequences and request a second confirmation only when needed.

**Tech Stack:** Go, existing downloader scheduler and SQLite-backed queue projection, authenticated `net/http` settings API, embedded HTML/JavaScript, Node WebUI tests.

**Spec:** `docs/superpowers/specs/2026-11-01-monitor-path-switch-impact-confirmation.zh-CN.md`

## Global Constraints

- Preserve existing dirty and untracked work; do not create a worktree or commit.
- Do not mark tasks missing at path-save time; retain successful-scan evidence semantics.
- Never automatically retry a task, provider mutation, or download due to a path change.
- Keep active downloads, running scans, and unfinished intakes as path-change blockers.
- Package only as `v0.8.30-preview.117` after the five-item batch reaches its release threshold; until then keep this as an unnumbered candidate implementation.

## Review Focus

- A task exactly under the new root is not shown as falling out; a nested task is shown because only direct children are listed.
- `pending`, `auth_wait`, and `retry_wait` are included; active, done, cancelled, and already failed jobs are excluded.
- A changed task projection between preview and submit invalidates confirmation; no path or settings write occurs.
- HTTP clients cannot use a stale preview token to bypass reconfirmation after queue changes.
- Queue changes that do not affect this target do not invalidate the preview unnecessarily.

---

### Task 1: Derive and fingerprint affected queued jobs

**Files:**
- Modify: `internal/downloader/managed_paths.go`
- Modify: `internal/downloader/managed_paths_test.go`
- Modify: `internal/downloader/scheduler.go`
- Modify: `internal/downloader/actions_test.go`

**Interfaces:**
- Produce `ManagedPathImpact` with current/target queue paths, stable task summaries, count, and SHA-256 digest.
- Produce `PreviewManagedPathChange(targetQueueDir string) (ManagedPathImpact, error)`.

- [x] Write failing tests for status filtering, direct-child eligibility, stable ordering, and digest changes when an affected job changes.
- [x] Run the focused downloader test and verify it fails for the missing preview behavior.
- [x] Implement the pure snapshot derivation using queue-locked task projections and exact cloud path semantics.
- [x] Re-run the focused test to green.

### Task 2: Require fresh confirmation before changing an affected queue path

**Files:**
- Modify: `internal/downloader/managed_paths.go`
- Modify: `internal/downloader/managed_paths_test.go`

**Interfaces:**
- Add a confirmed path setter that accepts the preview digest; keep the existing setter as an empty-digest call for no-impact changes and internal compatibility.
- A typed stale-preview error carries the latest `ManagedPathImpact`.
- Task cancel/retry handlers fail fast while the path write gate is held, so they cannot change the confirmed snapshot between verification and settings persistence.

- [x] Write failing tests for no-confirmation rejection, successful exact confirmation, and stale-snapshot rejection without path/settings mutation.
- [x] Run focused tests and verify the expected failures.
- [x] Recompute the digest after acquiring path-operation and queue-maintenance gates; reject if it differs from the supplied digest.
- [x] Re-run focused tests, including persistence across downloader restart.
- [x] Verify cancel/retry cannot mutate the queue while managed paths hold the write gate; actions continue to work outside that gate.

### Task 3: Expose preview and stale-confirmation details through the path API

**Files:**
- Modify: `main.go`
- Modify: `main_test.go`

**Interfaces:**
- `GET /api/settings/paths` optionally accepts the target queue path and returns its impact preview.
- `POST /api/settings/paths` accepts the preview digest; stale confirmation returns HTTP 409 JSON with the new preview.

- [x] Add failing handler tests for preview output, missing digest rejection, and stale digest response.
- [x] Run focused main tests and verify failures.
- [x] Implement strict request decoding and map validation/stale errors to stable JSON without changing existing path response fields.
- [x] Re-run the focused tests.

### Task 4: Show affected jobs and require explicit web confirmation

**Files:**
- Modify: `internal/webui/index_cloud.html`
- Modify: `scripts/webui.test.cjs`

**Interfaces:**
- The path form refreshes impact when the queue path changes and before submitting.
- On stale confirmation, the page replaces the list with the returned latest preview and requires another explicit confirmation.

- [x] Add WebUI tests for listing affected jobs, no-confirmation cancellation, explanatory copy, and stale-preview reconfirmation.
- [x] Verify the confirmation test fails when the confirmation helper is disabled.
- [x] Implement preview rendering with escaped task values, deterministic order, accessible status text, and no save on cancel.
- [x] Re-run targeted WebUI tests; local download migration confirmations remain intact.

### Task 5: Page large queue impact previews

**Files:**
- Modify: `internal/downloader/managed_paths.go`
- Modify: `internal/downloader/managed_paths_test.go`
- Modify: `main.go`
- Modify: `main_test.go`
- Modify: `internal/webui/index_cloud.html`
- Modify: `scripts/webui.test.cjs`

- [x] Add a failing downloader test for stable pages and a shared full-snapshot digest.
- [x] Bound preview HTTP responses to 50 tasks and reject invalid pagination.
- [x] Add UI previous/next controls and preserve target/request ordering across page loads.
- [x] Verify stale confirmation still requires a second explicit confirmation.

### Task 6: Record the monitor path in source-missing scan evidence

**Files:**
- Modify: `internal/downloader/scheduler.go`
- Modify: `internal/downloader/task_events_test.go`

- [x] Add a failing event evidence assertion for the successful scan's monitor path.
- [x] Persist `monitored_queue_dir` with the existing scan epoch and provider observation time.
- [x] Verify the scan event does not fabricate remote operation timestamps.

### Task 7: Record candidate outcomes and run final release gates when batch is complete

**Files:**
- Modify: `docs/ai/cloud-management-roadmap.zh-CN.md`
- Modify: `docs/ai/release-versioning.zh-CN.md`
- Modify: `docs/ai/progress.md`

- [x] Record five independent outcomes in one `.117` batch.
- [x] Run `go test ./... -count=1`, `go vet ./...`, `node scripts/webui.test.cjs`, `node scripts/userscript.test.cjs`, and `git diff --check`.
- [x] Build/update `.117` using `scripts/package.ps1 -Version v0.8.30-preview.117 -AllowDirty -UpdatePreview`; inspect archive entries, payload hashes, source snapshot, and private-data exclusions.
- [x] Record package hash and exact verification boundary; do not claim real Baidu/NAS acceptance.

### Task 8: Preserve full-list offsets beyond the interactive paging bound

**Files:**
- Modify: `internal/baiduapi/client.go`
- Modify: `internal/baiduapi/client_test.go`

- [x] Add a regression for full-list overlap pages beyond the public interactive offset bound.
- [x] Keep public page validation bounded while allowing the internal all-pages reader to use large offsets.
- [x] Re-run full release gates and update the same `.117` package and validation record. The final gate, ZIP/source snapshot audit, and validation record are verified in `releases/v0.8.30-preview.117-validation.md`.
