# 远端操作历史检索 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 在现有远端操作历史页按操作 ID、object ID、路径、状态和 UTC 时间范围检索 SQLite 操作记录。

**Architecture:** 保持数据只来自当前账号绑定的 SQLite store。在现有有界分页查询增加参数化筛选，API 负责输入验证与 UTC 解析，页面提供显式搜索/清空控件并复用当前只读结果渲染。

**Tech Stack:** Go、modernc SQLite、net/http、内嵌 HTML/JavaScript、Node `node:test`。

**Spec:** `docs/superpowers/specs/2026-10-30-remote-operation-history-search.zh-CN.md`

## Global Constraints

- 保持远端历史查询为 GET-only、本地 SQLite-only，不触发百度 API、队列或缓存变化。
- `q` 最多 128 个 Unicode 字符；`offset` 最大 1,000,000；`limit` 最大 50。
- `from` 和 `to` 为 RFC3339、包含边界，并在进入 SQLite 查询前转成 UTC Unix 纳秒。
- SQL 用户值均用参数绑定；LIKE 通配符按字面量匹配。
- 不改 SQLite schema，不添加依赖，不提供 mutation/unknown 放行入口。
- 不使用 worktree；保留现有 dirty main 工作区中的其它改动。

## Review Focus

- `%`、`_`、反斜杠被当作字面字符匹配；Store 回归验证。
- RFC3339 时区转换、包含边界、from 大于 to 被拒；Store/API 回归验证。
- q/status/date 参数组合后总数、offset、稳定排序一致；Store/API 回归验证。
- 连续提交不同搜索的响应不让旧请求覆盖新结果；WebUI 回归验证。
- 查询保持 GET-only、不泄漏 fingerprint、不调用 provider；API 回归验证。

---

### Task 1: SQLite 与 Downloader 筛选查询

**Files:** `internal/taskstore/remote_operations.go`, `internal/taskstore/remote_operations_test.go`, `internal/downloader/cloud_operations.go`, `internal/downloader/cloud_operations_test.go`

**Interfaces:** `RemoteOperationFilter{Status, Query string; FromUTC, ToUTC *time.Time}`；`RemoteOperationPageFiltered(filter RemoteOperationFilter, offset, limit int)`；Downloader façade 同名参数并委托 Store。原 `RemoteOperationPage` 保持兼容。

- [x] 先写 Store 测试：字面查询 operation ID/object ID/source/destination 路径，含 `%_\\` 字面字符。
- [x] 运行定向测试，确认缺少 filtered query 时按预期失败。
- [x] 实现参数化 where 条件、LIKE 转义、包含式纳秒时间过滤、状态组合和分页。
- [x] 写并运行边界测试：时间范围组合、from 大于 to、空/非法状态、limit/offset 上限及 closed store。
- [x] 增加 downloader façade 测试，确认输入筛选到达 Store 且无 provider 调用。
- [x] 运行 `go test ./internal/taskstore ./internal/downloader -run 'Test(RemoteOperation|CloudOperation)' -count=1`。

### Task 2: HTTP API 查询参数

**Files:** `internal/intakeapi/server.go`, `internal/intakeapi/cloud_operations.go`, `internal/intakeapi/cloud_operations_test.go`, `internal/intakeapi/server_test.go`, `main.go`

**Interfaces:** Backend 的 `CloudOperationPage` 接收 `taskstore.RemoteOperationFilter`；现有 endpoint 增加 `q`、`from`、`to` query 参数，响应 JSON 结构不变。

- [x] 先写 API 测试：过滤参数传入、RFC3339 时区归一化、空白 q、超长 q、非法时间、反向区间、GET-only 和零远端调用。
- [x] 运行定向测试并确认预期失败。
- [x] 实现 handler 参数校验与 UTC 时间解析；超界返回 400，其余错误继续按当前通用错误处理。
- [x] 运行 `go test ./internal/intakeapi ./internal/downloader ./internal/taskstore -run 'Test(RemoteOperation|CloudOperation)' -count=1`。

### Task 3: 管理页搜索控件

**Files:** `internal/webui/index_cloud.html`, `internal/webui/res/cloud_operations.js`, `scripts/webui.test.cjs`

- [x] 先写 WebUI 交互测试：输入 q 和本地时间后搜索请求带 q/from/to，筛选重置到第一页，清空后恢复全量查询。
- [x] 运行目标 Node 测试确认新行为失败。
- [x] 加入关键字和起止时间控件、提交/清空行为；将 `datetime-local` 转成 RFC3339 UTC。
- [x] 加请求序号保护，旧搜索的迟到响应不得覆盖新条件结果；用两个可控响应顺序的 UI 测试验证。
- [x] 保持 existing `escapeHTML` 与 unknown 无操作展示；运行 `node scripts/webui.test.cjs`、`node scripts/userscript.test.cjs`。

### Task 4: 发布与验证

**Files:** `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/progress.md`, `docs/ai/release-versioning.zh-CN.md`, `.60` 独立 release。

- [x] 更新 `.60` 的路线图、范围、成功标准和外部验收边界。
- [x] 运行 `go test ./... -count=1`、`go vet ./...`、WebUI/userscript 全测与三平台打包。
- [x] 生成 `v0.8.30-preview.60`，核对 ZIP 哈希、SHA256SUMS、源码快照和二进制版本。
- [x] 在运行中的 `.59` 用只读 GET 对 operation ID、object ID、路径、时间边界验证筛选结果；不执行远端写入。

**Success:** 搜索能在 SQLite 全部历史中准确筛选并稳定分页；服务端响应和用户可见错误清楚；本地之外没有云端副作用；`.60` 独立包通过发布门禁。
