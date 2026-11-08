# 入队项目监控观察证据 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline.

**Goal:** 入队根任务可追到首次可信监控观察，而非推断扫描成功等同项目发现。
**Architecture:** 在真实监控 reconcile 路径传入 run_id，仅对同身份且有 intake 来源的根任务保存首次观察路径、UTC、运行ID及事件；历史查询复用精确批次关联返回证据。普通 reconcile 不生成虚假运行关联。
**Tech Stack:** Go/SQLite job JSON、Vue、mock provider、JSDOM。
**Spec:** docs/ai/netdisk-product-roadmap.zh-CN.md P0-B。

## Global Constraints
固定 v0.8.31；不提交、不使用 worktree、不访问真实账号；观察项目不代表下载开始，也不保证整轮扫描最终成功。

## Review Focus
跨重启；后续扫描保留首次；未观察不伪造；只关联身份一致来源批次；保存失败停止调度并不发布虚假观察。

### Task 1
- [x] 先写模拟扫描/重启/首次证据不覆盖测试，观察失败。
- [x] 在现有持久化任务事件路径保存首次监控证据，历史返回。
- [x] 相关 Go 测试。

### Task 2
- [x] UI 显示历史观察及监控详情链接、未记录说明，严格可选字段校验。
- [x] UI/类型/构建和回归、独立审查、版本说明；整包门禁前不覆盖运行文件。

## Ledger
扫描观察只表示该项目在特定列表中被核对到，不声称运行成功或当前仍在线。

2026-11-05：定向 Go、37 项入队 UI、352 项全量前端、类型检查/构建和独立只读审查通过；新增关闭测试数据库后的保存失败回滚/停止调度测试通过。完整 downloader/intakeapi/taskstore 回归仍由原 session 18771 运行，未重启；日志位于 releases/v0.8.31-intake-monitor-go.log。未发布，真实服务未启动。

完整回归收尾：session 18771 正常退出0；downloader 418.608s、intakeapi 5.656s、taskstore 309.520s 全部通过。该回归覆盖监控证据阶段源码；后续等待原因变更另有定向测试，不混称同一快照。
