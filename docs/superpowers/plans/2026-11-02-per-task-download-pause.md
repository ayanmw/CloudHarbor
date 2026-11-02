# 单个下载任务暂停与继续实施计划

> 主目录内联实施；保留现有 dirty/untracked 内容；不使用 worktree，不提交或推送。
> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在全局队列暂停之外，允许安全暂停并继续单个下载任务，跨重启保留用户选择。

**Architecture:** SQLite 队列 JSON 保存 paused 状态与原调度状态；暂停先耐久记录，再取消活动任务 context。完成回调仅在持久暂停意图存在时保留 paused；恢复重新进入现有 scheduler 和完整下载校验链路。

**Tech Stack:** Go、SQLite taskstore、嵌入式 Poll/WebSocket WebUI、Node `node:test`。

**Spec:** `docs/superpowers/specs/2026-11-02-per-task-download-pause.zh-CN.md`

## Global Constraints

- 每个 preview 至少包含 5 条可独立验收的功能/改进/Bug 修复；相近行为合并到 `.124`。
- 暂停状态与事件写入当前 SQLite；不创建新运行数据文件。
- unknown/confirmed 百度后处理状态不能被任务暂停/恢复覆盖；不自动重发网盘写操作。
- 保留全局暂停、maintenance、auth 和目录扫描的既有独立语义。
- 本次不使用 worktree，不提交或推送；真实账号/NAS 验收与本机证据分开。

## Review Focus

- SQLite 写入失败：不能显示暂停成功；队列沿用既有持久化故障 fail-closed 规则。
- cancel 与源操作发送竞态：已进入 source operation 的任务不可暂停。
- 下载任务取消回调尚未完成：不能接受恢复并创建并发尝试。
- 进程在暂停后、回调持久化前崩溃：重启后仍 paused，恢复后可续传。
- retry_wait 暂停：保留原 UTC deadline，不因暂停重置 provider retry budget。

## Task 1：耐久单任务状态转换

**Files:** `internal/downloader/scheduler.go`、`downloader.go`、新 `task_pause.go`、新 `task_pause_test.go`。

- [x] 写 RED：暂停 pending/retry_wait 幂等持久化、暂停失败原子、重开恢复、resume 恢复重试 deadline。
- [x] 实现 `Downloader.PauseTask(id int64)` / `ResumeTask(id int64)` 与 durable `paused_from_status`。
- [x] 暂停持久化后才取消 active；拒绝 source state 非空和 active 尚未结束时恢复。
- [x] 测试全局暂停/maintenance 独立，暂停期间扫描仍能登记其他任务。

## Task 2：活动传输中断与安全续传

**Files:** `internal/downloader/scheduler.go`、`transfer.go`、`downloader.go`、相关测试。

- [x] RED：从真实本地 mock Range 下载中暂停，验证 active 收尾、任务留 paused、partial 保留且其他工作可继续。
- [x] 修改 complete 仅对已持久化 paused 意图不计失败并保持 paused；记录 pause completion 事件。
- [x] 恢复后让 scheduler 正常领取，从已校验 partial Range 续传；错误/损坏 partial 仍走原校验策略。
- [x] 测试 cancel 与 source operation `sending` 顺序竞态，禁止覆盖 unknown/confirmed。

## Task 3：管理员 API 与状态 DTO

**Files:** `main.go`、`main_test.go`、`internal/webui/webui.go`、DTO 测试。

- [x] RED：认证、方法、404/409/503、重复请求、Poll/WebSocket 同步的状态与能力字段。
- [x] 加入 POST pause/resume handler；失败返回错误，不猜测状态。
- [x] `CanPause`/`CanResume` 从持久 job 与 active/source operation 状态推导。

## Task 4：任务页控制

**Files:** `internal/webui/res/shared.js`、`index_poll.html`、`index_ws.html`、`res/shared.css`、`scripts/webui.test.cjs`。

- [x] RED：两种页面显示 paused 状态和按钮；仅点击时请求；失败不乐观改状态。
- [x] 复用任务 action busy/auth 过期模式；暂停正在收尾时给出刷新后继续提示。
- [x] Poll/WebSocket 渲染和排序刷新不因迟到响应复活旧状态。

## Task 5：`.124` 发布与路线图

- [x] 记录 `.124` 五条独立结果、设计、计划与版本验收说明。
- [x] 全仓 Go/vet、WebUI/userscript、三平台发布构建。
- [x] 独立核验 ZIP、所有 payload hash、源码快照、运行数据排除与 staging 清理。
- [x] 记录本机/SQLite/mock 与真实百度/NAS/SMB 的验证边界。
