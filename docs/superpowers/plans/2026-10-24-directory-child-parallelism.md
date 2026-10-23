# Directory Child Parallelism Implementation Plan

> **For agentic workers:** Execute inline with `superpowers:executing-plans`; preserve existing folder completion and root source-operation rules.

**Goal:** Use configured parallelism for file children inside a queued directory without exceeding the downloader-wide transfer cap.

**Architecture:** Add a shared file-transfer semaphore sized from effective `MaxParallel`. For each listed directory, process its direct files with a fixed-size worker pool capped by the same limit; recurse into child directories sequentially. Keep root scheduler workers unchanged, and wait for in-flight child transfers before returning.

**Tech Stack:** Go 1.22, standard-library channels/WaitGroup, existing HTTP and local disk fixtures.

**Spec:** `docs/superpowers/specs/2026-10-24-directory-child-parallelism-design.md`.

## Global Constraints

- Never have more than effective `MaxParallel` active file transfers across all roots in one Downloader.
- `MaxParallel=1` remains sequential and retains the current source-operation behavior.
- The root task is marked complete and post-processed only after all descendant files succeed.
- Preserve retry/resume behavior, task visibility and failure aggregation.
- No changes to API, JSON format, credentials, intake uncertainty rules, or cloud source operation semantics.

## Review Focus

- One large flat directory uses multiple transfers when `MaxParallel > 1`; test with a blocking local HTTP handler.
- Multiple roots share one global cap; test two simultaneous directory walks and assert peak active transfers never exceeds the configured value.
- A regular child failure still allows sibling files to finish but fails the parent; test source operation is not sent.
- Authentication failure stops enqueueing new siblings; test call count remains bounded after the first rejection.
- Cancellation stops enqueueing and waits for in-flight transfers; test no root post-processing occurs.
- `MaxParallel=1` stays sequential; cover the exact compatibility mode.

### Task 1: Bounded directory file transfers

**Files:**
- Modify: `internal/downloader/downloader.go`
- Test: `internal/downloader/downloader_test.go`
- Modify: `release/TESTING.md`, `docs/ai/experience-plan.md`, `docs/ai/progress.md`

**Interfaces:**
- Add a Downloader-owned transfer semaphore initialized from effective MaxParallel (zero in direct unit fixtures retains the scheduler's existing default of 2).
- Keep `downloadLocalFolder` and `downloadLocalFile` signatures unchanged.
- Add no public config fields or API methods.

- [x] Add a failing test proving two files in one directory start concurrently when MaxParallel=2.
- [x] Add a failing test proving simultaneous directory roots share the same MaxParallel transfer cap.
- [x] Add failure aggregation, auth fail-fast, cancellation, and MaxParallel=1 regressions.
- [x] Implement the shared transfer semaphore and bounded per-directory direct-file worker pool, keeping recursive child-directory calls sequential.
- [x] Run focused downloader tests and a fixed-latency benchmark. All-package race/vet checks remain before release.
- [ ] Review diff and docs; package an immutable preview only if the concurrency bound and root semantics hold and the latency benchmark improves.
