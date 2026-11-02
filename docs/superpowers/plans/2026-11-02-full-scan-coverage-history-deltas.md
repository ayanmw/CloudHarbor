# 全盘扫描覆盖历史变化对比实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 展示每轮完成扫描与紧邻上一轮完成扫描之间可证明的覆盖变化。

**Architecture:** taskstore 在读取历史行时查找同账号紧邻的上一完成轮，并依据两个不可变快照构建比较 DTO。API 复用该 DTO，历史 UI 和单轮 JSON 导出显示绝对值、差值和双方观察 UTC；不增加 schema 或百度请求。

**Tech Stack:** Go、SQLite、HTTP JSON、现有原生 HTML/JavaScript WebUI。

**Spec:** `docs/superpowers/specs/2026-11-02-full-scan-coverage-history-deltas.zh-CN.md`

## Global Constraints

- 所有历史只按 SQLite 已记录值解释；NULL 永远不是零。
- 比较只针对同账号、sequence 紧邻的前一个 `complete` 运行；不跳过缺快照的相邻运行。
- 不新增百度请求、远端写入、表结构迁移或依赖。
- JSON DTO 不得暴露 `account_fingerprint`。
- 保留稳定游标、现有筛选条件和历史记录时间 UTC 语义。

## Review Focus

- 第一条完成扫描没有前序：返回 `no_previous_run`，差值为空。
- 相邻旧轮缺快照：返回 `previous_snapshot_unrecorded`，不得越过旧轮找更早完整快照。
- 当前轮快照字段缺失：返回 `current_snapshot_unrecorded`，不得使用当前缓存替代。
- 原因 JSON 缺失但覆盖摘要齐全：摘要变化可算，九项原因差值为空并标 `reasons_unrecorded`。
- 账号相同时间附近有多条扫描：按 sequence 而非 UTC 排序保证唯一相邻项。

---

### Task 1: 定义比较 DTO 并测试相邻快照规则

**Files:**
- Modify: `internal/taskstore/cloud_scan_history.go`
- Test: `internal/taskstore/cloud_scan_history_test.go`

**Interfaces:**
- Produces `CloudScanCoverageComparison`，含状态、前序 run ID/sequence、双方 UTC、双方覆盖快照、未完整目录差值、可空原因差值。
- Produces比较构造函数，输入当前与相邻前序 `CloudScanRun`，输出 DTO。

- [x] 写首轮、完整相邻、NULL 快照和原因缺失测试。
- [x] 运行定向测试确认预期失败。
- [x] 实现 DTO 与纯比较函数；差值固定为当前减前序。
- [x] 重跑定向测试。

### Task 2: 为历史列表与单轮导出附加同账号相邻比较

**Files:**
- Modify: `internal/taskstore/cloud_scan_history.go`
- Test: `internal/taskstore/cloud_scan_history_test.go`

- [x] 增加数据库测试，验证以前序 sequence 最大的 `complete` 记录匹配，不受页面筛选影响。
- [x] 增加隔离、相同 UTC 不同 sequence、旧 NULL 及失败/暂停轮跳过规则回归。
- [x] 先观察 RED，再实现查询和列表/导出装配。
- [x] 验证导出的 run 使用同一 DTO 且不包含账号指纹。

### Task 3: 展示覆盖历史比较与单轮证据导出

**Files:**
- Modify: `internal/webui/index_cloud.html`
- Test: `scripts/webui.test.cjs`
- Modify: `docs/ai/cloud-management-roadmap.zh-CN.md`
- Modify: `docs/ai/progress.md`
- Modify: `docs/ai/release-versioning.zh-CN.md`

- [x] 前端回归覆盖首轮、改善/恶化、快照未记录和原因未记录文案。
- [x] 仅在测试失败原因确认为显示尚未实现后加入历史卡片渲染；显示两边 run、观察 UTC、状态/未完整数及九项原因差值。
- [x] 确认下载 JSON 与页面使用相同 DTO 字段；不显示当前缓存推算值。
- [x] 更新路线图为未编号候选 1/5，交付时补齐版本与逐项证据。

### Task 4: 全量验证和 `.142` 预览包

**Files:**
- Modify: `docs/ai/progress.md`, 路线图、版本管理记录
- Create: releases 中 `.142-validation.md` 及包

- [x] 跑 taskstore/intakeapi 定向测试、WebUI、userscript、`go test ./... -count=1`、`go vet ./...`。
- [x] 运行正式 `scripts/package.ps1`，确认 Windows AMD64、Linux AMD64、Linux ARM64、ZIP 条目和载荷哈希。
- [x] 将唯一 `.nas-downloader` 在停止服务后从 `.141` 移到 `.142`，逐项 SHA-256 核对后启动新版；绝不复制。
- [x] 本地只读核验 health、版本、SQLite integrity、OAuth/账号状态、任务队列及页面 HTTP；不得启动全盘扫描或执行网盘写入。
- [x] 记录源快照、ZIP SHA、运行证据、外部未验收边界；最终检查仅含本任务必要差异。

## Scope self-review

Spec 中历史相邻定义、NULL 不推断、原因差值可空、列表和导出一致、UTC 证据、账号隔离和不增加远端请求均由 Task 1–3 覆盖。Task 2 的数据层测试覆盖 sequence 相邻与帐号边界；Task 3 覆盖显示与脱敏 DTO 接线；Task 4 是交付门禁。无 SQLite 迁移。
