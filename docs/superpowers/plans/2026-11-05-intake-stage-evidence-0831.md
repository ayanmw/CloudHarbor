# 下载入队阶段时间证据 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline.

**Goal:** 为下载闭环补齐同步移动回执与本地根任务登记的独立 UTC 时间，供后续任务关联使用。
**Architecture:** 扩展既有原子入队 sidecar 的可选字段；不补造历史时间，不改变隔离或发送策略。前端在现有详情逐项展示证据。
**Tech Stack:** Go、Vue、TypeScript、现有模拟 provider 与 JSDOM。
**Spec:** docs/ai/netdisk-product-roadmap.zh-CN.md P0-B。

## Global Constraints
固定 v0.8.31；原目录实现，不提交、不使用 worktree；不访问真实账号；登记不是监控发现或已开始下载。

## Review Focus
旧记录缺字段可读；重启保持原始时间；重核对不伪造同步回执；队列登记失败不显示成功；非法时间响应拒绝。

### Task 1: 持久化阶段证据
- [x] 添加模拟提交/重启时间保持测试并观察失败。
- [x] 移动同步响应成功时记录 sync_confirmed_at_utc；登记成功时记录 queue_registered_at_utc，首次值不覆盖。
- [x] 运行入队相关 Go 测试。

### Task 2: 详情展示与兼容
- [x] 可选时间字段校验、逐项证据展示，旧记录显示未记录；明确尚不能证明下载开始。
- [x] UI 测试覆盖新旧记录和非法字段，构建及前端检查。
- [x] 更新版本说明；完整发布门禁前不覆盖运行程序。

## Ledger
2026-11-05：新增测试先观察缺字段失败，再实现；入队相关 Go 测试通过，入队 UI 35/35、全量前端 350/350、Vue 类型检查与构建通过。独立只读审查无阻断项；未发布、未启动真实服务。
2026-11-05：本阶段建立关联的时间基础，后续仍须实现根任务/监控实际关联；不将本阶段描述为下载闭环全部完成。
