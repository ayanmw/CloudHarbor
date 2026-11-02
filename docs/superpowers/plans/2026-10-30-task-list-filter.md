# 下载任务列表筛选和搜索实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给两个管理页面增加服务器端任务状态筛选和云端路径/文件名搜索。

**Architecture:** Downloader 在现有有界分页器中应用过滤，并返回命中数及全队列数；HTTP 和 WebSocket 共用验证规则；页面条件在排序、分页和刷新期间保持。

**Tech Stack:** Go、嵌入式 JavaScript、Node `node:test`。

**Spec:** `docs/superpowers/specs/2026-10-30-task-list-filter.zh-CN.md`

## 全局约束

- 保持现有状态统计卡片为未筛选的全队列统计。
- 筛选结果继续服务器端分页，不能将整个队列发送到浏览器。
- 搜索只匹配云端路径和文件名；按 Unicode 码点限制为 128，字面子串、不区分大小写。
- 无筛选使用现有高效分页实现；不更改持久化、远端 API 或任务操作。
- 不创建 worktree、不提交；保护现有未提交工作。

## 重点风险与测试

- 非 ASCII 名称和大小写：Downloader 搜索回归。
- 空查询且未知状态：正常全列表或 400，API 校验回归。
- 过滤后排序与 offset 边界：Downloader 分页回归。
- 搜索状态变更时 WebSocket 快速旧页响应：页面回归确保始终用最新条件。
- 全局统计被筛选污染：Downloader/API/UI 回归。

### Task 1：Downloader 有界过滤分页

**文件：** 修改 `internal/downloader/downloader.go`、`internal/downloader/task_page_test.go`。

- [x] 先写 RED 回归覆盖状态+路径/文件名组合、大小写、空格、分页和全局统计。
- [x] 运行定向测试确认失败。
- [x] 实现规范化查询/状态验证与 `GetTaskPageFiltered`；空筛选委托给原实现，过滤路径用有界堆选择 `offset+limit` 项。
- [x] 运行 Downloader 定向测试。

### Task 2：HTTP 与 WebSocket 筛选接口

**文件：** 修改 `main.go`、`main_test.go`、`internal/webui/webui.go`、对应测试。

- [x] 先写 HTTP 和 WebSocket 筛选参数/错误验证测试。
- [x] 复用 downloader 校验规则，传入同一分页/排序/筛选语义；响应同时给命中总数与全队列总数。
- [x] 运行 API/WebSocket 定向测试。

### Task 3：WebSocket 和轮询页面交互

**文件：** 修改 `internal/webui/index_ws.html`、`internal/webui/index_poll.html`、`scripts/webui.test.cjs`。

- [x] 先写两种页面的状态/搜索、重置 offset、分页保留条件、自动刷新保留条件和过期响应测试。
- [x] 增加控件并将同一状态/查询发送至 API；输入后显式搜索、Enter 提交、清除按钮重置。
- [x] 运行 WebUI 与 userscript 测试。

### Task 4：`.76` 发布

**文件：** 更新 `docs/ai/progress.md`、`docs/ai/cloud-management-roadmap.zh-CN.md`、`docs/ai/release-versioning.zh-CN.md`。

- [x] 记录主题、功能范围、外部验收边界和最终校验值。
- [x] 全仓测试、vet、UI 测试、三平台构建与 ZIP 检查；包内版本和源码快照与打包时工作区一致。

`.76` 完成：`go test ./... -count=1`、`go vet ./...`、WebUI 47/47、userscript 3/3 通过；三平台发布包、ZIP 清单、12/12 SHA-256、304 项源码快照及包内版本/架构核对通过。归档 SHA-256 为 `743C64CC3B78CB6F6CD294026A09C7F2DB634E161D47316DDA6B78E13A728B25`。未部署或操作真实网盘。
