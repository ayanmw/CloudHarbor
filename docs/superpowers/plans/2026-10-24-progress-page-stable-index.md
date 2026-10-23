# Progress Page Stable Index Implementation Plan

> **For agentic workers:** Execute inline with `superpowers:executing-plans`; preserve the current API and order exactly.

**Goal:** Avoid rescanning and heap-sorting the full task history on every progress-sorted page request.

**Architecture:** Cache IDs for non-downloading tasks in progress order, keyed by the existing task-stat generation because durable status and catalog changes invalidate it. Recompute the bounded set of currently downloading tasks from live progress on each request, sort that set, and merge it with the cached static IDs before selecting the requested window.

**Tech Stack:** Go 1.22, existing scheduler locks/generations, task-page tests and benchmarks.

**Spec:** `docs/ai/experience-plan.md`, section D5.

## Global Constraints

- Preserve progress ratio, tie-breaking by FsID, page offsets, totals, and global status counts.
- Keep active task progress live on every request; progress ticks must not invalidate/rebuild the static history index.
- Invalidate the cached order on any status, membership, or file metadata change that can affect a task's rank.
- Cache only FsIDs, not copied Task or FileInfo values.
- Do not change storage formats, APIs, or browser behavior.

## Review Focus

- Equal progress values retain ascending FsID order; test full-sort equivalence across offsets.
- Active task progress changes reorder the page without invalidating the static index; test two progress ticks.
- A static task changing from done to failed moves between progress groups after status persistence; test cache invalidation.
- Non-root task status transitions and membership changes must not leave stale IDs; retain generation and catalog tests.
- Offset near or beyond the end returns the same HasMore and page contents as the full sort; test boundary offsets.

---

### Task 1: Cache stable progress ordering and merge active tasks

**Files:**
- Modify: `internal/downloader/scheduler.go`
- Modify: `internal/downloader/downloader.go`
- Test/benchmark: `internal/downloader/task_page_test.go`
- Modify: `docs/ai/experience-plan.md`, `docs/ai/progress.md`

**Interfaces:**
- `GetTaskPage` signature and `TaskPage` response remain unchanged.
- Add a scheduler progress-ID index keyed by `taskStatsGeneration`.
- Add private helpers that construct a candidate for an FsID and return the stable progress-order IDs while queue and task locks are held.

- [x] Add a regression that first builds the stable index, changes live progress, and asserts the requested page reflects the latest dynamic order without rebuilding the stable index.
- [x] Add a regression that changes an inactive persisted task status and verifies the progress grouping and cached generation update.
- [x] Run new regressions to observe expected failures before implementation.
- [x] Implement lazy stable-ID sorting and merge the currently downloading candidates using the existing comparator semantics; use precomputed compact sort keys during rebuild to reduce lock-held cold latency.
- [x] Run existing full-sort equivalence, offset boundary, generation, task-stat and race tests.
- [x] Compare 100k warm progress-page benchmark against the current 6.9–7.3 ms/op baseline; measure cold rebuild and allocations.
- [ ] Keep the cache only if warm 100k progress pages improve materially with bounded index memory and no correctness or regression concerns; then run full platform checks and package a preview.
