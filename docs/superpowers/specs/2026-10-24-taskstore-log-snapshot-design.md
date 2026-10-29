# Task history journal and snapshot design

> Superseded on 2026-10-28 by [the SQLite-backed design](2026-10-28-taskstore-sqlite-design.zh-CN.md), following the user's storage decision. Retained as historical context only; do not implement this proposal.

中文版本：[任务历史日志与快照设计](2026-10-24-taskstore-log-snapshot-design.zh-CN.md)。

## Intent

Reduce restart cost on NAS filesystems by replacing one-file-per-root-task history with sequential journal replay plus periodic snapshots. Existing v1 queues must migrate without dropping task history or weakening the source-operation uncertainty rules. The downloader will continue to keep the current in-memory job map after startup; this change targets startup file operations and future persistence cost, not resident memory.

The user asked for continued optimization and independently testable releases. This document selects a design baseline for review; it does not authorize implementation of the new on-disk format until the design is approved.

## Goals

- Reduce startup from enumerating and opening every task file to opening a bounded number of snapshot and journal files.
- Preserve every task's latest durable `queueJob` state, task ordering contracts, task query behavior, and complete/failed history.
- Preserve write-ahead ordering: a `SourceState="sending"` transition must be durably acknowledged before a cloud rename/delete request can start.
- Keep queue scope binding, single-writer locking, private file permissions, symlink/reparse-point defenses, record size limits, and fail-closed corruption behavior.
- Migrate current v1 `manifest.json` plus `jobs/*.json` queues without modifying their source records until a complete v2 snapshot is durably published.
- Keep intake journal, credentials, administrator key, and download-mode settings in their current independent stores.
- Add benchmarks and synthetic migration/recovery tests; issue an immutable preview package only after full validation.

## Non-goals

- Change task lifecycle semantics, retry policy, source rename/delete behavior, unknown-result isolation, or history retention.
- Reduce the full in-memory task map or change task API payloads.
- Add SQLite or a runtime-configurable storage backend.
- Claim NAS speed or power-loss guarantees from local ext4/Windows tests.
- Automatically downgrade an already migrated v2 queue to an older binary. Older binaries must reject a v2 manifest clearly instead of reading stale v1 records.

## Constraints and current evidence

- v1 stores one JSON record per task. `Save` writes and syncs a temporary file, atomically renames it, and syncs the parent directory. `SaveBatch` syncs each temporary record, publishes each name, then syncs the jobs directory.
- Normal task lifetime persists a new `pending` record, `downloading` before work, then a completion/failure record. Retry, cancellation, authentication parking, and source-operation states also persist full jobs.
- Startup relies on `LoadEachRaw` filename ordering and strict validation. Corrupt records, unsafe paths, and a second writer stop startup.
- Intake `sending` becomes `unknown` after restart and remains isolated without a synchronous completion credential. Its journal is intentionally out of scope.
- SQLite WAL is not selected because its official documentation says WAL does not work over network filesystems; NAS/SMB locking and sync behavior varies by implementation. Local benchmarks will not substitute for target NAS acceptance.

## Approaches considered

1. **Keep one JSON file per job.** This preserves current behavior and avoids migration risk, but leaves the known many-small-file startup cost. It remains the fallback if the new format cannot meet the durability tests or target NAS measurements show no benefit.
2. **Use SQLite.** This provides transactions and indexes, but introduces a database dependency and network-filesystem locking/durability concerns. WAL is unsuitable for the intended NAS queue location, and rollback-journal mode still depends on the NAS locking implementation.
3. **Use an append-only job journal with periodic full snapshots.** This reduces active file count, supports sequential replay, and can retain the existing full JSON job record as each event payload. It requires framing, checksums, checkpoints, and an explicit v1 migration. This is the recommended baseline because it directly addresses the measured file-open bottleneck without relying on SQLite locking over NAS.

## Proposed format and write semantics

- Root manifest v2 binds the queue scope and selects a task-store directory containing a generation manifest, one immutable snapshot, and one active journal. The intake and credential sidecars remain siblings and keep their current APIs.
- Snapshot records contain the latest complete `queueJob` per FsID, ordered by the current v1 filename ordering so `LoadEach` retains its callback-order contract.
- Journal frames contain a monotonically increasing sequence, FsID, payload length, checksum, and the complete marshaled `queueJob`. Individual `Save` frames are fully written and `Sync`ed before success returns. `SaveBatch` validates all inputs before append, writes the batch, and syncs before success; on an error, the scheduler stops and the cloud sources remain available for reconciliation as today.
- Replay starts from the snapshot sequence and applies newer frames. Duplicate/out-of-order sequences, invalid IDs, oversized frames, invalid JSON, and checksum failures fail closed.
- Any incomplete trailing frame also fails startup rather than being silently ignored or truncated. A torn tail could be a `sending` transition whose sync result was not durably honored by the filesystem; silently dropping it could allow a later request to repeat a cloud mutation. Preserve the journal unchanged for diagnosis.
- Checkpointing holds the store writer lock. It writes and syncs a new full snapshot, creates and syncs a new empty journal generation, atomically switches the generation manifest, syncs its directory, then makes older generations eligible for cleanup. A crash before the manifest switch keeps the old generation authoritative; a crash after the switch has a complete snapshot and journal ready.
- Keep the same single-process writer lock and platform-specific secure file-opening rules. New files use owner-only permissions. Directory sync failures remain errors where supported and are reported without claiming durability on unsupported filesystems.

## v1 migration and recovery

1. Open and validate the current v1 manifest and every legacy job record using the existing hardened loader. Keep all source files untouched.
2. Build a staged v2 snapshot from all records, sync the snapshot and staged directory, then atomically publish the v2 store directory and sync its parent.
3. Atomically replace the root manifest with v2 only after the staged store is complete and reopenable. Until the manifest rename begins, v1 remains authoritative; errors before that point may remove only the incomplete staging directory and continue using v1. If the manifest write/rename/directory-sync reports an error, its durable outcome is uncertain: close the store and fail startup without cloud mutations, preserving both generations for the next open to inspect. Do not continue in v1 mode in the same process after an uncertain switch.
4. After the switch, v2 is authoritative. Do not silently fall back to stale v1 records if v2 is missing or corrupt. Retain legacy files until v2 has been reopened and validated; then remove the legacy copy through restartable cleanup. If cleanup is interrupted, v2 remains authoritative and cleanup resumes later.
5. Document temporary peak disk use during migration and the no-automatic-downgrade rule. The package must expose migration outcome clearly in logs and testing instructions.

## Validation and success criteria

- Unit tests cover frame encoding, checksum/length limits, sequence validation, ordering, batch append, sync errors, valid replay, incomplete-tail fail-closed behavior, mid-log corruption, and checkpoint crashes before/after manifest switch.
- Migration tests begin with a real v1 fixture, assert exact per-FsID payload and ordering equivalence after migration, inject failures at each publication boundary, and prove the v1 source remains recoverable until the v2 switch.
- Downloader regressions prove `sending` is durable before the source HTTP request, restart still converts unresolved `sending` to `unknown`, and no error path automatically resends a cloud operation.
- Compare 10k and 100k synthetic startup on Windows and WSL ext4 with the same Go toolchain and fixtures; report medians, allocation/heap data, migration peak disk size, and both cold/warm runs. Do not claim NAS gains without target hardware.
- Run Windows tests/vet, Go 1.22.2 WSL race/vet, Node UI tests, Windows AMD64/Linux AMD64/Linux ARM64 builds, immutable archive payload hashes, and Linux AMD64 package smoke.
- The preview is successful only if all tests pass, existing v1 fixtures migrate without data loss, failure injection keeps the old queue usable until commit, and package downgrade/corruption behavior is documented.

## Calls left to implementation

- Choose an initial checkpoint threshold from measured journal growth without adding user configuration.
- Choose a compact, versioned binary frame layout and checksum implementation from the Go standard library.
- Keep migration, checkpoint, and legacy cleanup bounded and cancellable where the existing store lifecycle permits.
