# 局部缓存刷新完成后的浏览上下文一致性实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**目标：** 刷新完成时只重读受影响的本地缓存视图，不触发意外的百度读取或覆盖无关导航。

**方案：** 在网盘管理页跟踪当前刷新运行的根目录；完成轮询只针对本地缓存目录/搜索做有界刷新，且沿用现有 browse 请求代次保护。服务端刷新协议和 SQLite 不变。

**技术：** Go 服务现有 API、内嵌 HTML/JavaScript、Node WebUI 测试。

**规格：** `docs/superpowers/specs/2026-11-02-refresh-completion-browse-context.zh-CN.md`

## 约束

- 不使用 worktree，不覆盖或清理本轮以外的工作区改动。
- 不新增百度请求；完成通知不得触发 `/api/cloud`。
- 仅匹配本页关联的 `run_id` 与根路径。
- 预览版需累计至少五项独立可验收结果后才发布。

## 任务

- [x] 在 `scripts/webui.test.cjs` 为完成刷新后的实时视图、相关缓存目录、无关缓存目录、缓存搜索与恢复刷新范围编写回归用例；先观察到原实现未重读已完成刷新的缓存目录。
- [x] 在 `internal/webui/index_cloud.html` 保存当前刷新根目录，并在完成时根据数据源/目录范围决定是否从第一页刷新缓存视图。
- [x] 运行定向 WebUI 测试，刷新相关 14/14 通过。
- [x] 运行 WebUI 全套测试和全仓 Go 测试/vet；WebUI 147/147、Go 全仓与 vet 通过，确认未更改 API 或后端协议。
- [x] 更新路线图与版本记录；发布门禁、三平台打包、ZIP 12 项载荷哈希、454 项源码快照和真实账号只读运行验收均完成。

## 验收重点

- 确认实时浏览器完成刷新时没有 `/api/cloud` 或目录大小读取请求。
- 确认缓存页只在目录属于刷新子树时更新；缓存搜索保留查询并从第一页更新。
- 确认晚到的状态轮询、失败状态和未完成运行不会改变当前浏览上下文。
