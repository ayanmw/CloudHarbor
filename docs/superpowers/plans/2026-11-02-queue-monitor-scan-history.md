# 自动队列扫描历史与重启恢复实施计划

> **执行方式：** 主目录内联实施。保留 dirty/untracked 内容；不创建 worktree、不提交/推送。

**目标：** 持久记录自动监控队列扫描结果和本机/百度时间证据，修正 skipped scan 被误记为成功，并提供只读历史查询。

**架构：** SQLite v33→v34 新增独立的账号隔离运行摘要与 append-only 事件表。Downloader 在持有扫描互斥和当前账号后创建 run；所有终态与事件原子写入。管理 API/UI 复用现有 keyset 分页、筛选、事件读取和有界显式导出模式。

**技术栈：** Go、modernc SQLite、现有 net/http、嵌入 HTML/JavaScript、Node `node:test`。

**设计：** `docs/superpowers/specs/2026-11-02-queue-monitor-scan-history.zh-CN.md`

## 全局约束

- 不创建或使用 Git worktree；不清理其他人的 dirty/untracked 文件；不提交或推送。
- SQLite 所有状态转换与对应事件同事务；事件 append-only；schema manifest 和数据库迁移可恢复。
- 本机记录 UTC 与百度目录响应观察 UTC 分字段；失败/skip 不伪造云端时间。
- API 账号隔离且使用显式 DTO；不得导出内部 account fingerprint、token 或密钥。
- skipped 与 completed 必须区分；无结果未知时不触发额外网盘请求或操作。
- 预览版累计五条独立用户结果后才递增；当前 `.127` 为最新交付，使用 `.128`。

## Review Focus

1. SQLite 完成摘要写入失败：内存健康状态不能先报成功；run 应保留为可由重启标记为 interrupted 的状态。
2. 进程在 start event 提交前/提交后退出：不得出现无 start 的摘要或伪造完成；迁移/运行恢复要幂等。
3. maintenance/scan mutex skip：不得更新 last-success 时间或创建可被 UI 显示为成功的记录。
4. 账号切换：列表、事件和导出不得泄漏其他账号记录或 account fingerprint。
5. 迟到 UI 响应、重复点击导出和超限历史：不覆盖新选择、不隐式下载、不返回部分证据。

---

### Task 1：SQLite 队列扫描运行历史存储

**Files:** `internal/taskstore/sqlite.go`、`internal/taskstore/migration.go`、新增 `internal/taskstore/queue_monitor_scan_history.go`、相邻 taskstore 测试。

**Interfaces:** Produces create/start/finish/skip/list/events/export methods and DTOs. Uses Store locking, schema migrations, UUID and export patterns already in taskstore.

- [x] 实现 schema 33→34 迁移、摘要/事件表、索引和 append-only 触发器；跨重开读回、账号隔离、UTC 日期筛选及摘要/事件原子事务测试通过。
- [x] 新进程开始同账号 run 时，将遗留 `running` 用本机 UTC 标记为 `interrupted`；保留已记录的百度观察时间；专门 v33→v34 迁移和 SQLite 完整性检查通过。
- [x] 提供稳定 sequence 分页、显式证据 DTO 和 10,000 条全有或全无 JSON 导出上限。
- [x] 账号隔离键使用本地绑定用户 ID 的 SHA-256，跨 OAuth token 刷新稳定；序列化不返回账号标识。

### Task 2：Downloader 扫描运行状态与健康 API

**Files:** `internal/downloader/downloader.go`、新增 `internal/downloader/queue_monitor_scan_history.go`、`main.go`、对应 Go 测试。

**Interfaces:** Consumes Task 1 Store methods. Produces Downloader latest/history methods and scanner lifecycle integration.

- [x] 实际扫描写入 start、百度列表响应、finish/failure 时间；错误只存固定分类 code。
- [x] maintenance/并发扫描返回 skip sentinel；跳过时不建摘要、不调用百度 ListDir；`runScanner` 不改最近扫描或最近成功结果。
- [x] 健康快照从 SQLite 读取当前绑定账号最近真实扫描和最近成功时间，worker 状态仍来自当前进程。
- [x] mock HTTP 扫描回归覆盖云端/本机时间证据、跨重启历史读取与稳定绑定账号。

### Task 3：只读队列扫描历史 API

**Files:** `internal/intakeapi/server.go`、`main.go` 路由、`internal/intakeapi/server_test.go` 和 main integration tests。

**Interfaces:** Consumes Task 1/2 Downloader history APIs. Produces authenticated GET routes for list, run events, and selected-run JSON export.

- [x] 接入只读列表、事件和导出 GET API，校验分页、状态、日期和 run ID；显式 DTO 省略账号指纹。
- [x] API 回归覆盖非法状态/日期/页大小/run ID 及本机和百度时间字段。

### Task 4：独立自动监控扫描历史页

**Files:** 新增 `internal/webui/index_monitor_history.html`、`internal/webui/res/monitor_history.js`、`internal/webui/webui.go`、`internal/webui/index_poll.html`、`internal/webui/index_ws.html`、`scripts/webui.test.cjs`。

**Interfaces:** Consumes Task 3 routes; task page links to `/monitor-history`; page bundle supplies filters, cursor paging, selected run event paging and explicit export.

- [x] 添加 Poll/WebSocket 任务页入口和独立只读历史页：状态/UTC 日期筛选、cursor 翻页、事件按需分页和单轮 JSON 导出。
- [x] 路径/错误/事件安全转义；打开页面只读本地历史 API，不请求百度；导出仅由用户显式点击触发。
- [x] WebUI 定向回归 141/141 通过；userscript 与全套门禁待 package 复验。

### Task 5：进度文档与 `.128` 发布

**Files:** `docs/ai/progress.md`、`docs/ai/cloud-management-roadmap.zh-CN.md`、`docs/ai/release-versioning.zh-CN.md`，外部 release validation record。

- [x] 汇总六条独立用户结果：持久历史、分离的时间证据、崩溃恢复、skip 语义、令牌刷新后的稳定账号隔离、跨重启健康快照和可筛选查看/导出工作流。
- [x] `git diff --check`、源码完整性及包源快照检查通过。
- [x] `.128` package 门禁通过 WebUI/userscript、全仓 Go 测试/vet 和三平台构建；后续源码/文档修改后通过 `-UpdatePreview` 重复全部门禁。
- [x] Windows/Linux AMD64 `--version`、ZIP 清单/载荷哈希、源码快照、无运行数据和 staging 清零均核验；最终 SHA/记录位于 releases 同级验证记录。
- [x] 门禁无失败；若后续发现问题，留在 `.128` 修复并重复门禁，不递增版本。
