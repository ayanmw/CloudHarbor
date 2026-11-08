# 入队到下载任务关联 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans inline.

**Goal:** 入队详情能打开具有持久化批次来源证据的下载根任务。
**Architecture:** queueJob 新增可选 intake_id，创建根任务时同事务保存；GetIntakes 仅对 moved 且批次与对象身份相符的任务生成当前读取快照。历史无来源证据不回填。
**Tech Stack:** Go、Vue、现有 provider mock/JSDOM。
**Spec:** docs/ai/netdisk-product-roadmap.zh-CN.md P0-B。

## Global Constraints
固定 v0.8.31，不提交、不使用 worktree、不操作真实账号；不把入队登记视为监控发现。

## Review Focus
重启来源保留；同对象旧任务不串批次；隔离项不显示关联；十六位以上对象 ID 不转 JS number；详情状态是读取快照。

### Task 1
- [x] 模拟提交后历史返回关联及跨重启测试，先观察失败。
- [x] 持久化根任务来源，历史读取精确匹配生成 task_id/status/observed_at_utc；旧任务不推断。
- [x] 针对性 Go 测试。

### Task 2
- [x] 新可选字段校验、任务链接及旧记录说明，UI 回归。
- [x] 类型检查/构建、前端完整测试、独立审查；更新版本说明。

## Ledger
本阶段关联根任务，不虚构监控运行 ID 或云端 operation；监控运行证据尚需后续接通。

2026-11-05：新增测试先观察无关联失败；针对性 downloader/intakeapi 测试通过；36 项入队 UI、全量 351 项前端、类型检查与构建通过。覆盖跨重启、无来源旧任务、其他批次、隔离项目、大整数 ID 及错关联拒绝。独立只读审查无阻断。尚未发布，真实账号与运行文件未触碰。
