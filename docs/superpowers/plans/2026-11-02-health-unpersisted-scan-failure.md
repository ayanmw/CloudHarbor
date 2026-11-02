# Health Projection for Unpersisted Scan Failures

> **For agentic workers:** Execute inline in the primary checkout; preserve all existing dirty files, do not use a worktree, and do not commit or push.

**Goal:** Keep `/api/health` and task-page scan status accurate when scan history writes fail, reads fail, concurrent reads overlap a run completion, or a prior running record is recovering.

**Architecture:** Keep SQLite as the source of persisted scan history. Read latest-run and latest-success summaries from one SQLite snapshot. Surface a current-process scan failure if its event write failed; if history reads fail, return a fixed availability code, label any fallback timestamp as runtime evidence, and serialize that timestamp in UTC. The task UI must not describe unreadable history or a startup/recovery state as normal.

**Tech Stack:** Go, existing health handler and queue monitor scan history.

## Steps

1. Add focused regressions for runtime failure masking and latest/latest-success snapshot coherence.
2. Add Go and WebUI regressions for a scan-history read failure and runtime timestamp labeling, including non-UTC process time.
3. Implement the health projection and UI text while keeping persisted and runtime evidence distinct.
4. Run taskstore/downloader/main and WebUI checks plus static analysis; inspect the final diff.
5. Track the four distinct user outcomes in the unnumbered reliability batch; do not create a preview until five independent outcomes are verified.
