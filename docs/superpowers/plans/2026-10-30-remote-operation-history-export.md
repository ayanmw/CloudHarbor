# 远端操作历史摘要导出 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将当前筛选命中的远端操作摘要从 SQLite 一致快照导出为有界 JSON 下载。

**Architecture:** Store 使用单个 SQLite 只读事务对现有过滤器计数并读取稳定排序结果；Downloader 透传上下文；管理 API 返回附件 JSON，网页用当前筛选生成下载 URL。

**Tech Stack:** Go、modernc SQLite、net/http、内嵌 HTML/JavaScript、Node `node:test`。

**Spec:** `docs/superpowers/specs/2026-10-30-remote-operation-history-export.zh-CN.md`

## Global Constraints

- 只读取当前账户 SQLite，不访问百度 API，不修改队列、缓存或远端状态。
- 每次最多 5,000 条；超过上限必须拒绝整个导出，不产生部分文件。
- 使用 `.60` 已验证的同一 status/query/from/to 过滤语义及 UTC 转换。
- JSON 不包含 account fingerprint 或 event evidence；unknown 不提供操作入口。
- 不改 schema，不添加依赖，不使用 worktree，保留当前 dirty main 的其它改动。

## Review Focus

- 计数和读取必须在同一事务快照；并发新增不可造成总数/导出内容不一致；Store 测试验证事务内快照结果。
- 超过上限不返回已读取的前 5,000 条；Store/API 测试验证全有或全无。
- JSON 与下载头不得包含 account fingerprint、evidence 或 Cookie；API 测试验证字段集合和脱敏边界。
- 筛选导出与列表使用完全相同的解析规则；API/UI 测试验证 q、status、from、to 一致。
- 同源认证 GET 必须携带当前管理会话 Cookie 且不添加分页参数；WebUI 测试验证筛选、Blob 下载和服务端错误提示。

---

### Task 1: Store 快照导出

**Files:** `internal/taskstore/remote_operations.go`, `internal/taskstore/remote_operations_test.go`

**Interfaces:** `RemoteOperationExportLimit = 5000`；`ErrRemoteOperationExportTooLarge`；`RemoteOperationExport(ctx context.Context, filter RemoteOperationFilter) ([]RemoteOperationSummary, error)`。

- [x] 先写失败测试：混合状态/路径/时间数据只返回正确摘要，字段无 account fingerprint/evidence。
- [x] 运行定向测试确认导出方法不存在时按预期失败。
- [x] 实现事务内共享过滤条件、先计数后上限判定、稳定倒序读取摘要。
- [x] 写失败测试：5,001 条返回超限错误且无部分数据；取消 context/closed store 返回错误。
- [x] 运行 `go test ./internal/taskstore -run TestRemoteOperationExport -count=1`。

### Task 2: Downloader 与受保护 JSON 下载 API

**Files:** `internal/downloader/cloud_operations.go`, `internal/downloader/cloud_operations_test.go`, `internal/intakeapi/server.go`, `internal/intakeapi/server_test.go`, `internal/intakeapi/cloud_operations.go`, `internal/intakeapi/cloud_operations_test.go`, `main.go`

- [x] 先写 API 回归：复用 `.60` 过滤参数、GET-only、attachment JSON headers、超限 413、无 fingerprint/evidence/provider calls。
- [x] 运行定向测试确认 `/export` 路由/接口缺失时失败。
- [x] 增加 Downloader 导出 façade 与 Backend 接口。
- [x] 注册受管理认证保护的 GET 下载 handler；固定超限错误转换为 413，其他错误按不可用处理。
- [x] 运行 Store/downloader/API 相关 Go 包测试。

### Task 3: 历史页导出入口

**Files:** `internal/webui/index_cloud.html`, `internal/webui/res/cloud_operations.js`, `scripts/webui.test.cjs`, `release/TESTING.md`

- [x] 先写 WebUI 回归：导出 URL 含当前 q/status/from/to、无 offset/limit，点击不发列表/百度/变更请求。
- [x] 运行定向测试确认导出按钮未接线时失败。
- [x] 增加“导出筛选摘要 JSON”按钮，校验筛选输入后通过同源 GET 下载附件；HTTP 错误显示在历史区，成功后显示文件名。
- [x] 更新中文测试指南，说明摘要字段、5,000 条上限及 evidence 仍需逐条查看。
- [x] 运行 WebUI 29/29；userscript 与全仓验证待发布阶段完成。

### Task 4: 发布与真实只读验收

**Files:** `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/progress.md`, `docs/ai/release-versioning.zh-CN.md`, `.61` 独立 release。

- [x] 更新 `.61` 路线图与外部验收限制。
- [x] 运行全仓 Go 测试、`go vet ./...`、WebUI 29 项/userscript 3 项与 Windows AMD64/Linux AMD64/Linux ARM64 打包门禁。
- [x] 生成 `.61` 包，验证 ZIP 哈希、12/12 清单、274 项源码快照、私有文件排除和包内版本。
- [x] 升级后对真实账号执行受保护 GET 导出，核对 19 条全量摘要、精确筛选 1 条、UTC 生成时间和无敏感字段；不执行远端写操作。
