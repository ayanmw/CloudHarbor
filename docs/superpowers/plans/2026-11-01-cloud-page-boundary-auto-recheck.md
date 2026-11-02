# 云目录分页边界漂移自动重核实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 全盘扫描与指定目录刷新在可观察到 offset 页边界漂移时，耐久地从当前目录第一页自动重核，最多两次，并让额度、历史、状态 API 与页面保持一致。

**Architecture:** SQLite 的 `boundary_restart` 追加事件是重核次数的唯一事实源。Store 在一个事务中按账号、run 和目录统计次数、复位检查点、处理扫描缓存或刷新暂存数据并追加事件；达到上限时才进入既有终止失败路径。每个 GET 继续在发送前消耗已持久化请求额度。

**Tech Stack:** Go、SQLite、现有百度网盘 mock、HTTP API、内嵌 WebUI 与 Node WebUI 回归。

**Spec:** `docs/superpowers/specs/2026-11-01-cloud-page-boundary-auto-recheck.zh-CN.md`

## 全局约束

- 每个当前目录最多自动重核 2 次；次数跨重启与额度暂停/恢复，按新 run 或当前目录完成自然归零。
- 常规分页不增加百度 GET；每次实际 GET 都先耐久预留请求额度。
- 全盘扫描重核时保留缓存对象与 SHA-256，只清除当前目录本轮 `last_seen_run_id` 标记；最终页成功后才移除仍未见项。
- 指定目录刷新只清除该 run 当前目录的 staging，保持已发布缓存及其它目录队列不变。
- 不声称远端原子快照，不修改百度远端内容，不使用 worktree，不覆盖无关工作区改动。
- 新 preview 至少提供 5 条独立用户结果后才编号；发布只进入独立 releases 目录，不提交 Git、不 push。

## Review Focus

1. 边界事件写入失败时，复位 offset、缓存标记和事件不得部分提交。
2. 第三次漂移必须停止；已经用掉的 GET 仍计入额度，不得额外发送下一页请求。
3. 额度恰在检测漂移后耗尽时，首页检查点和事件计数须跨关闭/重开一致。
4. 指定目录重核不能删除其它目录 staging 或已发布缓存。
5. 迁移事件表时必须保留事件 sequence、既有索引、append-only trigger，并保证 count 使用部分索引。

---

### Task 1: SQLite schema 31 和边界事件迁移

**Files:**
- Modify: `internal/taskstore/sqlite.go`
- Modify: `internal/taskstore/store.go`
- Modify: `internal/taskstore/migration.go`
- Test: `internal/taskstore/migration_test.go`

**Interfaces:**
- schema 常量递增到 31；迁移注册 `migrateV30`。
- 两种事件表允许 `boundary_restart`，并创建部分索引 `(account_fingerprint,run_id,current_directory) WHERE event_kind='boundary_restart'`。

- [x] **Step 1: 写迁移回归** `TestMigrateV30PreservesScanAndRefreshEventsAndAddsBoundaryRestart`
  - 用 schema 30 数据库建立两种事件表，各插入既有事件并记录其 sequence。
  - 迁移后验证历史值/sequence 不变；两表均能插入 `boundary_restart`；append-only update/delete 仍被 trigger 拒绝；部分索引存在。
- [x] **Step 2: 运行回归观察预期失败**
  - Run: `go test ./internal/taskstore -run TestMigrateV30PreservesScanAndRefreshEventsAndAddsBoundaryRestart -count=1`
  - Expected: FAIL，迁移入口/事件约束缺少 schema 31 支持。
- [x] **Step 3: 实现 schema 31 迁移**
  - 在事务中重建两张带新 CHECK 的事件表并复制原 sequence 与全部列；恢复原索引/trigger 和部分索引；更新 `store_meta` 为 31。
  - 扩展启动迁移链与 fresh schema 定义；manifest/SQLite 版本不一致时沿用现有恢复规则。
- [x] **Step 4: 重跑迁移回归**
  - Run: `go test ./internal/taskstore -run 'TestMigrateV30PreservesScanAndRefreshEventsAndAddsBoundaryRestart|Test.*Migration' -count=1`
  - Expected: PASS；迁移失败注入保持整体回滚。

### Task 2: 事件派生计数和事务化 Store 重核

**Files:**
- Modify: `internal/taskstore/cloud_inventory.go`
- Modify: `internal/taskstore/cloud_scan_history.go`
- Modify: `internal/taskstore/cloud_inventory_test.go`
- Modify: `internal/taskstore/cloud_scan_history_test.go`

**Interfaces:**
- `CloudScanState` 和 `CloudInventoryRefreshState` 增加只读 JSON 字段 `boundary_restart_count`；不新增 SQLite 状态列。
- 新增 Store 事务方法，输入 run state/time/max=2；返回已发生重核数或 `ErrCloudInventoryBoundaryRestartLimitReached`。
- 计数查询限定 `account_fingerprint + run_id + current_directory + event_kind`，使用部分索引。
- 状态与派生计数在同一 SQL 读快照返回；不能分两次无事务读取。

- [x] **Step 1: 写 Store RED 测试**
  - `TestRestartCloudScanBoundaryIsAtomicAndLimitedPerDirectory`：offset>0；前两次各产生一个事件并将 offset/boundary 复位；第三次返回上限错误且状态/事件不变化；新目录和新 run 计数为 0。
  - `TestRestartCloudRefreshBoundaryClearsOnlyCurrentStaging`：复位当前目录 offset/boundary、只清理当前目录 staging、保留其它目录 staging/队列/已发布缓存；第三次不重核。
  - `TestCloudBoundaryRestartCountUsesPartialIndex`：状态读取计数正确，`EXPLAIN QUERY PLAN` 使用新增 partial index。
- [x] **Step 2: 运行定向测试观察预期失败**
  - Run: `go test ./internal/taskstore -run 'TestRestartCloud(Scan|Refresh)Boundary|TestCloudBoundaryRestartCountUsesPartialIndex' -count=1`
  - Expected: FAIL，Store API 和派生字段尚不存在。
- [x] **Step 3: 实现 Store 事务**
  - 扫描：复用同 run/current directory 的 offset-reset 语义清除 `last_seen_run_id`，保持缓存对象/hash 和目录未完成；同事务查数、复位状态和追加事件。
  - 刷新：同事务查数、仅清除当前 run/current directory staging、复位状态并追加事件；不改变队列。
  - Load/恢复状态时从事件表派生计数；终止失败仍使用原逻辑。
- [x] **Step 4: 重跑 Store 测试**
  - Run: `go test ./internal/taskstore -run 'TestRestartCloud(Scan|Refresh)Boundary|TestCloudBoundaryRestartCountUsesPartialIndex|TestCloudInventoryRefreshEvents|TestCloudScan' -count=1`
  - Expected: PASS，失败注入验证状态/事件/缓存同步回滚。

### Task 3: 全盘扫描与局部刷新 worker 自动恢复

**Files:**
- Modify: `internal/downloader/cloud_inventory_scan.go`
- Modify: `internal/downloader/cloud_inventory_scan_test.go`

**Interfaces:**
- 两个 worker 在检测到 `NextOffset>0` 且页首 boundary 不匹配时调用 Task 2 Store 方法。
- 成功复位继续当前 worker 循环；达到两次上限后才保存终止失败状态。

- [x] **Step 1: 写 downloader RED 测试**（实现测试名与提案名不同，按实际覆盖记录）
  - `TestInventoryScannersRecheckWhenPageBoundaryMoves` 覆盖全盘扫描及指定目录刷新，在插入、删除和相同 fs_id 元数据变化下自动重核，并核对请求次数与完成状态。
  - `TestCloudInventoryScanRejectsChangedPersistedBoundaryAfterReopen` 覆盖关闭/重开后拒绝旧边界并成功重核。
  - `TestCloudInventoryScanResetsCheckpointAfterDuplicatePageAndResumes`、`TestCloudInventoryScanResetsCheckpointAfterDuplicateAcrossPages` 覆盖重复 fs_id 的有界失败和修复后恢复；Store 层 `TestRestartCloudScanBoundaryIsAtomicAndLimitedPerDirectory`、`TestRestartCloudRefreshBoundaryClearsOnlyCurrentStaging`、`TestCloudBoundaryRestartCountUsesPartialIndex` 覆盖跨重开派生次数、事务性限制与索引。
  - 请求额度暂停/追加恢复另由 `TestCloudInventoryScanRequestBudgetPausesAndResumesWithAdditionalQuota` 覆盖；不将它描述成与边界漂移合并的重开场景。
- [x] **Step 2: 运行定向测试观察预期失败**
  - Run: `go test ./internal/downloader -run 'Test(FullScan|ScopedRefresh|BoundaryDrift|BoundaryRestartBudget)' -count=1`
  - Expected: 新恢复测试 FAIL，现有“立即失败关闭”回归仍 PASS。
- [x] **Step 3: 接入两个 worker**
  - 仅边界不匹配进入自动重核；认证/API 临时错误仍按当前重试计划处理；其他验证失败继续失败关闭。
  - 对局部刷新重核不得调用失败保存路径；额度不足保持已复位 checkpoint 并暂停。
- [x] **Step 4: 重跑 worker 回归**
  - Run: `go test ./internal/downloader -run 'Test(FullScan|ScopedRefresh|BoundaryDrift|BoundaryRestartBudget)' -count=1`
  - Expected: PASS；现有稳定分页、额度、重复 fs_id、取消恢复测试不回退。

### Task 4: 状态 API、扫描历史与页面反馈

**Files:**
- Modify: `internal/taskstore/cloud_inventory.go`
- Modify: `internal/taskstore/cloud_scan_history.go`
- Modify: `internal/intakeapi/server.go`
- Modify: `internal/intakeapi/server_test.go`
- Modify: `internal/webui/index_cloud.html`
- Modify: `scripts/webui.test.cjs`

- [x] **Step 1: 写 API/UI RED 回归**
  - API 状态 JSON 暴露派生 `boundary_restart_count`，不暴露账号指纹、boundary JSON；历史事件新增 kind 可查询与导出。
  - WebUI 运行中的扫描/刷新显示“目录分页漂移，重新核对 N/2”；额度暂停和次数耗尽使用不同说明；两个历史事件列表显示本机记录 UTC 和中文标签。
- [x] **Step 2: 运行聚焦测试观察预期失败**
  - Run: `go test ./internal/intakeapi -run 'Test.*(InventoryScan|ScanHistory|Refresh)' -count=1`，`node scripts/webui.test.cjs`
  - Expected: 新断言 FAIL。
- [x] **Step 3: 接通 API 与 UI**
  - 复用现有扫描/刷新状态和历史路由；更新事件 kind 校验、筛选/导出白名单及 `scanHistoryEventLabels`、`refreshEventLabels`。
  - 显示 `boundary_restart_count` 时不改变账号隔离或私有字段脱敏。
- [x] **Step 4: 重跑 API/UI 聚焦测试**
  - Run: `go test ./internal/intakeapi -run 'Test.*(InventoryScan|ScanHistory|Refresh)' -count=1`，`node scripts/webui.test.cjs`
  - Expected: PASS。

### Task 5: 全量回归、发布并记录 `.118`

**Files:**
- Modify: `docs/ai/cloud-management-roadmap.zh-CN.md`
- Modify: `docs/ai/progress.md`
- Modify: `docs/ai/release-versioning.zh-CN.md`
- Create outside source tree: `D:\projects\Git-Projects\nas_baidupan_auto_downloader-releases\v0.8.30-preview.118.zip` and validation record

- [x] **Step 1: 全仓门禁**
  - Run `go test ./... -count=1`, `go vet ./...`, WebUI/userscript tests, `git diff --check`。
  - Expected: all pass; failures先查明并修复，保持 `.118` 同版迭代。
- [x] **Step 2: 跨平台构建与打包**
  - Run `scripts/package.ps1` for `v0.8.30-preview.118` with dirty-workspace allowance per repo conventions.
  - Expected: Windows AMD64、Linux AMD64、Linux ARM64，ZIP、源码快照、SHA-256、私有数据排除和版本命令核验通过。
- [x] **Step 3: 独立核对发布目录**
  - 对 ZIP 项目、载荷哈希、源码快照和 SHA-256SUMS 逐项复核；记录真实百度/NAS 未验收边界。
- [x] **Step 4: 更新路线图与验证记录**
  - 五项用户结果均有对应回归：全盘恢复、局部刷新恢复、跨重启限次、额度暂停续跑和审计时间证据、页面进度/终态解释。
  - 不 commit、不 push、不启停用户真实账号服务。
