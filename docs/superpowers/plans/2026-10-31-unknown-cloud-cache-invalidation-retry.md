# Unknown Cloud Cache Invalidation Retry Plan

> Inline execution in the primary checkout. No worktree, commit, push, live cloud mutation, or NAS/SMB test.

**Goal:** Ensure unknown remote operations cannot leave an unmarked complete cache after an SQLite invalidation failure.

## Tasks

### 1. Reproduce failure to retry unknown cache invalidation

- [x] Seed complete cache and unknown operation without the cache recovery marker; confirm current behavior left coverage complete before implementing recovery.
- [x] Add a recovery rollback test with an injected SQLite trigger and verify the marker is absent until retry.

### 2. Retry and mark unknown cache recovery

- [x] Let startup recovery find unknown operations missing `local_cache_recovery_complete`; transactionally stale affected paths, append UTC evidence, and write the marker.
- [x] For current unknown mutation results, write the same marker after cache invalidation succeeds; persist a diagnostic when invalidation or marker recording fails.
- [x] Keep operation status unknown and preserve account isolation/cache rows/hashes.

### 3. Verify

- [x] Run focused taskstore/downloader tests, full serial Go tests/vet, WebUI/userscript suites, and `git diff --check`.

## Completion

Candidate item 3/5 for the unnumbered remote-operation recovery batch. Do not package until five distinct user outcomes are complete.
