# 增量网盘缓存核对与提交实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 目录刷新期间继续使用最近一次完整缓存，目录列表完整后才增量发布，并让 confirmed 远端操作结果单独修正本地缓存。

**Architecture:** SQLite 增加账号/运行/目录隔离的分页暂存表。刷新页将远端行和检查点同事务写入暂存表，完整目录列表抵达后在单一事务中核对正式 inventory、hash 和目录观测元数据。远端 confirmed 操作在缓存更新拒绝或身份漂移时按响应后的线上身份执行有证据的定点修正；无法证明时继续失效并持久记录。

**Tech Stack:** Go、SQLite (`modernc.org/sqlite`)、现有云端 inventory / refresh 任务、HTTP intake API 与 WebUI 测试。

**Spec:** `docs/superpowers/specs/2026-10-30-incremental-cloud-cache-reconciliation.zh-CN.md`

## Global Constraints

- 不创建或使用 Git worktree。
- 不调用真实百度账号进行测试，不进行真实远端写入、完整扫描或删除。
- 正式缓存只接受完整目录列表；未知/失败/中断不能发布暂存内容。
- 每个 hash 必须继续绑定账号、fs_id、path、size、provider_mtime。
- 仅 confirmed provider 结果可修正缓存；缓存修复失败必须作为本地证据记录，不改写远端操作状态。
- 保留现有 API 限速、分页和暂停/续跑行为。

## Review Focus

- 暂存的多页列表不能被读接口/重复预览误当作完整远端目录；由刷新中读取旧正式内容测试覆盖。
- 暂停或崩溃时最后已提交的正式缓存不能损坏；由重新打开 SQLite 后恢复/失败测试覆盖。
- 文件元数据不变时 hash 不能误删，任何身份/内容字段变化必须清 hash；由增量对比测试覆盖。
- 父目录移除子目录必须清除对应子树和 hash，兄弟目录必须保留；由子树一致性测试覆盖。
- confirmed 后缓存不匹配不能把远端操作重新标为 unknown，也不能保留线上已删除对象；由 Store/API 操作回归覆盖。

---

### Task 1: SQLite 刷新暂存存储

**Files:** `internal/taskstore/sqlite.go`, `internal/taskstore/migration.go`, `internal/taskstore/store.go`, `internal/taskstore/migration_test.go`

- [x] 写 schema 与迁移失败测试：v17 升至 v18 创建以 `(account_fingerprint,run_id,directory,fs_id)` 隔离的 staging 表及必要索引；重复/恢复迁移保持幂等。
- [x] 运行定向迁移测试，确认失败信息指向缺失 schema。
- [x] 实现 schema 18 和原子迁移；仅新 run 第一次读取时清理旧 run 暂存，保留活跃 run 检查点。
- [x] 运行 taskstore migration 全测。

### Task 2: 目录页暂存和原子发布

**Files:** `internal/taskstore/cloud_inventory.go`, `internal/taskstore/cloud_inventory_test.go`, `internal/taskstore/cloud_duplicates.go`, `internal/taskstore/cloud_duplicates_test.go`

- [x] 添加 Store 回归：中间页只写 staging，正式列表、目录 complete 状态、generation 与 hash 组保持原样。
- [x] 完整多页后单事务比较并发布：同一列表不增加 generation；变化列表清理缺失项、upsert 改动项；仅受影响 hash 失效；删除目录清理后代。
- [x] 添加暂停重开、页面失败保留正式快照、相同清单不抖动 generation、MD5 改动只失效受影响文件 hash 的测试。
- [x] 将局部刷新写入改为 staging API，刷新任务/游标/暂存行保持事务一致；全盘初始扫描路径保持既有行为。
- [x] 运行 taskstore 和 downloader 定向测试。

### Task 3: confirmed 远端操作的缓存修正

**Files:** `internal/taskstore/cloud_inventory.go`, `internal/taskstore/remote_operations.go`, `internal/taskstore/remote_operations_test.go`, `internal/downloader/cloud_management.go`, `internal/downloader/cloud_management_test.go`

- [x] 复现 confirmed 后本地 identity mismatch 导致 inventory 未更新的旧行为。
- [x] 实现有界的 confirmed repair 事务：以线上响应后的 fs_id/path/size/mtime 为准修正本地项；删除确认不存在的项；校验相应 hash 是否可保留。
- [x] 记录独立缓存修正证据（operation ID、对象 ID、旧/新身份、结果、UTC 时间）；不得把 provider confirmed 改成 unknown。
- [x] unknown、身份无法确认和本地事务失败均保持隔离/刷新提示，不伪造修复成功。
- [x] 运行 Store/API/downloader 相关测试。

### Task 4: UI 状态说明与验收

**Files:** `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`, `docs/ai/progress.md`, `docs/ai/release-versioning.zh-CN.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`

- [x] UI 测试确认刷新期间仍明确显示缓存观测时间，并说明当前列表是上次完整快照；不增加新的自动远端请求。
- [x] 更新中文进度、路线图和验收边界。
- [x] `node scripts/webui.test.cjs`（61/61）、`node scripts/userscript.test.cjs`（3/3）、`go test ./...`、`go vet ./...`、`git diff --check` 全部通过。
- [x] Windows AMD64、Linux AMD64、Linux ARM64 构建通过；最终 `.86` ZIP、来源快照、12 项载荷清单和私有运行数据检查通过，外部验证记录已写入 release 目录。
