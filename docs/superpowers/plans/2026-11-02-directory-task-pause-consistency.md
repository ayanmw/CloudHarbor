# 目录下载任务暂停状态一致性实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 保持目录暂停/取消后的代状态一致，并在任务操作持久化失败时恢复旧状态。

**Architecture:** 在根目录队列尝试完成后，基于根云端路径协调内存后代视图，只修正与用户暂停/取消一致的中断项。Cancel/Retry 写入 SQLite 失败时恢复变更前的队列快照；取消事件补记已移除的重试截止时间。不更改持久化的子任务数据模型。

**Tech Stack:** Go、现有 scheduler、任务状态缓存、SQLite 队列记录。

**Spec:** `docs/superpowers/specs/2026-11-02-directory-task-pause-consistency.zh-CN.md`

## Global Constraints

- 不创建或使用 worktree；不提交、不推送。
- SQLite 中根队列任务是暂停/取消状态的持久权威记录；不得把已完成/真实失败子项伪装为中断。
- 不连接真实百度账号、不做网盘远端写操作、不宣称 NAS/SMB 验收。
- 新 preview 至少累计 5 条独立功能/改进/Bug 后才发布；未达门槛继续合并相邻改进。

## Review Focus

- 直接文件与嵌套目录取消：只有路径严格属于当前根目录时才修正。
- 已完成子项：不能从 done 降为 paused。
- 真实失败：错误不是 `context canceled` 时保留 failed 与原错误。
- 用户可见统计：改动状态后必须使统计缓存失效，清除误导性的错误与瞬时速度。
- 非暂停收尾：根任务最后不是 paused 时不执行后代协调。

---

## Task 1：回归证明目录暂停会误报子项失败

- [x] 在 `internal/downloader/task_pause_test.go` 构造暂停目录根任务、直接和嵌套取消子项、已完成子项、真实失败子项。
- [x] 调用 scheduler 完成路径并断言取消后代为 paused、done/真实失败不变、错误与速度清除。
- [x] 运行目标测试，确认它在旧实现上因取消后代仍为 failed 而失败。

## Task 2：在根任务暂停收尾中修正后代内存状态

- [x] 在 `internal/downloader/scheduler.go` 的持久化结果确认后，限定目录根且最终 paused 的情况。
- [x] 严格匹配根路径后代、failed 状态与精确 `context canceled` 错误；改为 paused 并清理错误/速度/完成时间。
- [x] 状态发生变化时更新任务统计 generation；保留 done、其他真实失败和非后代。
- [x] 运行目录暂停回归及既有单文件活动暂停/续传回归。

## Task 3：修正目录取消后的代状态

- [x] 回归覆盖用户取消已暂停目录时，后代不能继续显示 paused。
- [x] 回归覆盖活动目录取消时，子项在取消收尾后显示 cancelled。
- [x] 仅更新指定根路径下的中断后代，保留 done 与真实 failed 项。

## Task 4：使取消/重试写入失败回滚并补全取消时间证据

- [x] SQLite 关闭时取消失败回归：确认任务仍为原状态且不误报 cancelled。
- [x] SQLite 关闭时手动重试失败回归：保留原错误、尝试/失败计数、重试截止及容量证据。
- [x] `retry_wait` 任务取消事件增加原 `next_attempt` UTC 证据；验证精确到纳秒且不冒充云端时间。

## Task 5：路线图、验收记录与预览发布

- [x] 更新路线图、进度、版本记录与 `release/TESTING.md`，记录累计 5 项改进和验证边界。
- [x] 运行全仓测试、vet、WebUI/userscript 检查及三平台构建。
- [x] 发布 `.125`，核对 ZIP、载荷哈希、源码快照及运行数据排除，保存验证记录。
