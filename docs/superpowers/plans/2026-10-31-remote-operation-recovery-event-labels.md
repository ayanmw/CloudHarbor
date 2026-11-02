# Remote Operation Recovery Event Labels Plan

> Inline execution in the primary checkout. No worktree, commit, push, live cloud mutation, or NAS/SMB test.

**Goal:** Explain local recovery events in the remote operation history without hiding raw audit evidence.

## Tasks

### 1. Reproduce raw-only evidence display

- [x] Load representative cache-recovery and unknown-persistence events and confirm the history page shows only JSON.

### 2. Add semantic local event labels

- [x] Add Chinese summaries for recovery, cache failure and local reconciliation event types.
- [x] Show affected cache paths where present while retaining UTC times and escaped raw JSON.
- [x] Leave unrecognized events visible as raw evidence.

### 3. Verify

- [x] Run focused WebUI tests, full WebUI/userscript suites, full serial Go tests/vet, and `git diff --check`.

## Completion

Candidate item 5/5 for the unnumbered remote-operation recovery batch. Packaging still requires all five items to pass verification.
