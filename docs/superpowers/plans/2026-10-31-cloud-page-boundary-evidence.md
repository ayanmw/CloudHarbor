# Cloud Page Boundary Evidence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist the lookahead identity returned by existing paginated list calls and fail closed when the next page no longer begins with that identity.

**Architecture:** Keep the existing `limit+1` provider request and expose its extra item as an optional boundary record. Persist that record with both full-scan and scoped-refresh checkpoints in SQLite schema 27. Compare the next page's first item before committing it; on mismatch, mark the scan failed and leave the directory incomplete. This is boundary-drift detection, not an atomic snapshot guarantee.

**Tech Stack:** Go, SQLite via modernc.org/sqlite, existing Baidu list client, Go tests.

**Spec:** `docs/superpowers/specs/2026-10-31-cloud-page-boundary-evidence.zh-CN.md`

## Global Constraints

- No Git worktree without explicit authorization.
- No real Baidu account, NAS/SMB, remote mutation, or download for this validation.
- Existing page request rate and `limit+1` request size do not increase.
- Boundary evidence must survive SQLite restart with the scan cursor.
- `complete` does not imply an atomic provider snapshot.

## Review Focus

- Existing page lookahead is retained without adding an HTTP request.
- Boundary metadata changes with stable fs_id are detected.
- Boundary loss caused by insertion/deletion is detected.
- Restart preserves boundary and offset atomically.
- Old active checkpoints without boundary evidence restart the current directory from offset zero.

---

### Task 1: Expose the page lookahead

**Files:**
- Modify: `internal/baiduapi/client.go`
- Test: `internal/baiduapi/client_test.go`

- [ ] Add a page method returning entries, `hasMore`, optional next-boundary item, and error; retain `ListDirPage` compatibility.
- [ ] Verify a two-page mock uses one HTTP request per page and returns the extra item as the boundary.
- [ ] Verify a short final page returns no boundary.

### Task 2: Persist boundary with scan checkpoints

**Files:**
- Modify: `internal/taskstore/sqlite.go`, `migration.go`, `store.go`, `cloud_inventory.go`
- Test: `internal/taskstore/migration_test.go`, `cloud_inventory_test.go`

- [ ] Add private next-boundary fields to full-scan and scoped-refresh state.
- [ ] Add schema 26→27 columns with empty default and include them in schema validation.
- [ ] Save and load boundary JSON alongside state; page transaction atomically advances entries, offset, and next boundary.
- [ ] Verify close/reopen retains the exact boundary and legacy migration initializes it empty.

### Task 3: Enforce boundary continuity in both scanners

**Files:**
- Modify: `internal/downloader/cloud_inventory_scan.go`
- Test: `internal/downloader/cloud_inventory_scan_test.go`

- [ ] On offset zero, accept no previous boundary; otherwise compare the first provider item with the persisted boundary before creating cache entries.
- [ ] On mismatch, mark the scan failed, clear retry scheduling fields, and do not commit the new page.
- [ ] Persist the current page's lookahead boundary with the next offset; clear it at directory completion.
- [ ] Verify full scan and scoped refresh detect boundary deletion, replacement, metadata changes, and restart recovery.
- [ ] For old active state with nonzero offset and no boundary, reset that directory to offset zero before listing and clear its stale per-run page observations safely.

### Task 4: Document semantics and verify release gates

**Files:**
- Modify: `docs/ai/progress.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`, `release/TESTING.md`

- [ ] Document that boundary continuity catches observable drift but is not an atomic snapshot.
- [ ] Run `go test -count=1 ./...`, `go vet ./...`, WebUI/userscript checks, and `git diff --check`.
- [ ] Continue accumulating independent `.108` deliverables; do not package until the 5-item gate is met.
