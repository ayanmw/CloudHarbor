# 上传恢复历史网页入口 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline.

**Goal:** 批次详情可按需分页核对真实重启恢复记录。
**Architecture:** 从既有 events/所属批次 items 只读查询恢复事件，解析已定义机器格式；独立 GET endpoint、字符串序号游标、最多100项。不随2秒进度轮询重复扫描事件；前端展开才读取，离页/换批次取消迟到结果。
**Tech Stack:** Go SQLite/http、Vue、node:test/JSDOM。
**Spec:** docs/ai/netdisk-product-roadmap.zh-CN.md P0-C。

## Global Constraints
固定v0.8.31；原目录，不提交、不worktree，不操作真实账号；不增加数据库结构，不重试未知提交，不把历史状态冒充当前状态。

## Review Focus
批次隔离；游标精确/稳定；无记录旧批次；坏证据显式失败；换批次及迟到401不得串账号页面。

### Task 1
- [x] API分页/跨批次/只读测试先失败，再实现读取与路由。
- [x] uploads与主路由测试。

### Task 2
- [x] 懒加载恢复历史表，完整时间/原阶段/恢复动作/分片量，成功翻页才提交游标。
- [x] UI定向/全量、类型构建、审查，版本说明；不发布。

## Ledger
此入口展示已存证据，不补造旧恢复记录；速度ETA及分片会话恢复仍需后续实施。

2026-11-05：先观察缺HandleRecovery编译失败，再实现；大序号fixture重复sqlite_sequence行曾失败，改UPDATE/必要时INSERT后通过。上传模块/主程序完整Go、uploads vet通过；主路由增加未授权401断言；空历史/坏证据、跨批次、稳定大序号游标与非法参数均验证。审查发现BFcache迟到401风险，已补pagehide失效/pageshow显式恢复并回归，复审无阻断。上传相关20/20、完整前端361/361、Vue类型/构建、diff检查通过。本轮尚未可见浏览器/真实账号验收或发布。
