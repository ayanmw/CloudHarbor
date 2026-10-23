# Global Transfer Worker Budget Implementation Plan

**Goal:** Keep per-directory worker goroutines and queued `downloading` tasks bounded by the Downloader-wide `MaxParallel` transfer budget.

**Design:** `docs/superpowers/specs/2026-10-24-global-transfer-worker-budget-design.md`.

1. Add a RED multi-root blocked-transfer regression showing per-directory worker multiplication.
2. Reserve shared transfer permits before spawning workers; use a slot-held file helper; use the context-aware serial path when no permit is immediately available.
3. Verify permit release and existing auth/cancel/failure semantics; run focused/full tests, race/vet, and the latency benchmark.
4. Package an immutable preview after checks pass; record measured evidence and limits.
