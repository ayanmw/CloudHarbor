# Global Transfer Worker Budget

## Context

v0.8.19 caps active network transfers with a Downloader-wide semaphore, while every concurrently executing directory independently creates up to `MaxParallel` file workers. With `R` active root folders and a limit `P`, as many as `R*P` child goroutines can be created and most can wait inside `downloadLocalFile` after their task is marked downloading.

## Decision

Reserve available Downloader transfer slots before creating directory file workers. A directory starts no more file workers than permits it immediately acquires. Each worker retains its permit while consuming that directory's direct-file queue and calls a slot-held transfer helper. If no permit is immediately available, the directory coordinator processes its files sequentially through the ordinary context-aware slot acquisition path.

This bounds additional directory transfer workers globally to `MaxParallel` while preserving overlapping direct children when slots are available. It does not change the scheduler's root worker count, persisted task format, retry/resume path, or source post-processing.

## Constraints

- Active file transfer and reserved file worker count never exceed configured `MaxParallel`.
- Every permit is released after its worker exits, including cancellation and authentication failure.
- Authentication failure stops scheduling siblings and cancels/waits for outstanding workers.
- Cancellation while waiting for a permit returns promptly; cancellation with reserved workers closes their job feed and waits for them.
- `MaxParallel=1` stays serial.
- Root folders still finish and run source operations only after all descendant transfers succeed.

## Validation

- Regression with multiple blocked directory roots asserts child tasks do not all become downloading while waiting for the small global transfer budget.
- Existing sibling-concurrency, cross-root peak, auth fail-fast, cancellation, aggregation, and `MaxParallel=1` tests remain green.
- Run full Go tests, race, vet, fixed-latency benchmark, and package the next preview only if results pass.
