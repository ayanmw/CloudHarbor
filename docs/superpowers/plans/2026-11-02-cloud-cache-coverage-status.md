# 全盘扫描与当前缓存覆盖状态分开展示实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**目标：** 将扫描运行是否结束与当前缓存是否完整分开计算、返回和展示。

**方案：** 增加 SQLite 同一只读事务的扫描运行/缓存覆盖摘要读取；扫描状态 API 兼容添加嵌套字段，网盘管理页分别显示运行与覆盖。搜索覆盖沿用同一计算规则，不改变扫描与失效行为。

**技术：** Go SQLite store/downloader/API，嵌入式 HTML/JavaScript，Node WebUI 测试。

**规格：** `docs/superpowers/specs/2026-11-02-cloud-cache-coverage-status.zh-CN.md`

## 约束

- 在主 checkout 内联实施；不使用 worktree，不覆盖无关脏改动。
- 任何状态接口和页面读取不得因状态展示额外访问百度或自动启动扫描。
- API 现有扫描顶层字段保持兼容；覆盖数据读取失败不能默认标记完整。
- 本版本需累计五项不同结果后再交付 `.133`。

## 任务

- [x] 在 Store、Downloader、API 与 WebUI 回归中覆盖 complete、stale/incomplete、running、unknown、账号隔离和读取失败；先观察到 Store 接口缺失、旧 UI 没有覆盖摘要及错误时仍显示旧摘要。
- [x] 用同一 SQLite 只读事务读取扫描检查点、未完整目录计数、缓存 generation 与扫描状态更新时间；搜索与状态页面复用同一覆盖计算。
- [x] 将嵌套覆盖摘要接入 Downloader 和扫描状态 GET；SQLite 读取失败通过既有 API 错误映射失败关闭。
- [x] 增加 API 与 WebUI 回归，页面并列显示扫描状态和当前覆盖、未完成数量、generation 与更新时间，并标记上次成功读取。
- [x] 运行全量 Go/WebUI/userscript/vet/跨平台打包门禁，更新路线图并完成真实账号只读验收。

## 验收重点

- 扫描完成但 root/其他目录为 stale 时，不能显示缓存覆盖完整。
- 同一快照的 generation、coverage count、scan status 与时间字段一致。
- 只打开页面或轮询状态不会请求百度、启动刷新或改变缓存。
