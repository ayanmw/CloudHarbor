# Taskstore Bounded Read Prefetch Implementation Plan

> **For agentic workers:** Execute this plan inline with `superpowers:executing-plans`. Each step follows TDD and must be independently verified.

**Goal:** Reduce large-queue startup latency on filesystems where opening many small job files is expensive, while preserving deterministic callback order, record validation, bounded memory, and current JSON v1 durability.

**Architecture:** Keep the existing one-file-per-job format and safe platform-specific open helpers. Windows uses at most four concurrent record reads with worker-owned reusable buffers, then delivers callbacks in filename order; Linux and other platforms keep the serial path because local ext4 measurements showed no benefit from concurrency. The worker cannot reuse a buffer until the ordered callback completes.

**Tech Stack:** Go 1.22, `os.DirEntry`, bounded goroutines/channels, existing taskstore tests and startup benchmarks.

**Spec:** `docs/ai/experience-plan.md`, sections D5-D6.

## Global Constraints

- Preserve JSON v1 records and single-writer locking.
- Preserve `LoadEach` validation and `LoadEachRaw` callback-owned buffer lifetime.
- Preserve sorted filename order and first-error order.
- Continue using platform-specific no-follow/reparse-point file opens.
- Bound concurrent reads and retained in-flight record bytes independently of queue length.
- Default Linux and other non-Windows platforms to serial reads until NAS-backed measurements justify another setting.
- Do not add dependencies, settings, or storage migration.

## Review Focus

- A slow first read must not reorder callbacks when later files finish first; assert IDs remain in filename order.
- A visitor error must stop later callbacks and be returned unchanged; retain existing regression coverage.
- A malformed, oversized, unexpected, or symlinked entry must retain rejection behavior; retain existing platform and taskstore coverage.
- The worker must not overwrite a record buffer before the visitor returns; retain existing reuse-lifetime coverage and verify race tests.
- A very large directory must not create one goroutine or retained buffer per record; implementation uses a fixed maximum of four workers.

---

### Task 1: Add bounded parallel reads with ordered visitation

**Files:**
- Modify: `internal/taskstore/store.go`
- Test: `internal/taskstore/store_test.go`
- Benchmark: `internal/downloader/startup_benchmark_test.go` (reuse existing benchmark fixtures; add narrowly filtered read comparison only if required)
- Modify: `docs/ai/experience-plan.md`, `docs/ai/progress.md`

**Interfaces:**
- `Store.LoadEach` and `Store.LoadEachRaw` retain their signatures and callback contracts.
- Add a private ordered visitor helper parameterized by worker count and a private record-reader callback so deterministic concurrency and order can be tested without timing the filesystem.
- Production callers use no more than four read workers.

- [x] Record a pre-change component baseline: Windows 10k `Store.Load` 1.68–2.36 s/op and WSL 10k `OpenDownloader` about 61–63 ms/op; the original benchmark filter accidentally matched 100k too, prompting a harness correction.
- [x] Add `TestLoadEachConcurrentReadsKeepFilenameCallbackOrder`: force the later read to start while the first is blocked, then assert reads overlap and callbacks still visit IDs `[1,2,3]`. A temporary serial-only probe made the test fail with the expected overlap assertion; restored parallel implementation passes.
- [x] Implement bounded batch readers with one reusable `bytes.Buffer` per worker, preserving filename-order callbacks and releasing results after each batch.
- [x] Run taskstore tests on Windows and WSL Go 1.22.2, including existing ordering, first callback error, malformed JSON, max-size, raw body lifetime, and symlink tests.
- [x] Compare Windows 10k raw reads: about 2.61 s/op with one worker and 1.32 s/op with four; full Windows `OpenDownloader` samples 1.41/1.64 s. WSL ext4 raw reads were 32–35 ms serial and 39–41 ms with four; keep four only on Windows and leave Linux serial pending NAS measurements.
- [x] Run full Windows tests/vet and WSL full race/vet; update D6 measurements with platform and fixture limitations.
- [x] Package and smoke-test immutable v0.8.16-preview.1: 17 Node tests, Windows test/vet, three-platform builds, 11 payload hashes, package startup/auth/401/SIGTERM, and ARM64 ELF check pass.
- [x] Superseded before packaging: v0.8.23 moved the active task store to SQLite, so the per-record JSON filesystem A/B gate no longer measures the current startup path. Current SQLite startup measurements and package evidence are tracked in the SQLite design/progress records; retain this plan as historical evidence for the former JSON backend.
