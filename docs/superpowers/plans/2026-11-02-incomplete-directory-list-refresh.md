# 不完整目录清单自动更新实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**目标：** 在本页可确认缓存更新后刷新已打开的不完整目录清单，避免旧结果误导用户。

**方案：** 复用现有局部刷新完成 run 绑定和清单加载状态；刷新完成或远端变更成功后，从第一页调用既有本地清单 API。无服务端或数据库变更。

**技术：** 内嵌 JavaScript、Node WebUI 回归测试、PowerShell 发布门禁。

**规格：** `docs/superpowers/specs/2026-11-02-incomplete-directory-list-refresh.zh-CN.md`

## 全局约束

- 使用主 checkout；不创建或使用 worktree。
- 保留既有工作区改动和真实账号运行数据。
- 清单自动重读不得触发百度 API。
- 发布前版本批次累计至少五条独立功能/改进/缺陷修复结果。

## 评审重点

- 完成状态的 2 秒轮询不能重复发起清单读取。
- 刷新须保留用户筛选并复位分页游标/generation。
- 远端操作失败不能被当作缓存更新。
- 首次加载与未加载清单时不增加无用请求。
- generation 冲突仍通过现有逻辑回到新快照第一页。

## 任务

- [x] 在 `scripts/webui.test.cjs` 增加局部刷新完成及远端变更成功/失败回归，先确认失败，再实现。
- [x] 在 `internal/webui/index_cloud.html` 接入清单刷新，并保留现有视图与认证行为。
- [x] 运行 WebUI 152/152、userscript 3/3、全仓 Go 测试/vet、Windows AMD64/Linux AMD64/Linux ARM64 发布构建及 ZIP/SHA/source snapshot 检查。
- [x] 写入 validation 记录并更新路线图与版本进度；将唯一真实 runtime 移至 `.136`，只读验收健康、登录、队列、本地清单与页面。
