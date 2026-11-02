# 全盘扫描完成后刷新不完整目录清单实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**目标：** 全盘扫描更新缓存后，使已打开的不完整目录清单自动反映最新状态。

**方案：** 在现有扫描状态轮询中按服务端 `run_id` 追踪已观察到的活动扫描；同一轮转为 `complete` 时，对已加载清单调用现有本地 API 一次。

**技术：** 内嵌 JavaScript、Node WebUI 回归测试、PowerShell 发布脚本。

**规格：** `docs/superpowers/specs/2026-11-02-full-scan-incomplete-list-refresh.zh-CN.md`

## 全局约束

- 在主 checkout 内联实现，不使用 worktree，不提交。
- 仅复用现有状态 API 和 SQLite 清单 API，不增加百度请求或 schema。
- `.137` 至少累计五项独立、可验证用户结果后才交付。

## 评审重点

- 页面初次读取到历史 `complete` 不误判为新完成。
- 同一轮 run 的状态轮询不会重复触发清单读取。
- 暂停/失败不触发，后续恢复完成能触发。
- 未加载清单时不增加请求；筛选与第一页语义保留。
- 本页与其他标签页发起的运行均能用同一 `run_id` 路径处理。

## 任务

- [x] 为扫描运行/完成、历史完成、失败/暂停、重复轮询和未加载清单写 UI 回归，观察预期失败。
- [x] 在 `internal/webui/index_cloud.html` 接入扫描 run 观察和完成收尾，并确保手动开始后快速完成也不会漏掉 run。
- [x] 运行 WebUI/userscript、全仓 Go 测试/vet、三平台构建及 ZIP/SHA/source snapshot 门禁。
- [x] 更新 `.137` 验证记录与版本路线图；将唯一真实账号运行目录移动到 `.137`，完成只读健康、队列和本地清单验收。
