# Confirmed Cloud Changes in Pending Queue Plan

## Goal

After a confirmed Baidu rename, move, or delete, reconcile an untouched local queue job by its stable `fs_id` without resetting its selected download mode or task history. Keep SQLite as the durable source and link every projection change to the confirmed remote operation and its timestamp evidence.

## Existing architecture

- `Downloader.MutateCloud` serializes remote changes under scheduler maintenance and records a `remote_operations` journal entry.
- `scheduler.reconcileObserved` rebuilds eligibility from a fresh monitor-directory listing. When an existing `fs_id` has changed `FileInfo`, its current code creates a new pending job, which can overwrite the existing projection.
- Jobs are persisted as JSON in the SQLite `jobs` table; `job_events` is append-only and already supports operation IDs, cloud observation times, and response observation times.
- The accepted cloud-management design says to update queue paths by `fs_id`, record remote deletion distinctly, and never guess about active or unknown work.

## Scope and safety rules

- Only untouched `pending` or `auth_wait` jobs with zero attempts/failures, no source operation state, and no active worker may be rebased to the post-operation `FileInfo`.
- Preserve job ID (`fs_id`), mode, creation time, attempts, failures, and all existing operation evidence. Do not reset an existing job to manufacture a new task.
- A confirmed delete of an eligible untouched job transitions it to `cancelled` with a dedicated event linked to the remote operation. Other task states remain intact.
- Ordinary failed, completed, retried, active, or source-operation (`sending`/`unknown`) jobs are not rewritten. If a later scan sees changed metadata for such a job, preserve its projection and do not create a replacement with reset history.
- A failed job with `SourceState == "changed"` is a specific resolved-conflict path: the next successful listing of the same `fs_id` may rebase its metadata, clear the changed marker, and return it to `pending` while preserving mode and attempt/failure counters. This preserves the existing retry-after-fresh-listing contract.
- A zero-attempt `failed` job whose error is the scheduler's source-missing result may be rebased when a confirmed move or later scan returns the same `fs_id` to the monitored directory. Keep it failed and update the message to offer manual retry; never start it automatically.
- Unknown or failed remote mutations do not change queue jobs. The current quarantine rules remain authoritative.
- No SQLite schema migration is needed; use the existing job projection and append-only event columns.

## Acceptance

1. Confirmed rename and move update an untouched queued task by `fs_id`, preserve its mode and creation/history fields, and persist old/new identity plus remote operation/timestamp evidence.
2. Confirmed delete cancels the untouched task with an event carrying the operation ID and response-observed UTC timestamp.
3. Ordinary jobs with attempts, terminal state, active work, or unresolved source operation are not rebased or replaced; their status, mode, attempts, and evidence survive a scan with changed identity. The resolved `changed` source state is requeued after a successful listing without resetting history.
4. Unknown remote mutation does not update the projection and never replays the provider mutation.
5. Focused Go tests, full Go tests, vet, WebUI/userscript checks, three-platform package build and archive checks pass before a new preview is delivered.

## Implementation sequence

1. Add failing scheduler and `MutateCloud` regressions for preserved queue projections, changed identities, delete evidence, and unknown outcomes.
2. Implement safe same-`fs_id` reconciliation and confirmed mutation projection updates using existing SQLite event support.
3. Run focused tests, then the full package gate; review migration compatibility and final diff.
4. Update progress/version roadmap, build the candidate preview only after acceptance passes, and record the final archive hash and validation evidence.
