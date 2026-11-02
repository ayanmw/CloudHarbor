# Confirmed Cloud Operation Restart Recovery Plan

> Inline execution in the primary checkout. No worktree, commit, push, live cloud mutation, or NAS/SMB test.

**Goal:** Detect a confirmed remote mutation whose local reconciliation did not finish before shutdown and safely invalidate its cache on restart.

**Design:** Append an immutable local completion event only after cache handling, queued identity handling, and monitor queue reconciliation succeed. On reopen, transactionally invalidate paths for confirmed operations without either marker, append path/time evidence, then append a distinct cache-recovery marker so the fallback runs once. The recovery marker does not claim the local task queue has been reconciled; the normal authorized monitor scan handles that. Preserve cached rows and hashes; never repeat provider mutations.

## Tasks

### 1. Reproduce interrupted confirmed operation

- [x] Seed complete directory coverage, create a confirmed operation without a local-complete event, reopen, and confirm old behavior incorrectly leaves coverage complete.
- [x] Seed a recovery and establish that its completion marker makes subsequent opens idempotent.

### 2. Add durable completion and recovery evidence

- [x] Add an idempotent taskstore method to append `local_reconciliation_complete` only for confirmed operations.
- [x] Extend startup recovery to find confirmed operations lacking either marker, atomically stale source/target paths, append recovery evidence and a distinct cache-recovery marker, and advance account generation.
- [x] Mark the operation locally reconciled at the end of successful `MutateCloud` local synchronization, including a safely stale cache outcome.
- [x] Verify cached rows/hashes remain under the shared stale-cache path and that recovered operations are not invalidated again.

### 3. Verify failure behavior

- [x] Inject SQLite failure and verify recovery changes, evidence, generation and marker roll back together; retry and verify convergence.
- [x] Run focused taskstore/downloader tests, full Go tests/vet, WebUI/userscript suites, and `git diff --check`.

## Completion

This is candidate item 2/5 of the unnumbered remote-operation recovery batch. Do not package before five distinct user outcomes are complete.
