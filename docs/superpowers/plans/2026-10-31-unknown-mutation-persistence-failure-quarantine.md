# Unknown Mutation Persistence Failure Quarantine Plan

> Inline execution in the primary checkout. No worktree, commit, push, live cloud mutation, or NAS/SMB test.

**Goal:** Keep an ambiguous cloud source quarantined when persisting `unknown` fails after the provider request.

## Tasks

### 1. Reproduce the current process gap

- [x] Inject an SQLite update trigger failure after a mocked ambiguous provider response and confirm the operation is not blocked in memory.

### 2. Quarantine immediately and recover durably

- [x] On unknown-state persistence failure, report status unknown, block source/object/target in memory, and best-effort invalidate the local cache.
- [x] Preserve existing startup recovery of the prepared journal row; after clearing the fault and reopening it becomes unknown with stale cache.
- [x] Verify no provider mutation replay and no task identity transition occurs.

### 3. Verify

- [x] Run focused downloader/taskstore tests, full serial Go tests/vet, WebUI/userscript suites, and `git diff --check`.

## Completion

Candidate item 4/5 for the unnumbered remote-operation recovery batch. Do not package until five distinct user outcomes are complete.
