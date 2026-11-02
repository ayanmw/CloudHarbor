# Large File Duplicate Hash Resume Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make large duplicate-content scans show in-file progress and safely resume streaming SHA-256 work after transient failure, pause, or process restart.

**Architecture:** Persist a bounded `crypto/sha256` binary state together with current candidate identity and byte offset in the duplicate scan row. Resume only after current provider identity matches and `DownloadFileWithRange` validates the returned suffix; complete the verified hash, scan counters and checkpoint removal in one SQLite transaction. Expose only safe progress metadata through the existing scan status API.

**Tech Stack:** Go, `crypto/sha256` binary hash state, Baidu Range download client, SQLite schema migration, Node WebUI tests.

**Spec:** `docs/superpowers/specs/2026-10-31-large-file-duplicate-hash-resume.zh-CN.md`

## Global Constraints

- Keep the primary checkout; do not create a worktree, commit, push, or perform a cloud mutation.
- New durable state belongs in SQLite schema 24 and must survive close/reopen.
- Checkpoint after each 16 MiB and immediately on pause or a returned read error.
- Never attach a partial or stale digest to `cloud_content_hashes`.
- Preserve provider Retry-After, existing rate limits, UTC retry deadlines, and account isolation.

## Review Focus

- Stale/replaced inventory identity: current provider metadata and cache identity must match the checkpoint before any suffix is accepted.
- Ignored or malformed Range: restart the hash from zero only for `ErrRangeIgnored`; malformed 206/Content-Range fails closed.
- Process stop between data read and SQLite commit: resume from the last durable offset/hash pair, never combine mismatched offset and digest state.
- Corrupt or unsupported SHA state: discard the active checkpoint and safely recompute the candidate from byte zero.
- Retry-After on download content errors: retain the parsed bounded delay without exposing URLs, response bodies or tokens.

---

### Task 1: Persist active candidate identity and SHA checkpoint

**Files:**
- Modify: `internal/taskstore/cloud_duplicates.go`
- Modify: `internal/taskstore/sqlite.go`
- Modify: `internal/taskstore/migration.go`
- Modify: `internal/taskstore/store.go`
- Test: `internal/taskstore/cloud_duplicates_test.go`
- Test: `internal/taskstore/migration_test.go`

**Interfaces:**
- Extend `CloudDuplicateScanState` with current file identity, active bytes and an unexported-from-JSON `[]byte` SHA state field.
- Add atomic `CompleteCloudDuplicateCandidate(account string, state CloudDuplicateScanState, item CloudInventoryEntry, digest string, verified time.Time) error`.

- [x] Add RED tests for active state Store close/reopen and a completion transaction that advances counters, writes verified hash and clears active state.
- [x] Add RED migration test from schema 23 preserving queue, events, inventory, and verified hashes while adding active checkpoint columns.
- [x] Implement schema 24 creation/migration and state serialization; keep SHA binary bytes excluded from JSON/evidence.
- [x] Implement `CompleteCloudDuplicateCandidate` with conditional current identity/complete-directory checks and atomic hash/state update.
- [x] Run taskstore duplicate and migration tests.

### Task 2: Preserve Retry-After for download stream HTTP errors

**Files:**
- Modify: `internal/baiduapi/client.go`
- Test: `internal/baiduapi/client_test.go`

- [x] Add RED tests for download HTTP 429 with seconds and HTTP-date `Retry-After` values; ensure existing Range contract tests still reject malformed responses.
- [x] Populate `APIError.RetryAfter` from download response headers using the shared parser.
- [x] Run focused Baidu API download and Retry-After tests.

### Task 3: Resume duplicate hashing from durable byte/hash checkpoints

**Files:**
- Modify: `internal/downloader/cloud_duplicate_scan.go`
- Modify: `internal/downloader/cloud_duplicate_retry_test.go`
- Modify: `internal/downloader/downloader_test.go`

**Interfaces:**
- Implement `hashCloudFileResumable(ctx context.Context, client *baiduapi.Client, item taskstore.CloudInventoryEntry, state taskstore.CloudDuplicateScanState, checkpoint func(int64, []byte) error) (string, int64, []byte, error)`.
- Persist byte offset and serialized SHA state every `duplicateHashCheckpointInterval = 16 << 20`, plus the last read offset on pause/read error.

- [x] Add RED test using an HTTP fixture that interrupts after a persisted offset and verifies the retry sends the expected Range and computes the same digest as a full read.
- [x] Verify the persisted SHA state separately survives Store close/reopen; downloader Range resume is exercised in the same local HTTP fixture.
- [x] Cover Range rejection through the existing Range contract tests; existing identity checks reject provider metadata changes, and invalid SHA state resets to zero before requesting content.
- [x] Implement streaming checkpoint/resume; on `ErrRangeIgnored`, reset hash state once and redo from zero.
- [x] Integrate worker progress persistence and final `CompleteCloudDuplicateCandidate` transaction; preserve active checkpoint during retry backoff.
- [x] Run duplicate scan retry, identity and taskstore suites.

### Task 4: Display current file and in-file progress

**Files:**
- Modify: `internal/webui/index_cloud.html`
- Test: `scripts/webui.test.cjs`

- [x] Add a RED test for display of active path, file bytes, completed bytes and total bytes; path is rendered through `textContent` so HTML-like text is inert.
- [x] Render active file progress from the existing duplicate scan status response, keeping retry and completed progress visible.
- [x] Run WebUI tests (83/83).

### Task 5: Candidate route, regression gate and release

**Files:**
- Modify: `docs/ai/release-versioning.zh-CN.md`
- Modify: `docs/ai/cloud-management-roadmap.zh-CN.md`
- Modify: `docs/ai/progress.md`
- Modify: `release/TESTING.md`
- External artifact: `D:\projects\Git-Projects\nas_baidupan_auto_downloader-releases\v0.8.30-preview.99-validation.md`

- [x] Run focused RED/GREEN tests, full `go test -count=1 ./...`, `go vet ./...`, WebUI (83), userscript (3), and `git diff --check`.
- [x] Update `.99` package with `scripts/package.ps1 -Version v0.8.30-preview.99 -UpdatePreview -AllowDirty`; verify all three builds, ZIP/manifest hashes, source snapshot and no private runtime data.
- [x] Stop only the exact `.98` / `.99` process after confirming health, account binding and zero tasks/unknown; move the single authorized `.nas-downloader` to `.99`, restart and perform read-only health/cache/root/monitor checks.
- [x] Write final package evidence and explicit local-vs-Baidu-vs-NAS verification limits.
- [x] Do not run a large real-account hash scan, delete duplicates, mutate remote files, or perform NAS/SMB claims.
