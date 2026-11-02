# 队列 SQLite 写失败状态投影一致性实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** SQLite 写失败时，任务页内存状态保持为最近一次成功提交的队列记录。

**Architecture:** 在五个先改内存后持久化的队列状态路径上保存最小必要的旧字段快照。SQLite 写失败仍触发现有 fail-closed；只回滚未提交的当前 job，不回滚已成功提交的其它行或远端状态。

**Tech Stack:** Go scheduler、SQLite task store、现有 event journal。

**Spec:** `docs/superpowers/specs/2026-11-02-queue-persistence-failure-projection.zh-CN.md`

## Global Constraints

- 不创建或使用 worktree；不提交、不推送。
- SQLite 最后成功记录是重启恢复时的权威任务状态。
- 队列保存故障后继续 fail-closed，不自动重新请求百度或重试任务。
- `.126` 包含五个彼此可验收的队列状态转换修复后才能发布；失败则留在 `.126` 内修正。

## Review Focus

- Claim 失败：attempt id、计数和容量状态不能部分更新。
- 授权等待：只回滚失败写入的当前 job，之前成功提交的 job 不撤销。
- 云端身份更新：完整恢复旧 FileInfo 和被身份切换分支改动的调度字段。
- 授权恢复：失败时保留 auth_wait 和原错误，不显示 pending。
- 完成事件失败：不覆盖已持久化的 paused/cancelled/auth_wait/source 状态。

---

## Task 1：领取任务事件写失败回滚

- [x] 先添加关闭 SQLite 后 claim 的回归并确认旧实现失败。
- [x] 恢复领取前状态、Attempts/AttemptID、重试 deadline、Error、LocalStorage 和 Updated。
- [x] 断言队列 fail-closed 且重新打开 SQLite 后任务仍是原状态。

## Task 2：授权等待事件写失败回滚

- [x] retry_wait 回归确认旧实现会留下未提交 auth_wait。
- [x] 仅失败 job 恢复旧 Status/Error/NextAttempt/Updated，保留 fail-closed。

## Task 3：成功扫描身份更新写失败回滚

- [x] 使用同 fs_id 同内容不同路径触发身份 rebase，SQLite 关闭后确认旧实现保留新路径。
- [x] 恢复 FileInfo/Status/SourceState/Error/NextAttempt/Updated。

## Task 4：授权恢复事件写失败回滚

- [x] auth_wait 任务经成功扫描后触发恢复事件失败，确认其仍为 auth_wait 且保留原错误/时间。

## Task 5：活动尝试收尾失败不覆盖已持久状态

- [x] 先持久化单项 paused，再注入 attempt-finished 保存失败；确认旧实现把它覆盖为 failed。
- [x] 保持 paused/cancelled/auth_wait 和 confirmed/unknown source quarantine 的已提交状态。
- [x] 跑全套发布门禁，更新路线图/进度/测试文档并打包 `.126`。
