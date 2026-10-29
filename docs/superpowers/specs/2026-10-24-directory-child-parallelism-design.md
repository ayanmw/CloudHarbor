# Directory Child Download Parallelism Design

## Intent

Improve throughput for a queued directory containing many independent files. Today each root queue worker processes one directory, then downloads that directory's direct children sequentially; a single large directory therefore uses only one network transfer even when `MaxParallel` is greater than one.

## Goal and boundaries

- Download direct file children of each listed directory concurrently, up to the configured `MaxParallel` limit across the entire downloader.
- Keep root scheduler worker count and the global number of active file transfers bounded by `MaxParallel`.
- Wait for every in-flight file and nested directory before marking a directory successful; aggregate ordinary child failures and propagate cancellation/authentication failures using current semantics.
- Keep nested directory traversal recursive and sequential between directory siblings in this change. Do not change listing pagination, retry policy, persisted task format, per-root keep/delete behavior, or when the root source is renamed/deleted.

## Approaches considered

1. Leave root-only parallelism unchanged. It preserves behavior but cannot use configured concurrency for one large directory.
2. Spawn one goroutine per child. It is simple but queue size controls goroutine count and multiple roots can exceed the documented concurrency limit; reject it.
3. Use a bounded per-directory file worker pool plus a shared downloader-wide file-transfer semaphore. This uses the existing limit, bounds workers per directory, preserves the root scheduler, and leaves recursion/source handling unchanged. Use this approach.

## Behavior and error rules

- `MaxParallel=1` remains sequential.
- For higher values, direct file children may finish in a different order, but error aggregation remains deterministic in provider-list order.
- A failed ordinary child does not stop its siblings; the parent remains failed and root post-processing is skipped.
- Authentication failure stops queuing additional siblings; already-running transfers are allowed to observe their existing contexts and settle before the root returns.
- Cancellation stops queuing additional siblings, waits for active transfers to settle, then fails the root without running source post-processing.

## Validation

Use local HTTP and disk fixtures. A blocking download handler must observe parallel sibling transfers for one directory, and concurrent roots must never exceed the global configured transfer cap. Add failure aggregation, cancellation, `MaxParallel=1`, complete local content, and no-root-source-operation checks. Measure a fixed-latency flat directory before/after; package only if throughput improves without violating the cap or failure semantics.
