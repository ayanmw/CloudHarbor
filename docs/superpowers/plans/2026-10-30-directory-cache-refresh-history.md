# 目录缓存刷新历史实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 为目录缓存刷新保存可跨重启读取的关键事件，并在网盘管理页提供按需只读时间线。

**Architecture:** SQLite schema 19 增加账号/run 隔离的 append-only 刷新事件表。刷新状态转换和目录缓存发布在现有事务中写事件；认证 API 使用稳定 sequence 游标分页，页面按需展示。事件不逐页记录。

**Tech Stack:** Go、SQLite（modernc）、net/http、嵌入式 HTML/JavaScript、Node WebUI tests。

**Spec:** `docs/superpowers/specs/2026-10-30-directory-cache-refresh-history.zh-CN.md`

## Global Constraints

- 不访问真实百度账号或执行真实云端操作。
- 不改变缓存刷新频率、远端请求、缓存发布语义和 unknown 隔离策略。
- 所有时间证据用 UTC；目录列表读取完成时间由本机时钟产生，并非百度服务器时间，与稍后写入事件的本机 UTC 分开记录。
- 不使用 worktree、不提交 Git；保留无关工作区改动。

## Review Focus

- 状态更新已提交但事件未提交：事件与刷新状态必须共用事务。
- 缓存目录发布已提交但缺少目录事件：发布与事件必须共用事务。
- 重启后恢复同 run：只能追加 resumed，不覆盖 started 或历史事件。
- 分页期间新增事件：sequence 游标不能重复或跳过既有边界记录。
- 跨账号/跨 run 请求：不得返回其他作用域事件。

---

### Task 1: SQLite 事件存储与 v18→v19 迁移

**Files:** `internal/taskstore/sqlite.go`, `internal/taskstore/store.go`, `internal/taskstore/migration.go`, `internal/taskstore/migration_test.go`, `internal/taskstore/cloud_inventory.go`, `internal/taskstore/cloud_inventory_test.go`

- [x] 添加迁移与事件分页测试：v18 数据升级保留刷新状态；事件 append-only、序号稳定、账号/run 隔离、关闭重开后读取；事件写失败时对应事件/刷新状态/目录正式缓存一并回滚。
- [x] 运行定向测试确认迁移/事件用例先失败。
- [x] 实现 schema 19、必要索引、幂等迁移及 `CloudInventoryRefreshEvent` 追加/分页接口；事件写入复用刷新事务，不创建第二条可部分提交的路径。
- [x] 再运行 taskstore 迁移和事件回归，确认通过。

### Task 2: 刷新生命周期与目录发布事件

**Files:** `internal/taskstore/cloud_inventory.go`, `internal/downloader/cloud_inventory_scan.go`, `internal/downloader/cloud_inventory_scan_test.go`, `internal/taskstore/cloud_inventory_test.go`

- [x] 添加测试覆盖 started/resumed/paused/failed/completed 和目录发布事件的顺序、UTC 时间、provider observed time、变化标志与进度快照。
- [x] 验证新增断言在实现前失败。
- [x] 将 started/resumed/paused/failed/completed 事件与对应状态保存置于同一事务；目录发布事件与最终页缓存发布同事务，并记录 changed、provider observed time 和刷新计数。
- [x] 运行 Store/downloader 定向回归与相关重启测试。

### Task 3: 认证只读分页 API

**Files:** `internal/intakeapi/server.go`, `internal/intakeapi/server_test.go`, `internal/downloader/cloud_inventory_scan.go`

- [x] 添加 API 测试覆盖 run ID、limit、before_sequence 参数校验、账号隔离、稳定游标及只读 GET 不启动网盘请求。
- [x] 验证测试先失败。
- [x] 增加认证保护的 `/api/cloud-inventory/refresh/events?run_id=…&limit=…&before_sequence=…` GET，单页最多 100 条，最新序号优先；空/非法 run ID 返回明确 400，不泄露底层错误。
- [x] 运行 intakeapi/downloader 定向测试。

### Task 4: 网盘管理页按需事件时间线

**Files:** `internal/webui/index_cloud.html`, `scripts/webui.test.cjs`, `docs/ai/progress.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/release-versioning.zh-CN.md`

- [x] 增加测试确认刷新历史默认无额外请求、用户展开后读取当前 run、加载更早使用稳定游标、UTC 本机/线上时间标签、转义路径、切换 run 忽略迟到响应、失败保留重试。
- [x] 验证新 UI 断言先失败。
- [x] 在目录刷新详情加入可折叠历史时间线；明确标记“本机 SQLite 事件”与“百度列表观测时间”；保留现有只读/不发远端请求行为。
- [x] 更新进度与路线图，记录 `.87` 范围和未验收边界。
- [x] 运行 WebUI/userscript、全仓 `go test ./...`、`go vet ./...`、`git diff --check` 及三平台发布包门禁；核对 ZIP 清单、源码快照和私有运行数据。


