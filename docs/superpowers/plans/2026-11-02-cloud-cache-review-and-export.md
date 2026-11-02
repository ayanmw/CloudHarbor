# 网盘缓存覆盖复核与证据导出实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**目标：** 让用户能快速定位缓存覆盖问题、下载一致且脱敏的清单，并让恢复活动页追上暂停期间完成的缓存扫描。

**方案：** 复用扫描覆盖原因分布和现有 generation 游标 API；导出只在用户点击时分页读取，显式验证所有页面属于同一 SQLite generation。

**技术：** 内嵌 JavaScript、Node WebUI 回归测试；不新增 Go API 或 SQLite schema。

**规格：** `docs/superpowers/specs/2026-11-02-cloud-cache-review-and-export.zh-CN.md`

## 全局约束

- 使用主 checkout，不创建 worktree、不提交。
- 现有百度 Provider 请求次数必须保持为零。
- 导出全量上限 10,000 条；失败不得下载部分结果或泄露账号凭据。
- `.138` 发布前至少累计 5 条独立用户结果。

## 评审重点

- 原因计数与路径筛选值的映射必须准确且对路径输入安全转义。
- 页级导出不能误称全量导出。
- 全量导出逐页核对 generation 和 total，冲突即中止且不点击下载。
- 手动过滤、扫描完成自动刷新和导出进行中切换筛选不能串用不同快照。
- 页面恢复时的 generation 追赶不能在扫描仍运行时反复读清单。

## 任务

- [x] 为原因计数/快捷筛选、当前页/全量导出、generation 冲突、上限、脱敏与休眠恢复追赶编写 UI 测试，观察失败。
- [x] 在 `internal/webui/index_cloud.html` 实现计数快捷筛选、快照导出和恢复后的 generation 对账。
- [x] 运行 WebUI 161/161、userscript 3/3、全仓 Go 测试/vet、三平台构建与 ZIP/SHA/source snapshot 检查。
- [x] 更新 `.138` 进度和验证记录，沿用单份真实账号目录并只读验收；没有启动百度扫描或远端写操作。
