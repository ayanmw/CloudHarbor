# Batched Queue Admission Implementation Plan

**Goal:** Reduce the delay when a cloud scan discovers many new root tasks while keeping each task record individually atomic and durable before workers can claim it.

**Architecture:** Process new jobs in bounded batches of at most 1,024 records. Prepare and fsync each file in parallel with a fixed four-worker cap, atomically rename each into `jobs/`, then sync the directory once per batch. Only after every batch succeeds does `reconcile` publish the jobs in memory and release its scheduler lock. Existing record updates continue using the single-record save path.

**Safety basis:** These are newly discovered roots whose source items remain in the cloud queue. Workers cannot claim them while `reconcile` holds the scheduler mutex. If a batch is interrupted or partially renamed, any recovered records are valid and the next complete cloud scan can register missing roots; no source operation or deletion occurs during admission.

**Review focus:** Validate the full batch before writing; keep JSON validation, 1 MiB record limits, file sync, same-directory atomic renames, symlink checks, single-writer lock, and directory sync error propagation. A failed batch must not publish partial in-memory jobs or start workers.

## Tasks

- [x] Add a reproducible fresh-queue reconcile benchmark and record 1,000/10,000-job baseline.
- [x] Add RED tests for batch record round-trip, validation failure before any publication, and exactly one directory sync per successful batch.
- [x] Implement `durablefs.WriteFilesAtomic`, taskstore batch-save API, and staged batch admission in `scheduler.reconcile`.
- [x] Verify partial rename and directory-sync failure behavior; Windows/WSL package tests, WSL race, vet, Node tests and three-platform builds pass.
- [x] Keep the change after measurable fresh-queue admission gains with source tasks unchanged until durable registration; package immutable v0.8.18-preview.1 and complete package smoke.
