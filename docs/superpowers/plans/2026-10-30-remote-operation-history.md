# 远端操作历史只读视图实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Work in the existing main checkout because the user has explicitly prohibited worktrees. Do not clean or overwrite unrelated dirty files.

**Goal:** 在管理页提供可分页、可按状态筛选的只读远端操作历史及事件证据详情。

**Architecture:** 在 `taskstore` 中为既有 SQLite 操作投影和 append-only 事件增加有界分页读取；`Downloader` 将其作为本地只读能力提供给 `intakeapi`。管理页面通过仅 GET 的受保护 API 按需加载列表与事件，不调用百度 API，不提供重发、放行或删除记录入口。

**Tech Stack:** Go `database/sql` + modernc SQLite、现有 `net/http` 管理 API、内嵌 WebUI/JavaScript、Node `node:test`。

**Spec:** `docs/superpowers/specs/2026-10-30-remote-operation-history.zh-CN.md`

## 文件边界

- `internal/taskstore/remote_operations.go`：新增操作页和单操作事件页查询及分页数据类型。
- `internal/taskstore/remote_operations_test.go`：时间排序、状态筛选、分页、事件读取及失败路径回归。
- `internal/downloader/cloud_operations.go`：薄封装当前 store 的只读操作历史读取。
- `internal/downloader/cloud_operations_test.go`：验证缺失/关闭 store 等边界，不接触百度网络。
- `internal/intakeapi/server.go`：扩充只读 Backend 接口并路由到新 handler。
- `internal/intakeapi/cloud_operations.go`：分页输入校验、JSON 响应和证据敏感字段掩码。
- `internal/intakeapi/cloud_operations_test.go`：GET-only、参数校验、not-found、敏感信息掩码和无远端调用。
- `main.go`：注册 `/api/cloud/operations` 和 `/api/cloud/operations/{id}/events`。
- `internal/webui/index_cloud.html`：增加折叠式“远端操作历史”区域并引用新脚本。
- `internal/webui/res/cloud_operations.js`：列表筛选、分页、按需事件详情与安全文本渲染。
- `scripts/webui.test.cjs`：覆盖历史卡片交互、筛选/分页、UTC/unknown 展示、XSS 转义及无变更控件。
- `docs/ai/release-versioning.zh-CN.md`、`docs/ai/cloud-management-roadmap.zh-CN.md`、`docs/ai/progress.md`：记录 `.59` 范围和证据。

## Task 1: SQLite 操作与事件有界分页

**Files:** `internal/taskstore/remote_operations.go`, `internal/taskstore/remote_operations_test.go`

- [x] 先写失败测试：操作倒序、状态过滤、offset/limit、未知状态/边界拒绝或返回空页。
- [x] 运行定向测试，确认新测试按预期失败。
- [x] 新增 `RemoteOperationPage` 元数据和 `RemoteOperationEvent` 类型；SQL 查询使用参数、稳定排序、硬上限。操作查询不读取 provider API，也不按可轮换的 access-token 指纹过滤；单目录 account binding 是数据边界。
- [x] 写失败测试：单 operation 事件按序列倒序分页，错误 ID/越权不存在 ID返回同一种 not-found，closed store 返回关闭错误。
- [x] 实现事件页联表读取，先检查 operation 在本 store 中存在；只返回其自身事件。
- [x] 运行 `go test ./internal/taskstore -run 'TestRemoteOperation(Page|Event)' -count=1`，确认通过。

**Expected:** 同一 SQLite store 的操作与事件可稳定分页；未知 operation ID 不泄露是否属于其他 store；不增加 schema。

## Task 2: Downloader 本地只读 façade

**Files:** `internal/downloader/cloud_operations.go`, `internal/downloader/cloud_operations_test.go`

- [x] 写测试：未初始化 store 和 closed store 返回错误；可用 fake/test store 返回既有历史；方法不触发认证或 provider 请求。
- [x] 先运行定向测试确认失败。
- [x] 添加 `CloudOperationPage` 与 `CloudOperationEventPage` 薄封装，拒绝 invalid status/pagination 并委托 store。
- [x] 运行 `go test ./internal/downloader -run 'TestCloudOperation(Page|Event)' -count=1`。

**Expected:** 读取历史完全来自本地 store；无 `authorizedClient`、百度 API 请求或远端副作用。

## Task 3: 受保护 API 与敏感证据掩码

**Files:** `internal/intakeapi/server.go`, `internal/intakeapi/cloud_operations.go`, `internal/intakeapi/cloud_operations_test.go`, `main.go`

- [x] 扩展 `Backend`；fake backend 记录调用参数并提供 fixture。
- [x] 先写 API 回归：GET-only；status/offset/limit/operation ID 校验；分页输出；unknown 事件可读但无 action；敏感 JSON 键（token/secret/password/authorization）递归掩码；not-found 返回 404；读取不调用 `ListCloud`/`MutateCloud`。
- [x] 定向测试先按预期失败。
- [x] 实现 list/detail handler，返回 UTC 时间和稳定分页元数据；将 `/api/cloud/operations` 与 `/api/cloud/operations/{id}/events` 挂到现有管理认证 handler 中。
- [x] 运行 `go test ./internal/intakeapi ./internal/taskstore ./internal/downloader -run 'Test(RemoteOperation|CloudOperation)' -count=1`。

**Expected:** 两个 GET 端点只读、分页有界、操作详情只可查本地 store；错误不会把敏感字段或其它账号存在性暴露给浏览器。

## Task 4: 管理页历史列表和事件详情

**Files:** `internal/webui/index_cloud.html`, `internal/webui/res/cloud_operations.js`, `scripts/webui.test.cjs`

- [x] 增加折叠面板、状态筛选、列表分页按钮与事件详情容器；默认折叠且缓存模式也可查看本地操作历史。
- [x] 先加 WebUI 回归并运行，确认新控件/交互按预期失败。
- [x] 实现按需 GET 列表和事件；使用 `textContent`/HTML 转义，显示状态、operation ID、对象路径、发起/响应/本地记录 UTC 时间。unknown 文案明确“隔离，不可重发”；不出现操作按钮。
- [x] 覆盖敏感字段不回显、路径/证据文本 XSS 转义、分页及状态变化后回到第一页。
- [x] 运行 `node scripts/webui.test.cjs` 与 `node scripts/userscript.test.cjs`。

**Expected:** 只读视图可在本地运行时浏览历史；页面操作不会调用百度 provider 写接口。

## Task 5: 发布文档与 v0.8.30-preview.59

**Files:** 三份路线/进度文档及独立 releases `.59` 产物。

- [x] 将 `.59` 主题、目标和验收证据加入路线图；更新 WebUI/接口成功证据和外部限制。
- [x] 运行 `go test ./... -count=1`、`go vet ./...`、WebUI/userscript 测试及 Windows AMD64/Linux AMD64/Linux ARM64 打包门禁。
- [x] 使用既有 `scripts/package.ps1` 创建 `v0.8.30-preview.59`，不在 ZIP 中包含运行状态；验证 SHA256SUMS、源码快照和包内版本。
- [x] 按用户现有“只保留一份、随最新版运行”授权，将唯一 `.nas-downloader` 从 `.58` 移动到 `.59`；仅在 `.58` 空闲且目标不存在时操作。启动 `.59` 后验证操作历史 count、first-page read、UTC evidence、OAuth/health 和单副本路径。

**Expected:** `.59` 至少提供一个可独立使用的新功能；源码、包和真实本地只读 UI 链路一致；真实云端变更、重复扫描和删除不属于本批验证。

## Review Focus

- 状态分页必须是 SQLite 参数化查询且上限受限；稳定排序不能在相同毫秒/纳秒时间戳时跳项或重复。
- operation detail 必须限制在同一个 queue store，不能接受请求参数来切换账号或暴露 account fingerprint。
- sensitive evidence masking 必须覆盖对象嵌套、数组及大小写变化的敏感键。
- 页面文字从网盘路径和证据安全渲染；unknown 必须保持只读，不出现任何执行入口。
- GET 不触发百度请求、缓存刷新、队列恢复或日志变更。
