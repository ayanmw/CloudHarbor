# 全盘扫描覆盖历史筛选实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让用户按历史完成时的覆盖状态、未完整目录数和覆盖原因筛选扫描历史，并在分页时保留条件。

**Architecture:** 扩展现有 SQLite `CloudScanRunFilter` 查询条件、历史 API 校验和管理页表单；沿用 sequence 游标、历史 DTO 与现有页面请求流，不添加 schema 或新请求路径。

**Tech Stack:** Go、SQLite、HTML/JavaScript、Node `node:test`。

**Spec:** `docs/superpowers/specs/2026-11-02-full-scan-coverage-history-filters.zh-CN.md`

## Global Constraints

- 仅本地 SQLite 只读查询，不增加百度 Provider 请求。
- 旧历史覆盖快照/原因保持 NULL，不从当前缓存推断过去。
- 不新增数据库迁移、索引、API 路由或外部依赖。
- 不创建或使用 worktree，不提交 Git；保留其它未提交改动。
- Preview 需累计至少 5 个独立用户结果。

## Review Focus

- 旧记录缺少覆盖状态或原因快照时必须分开筛选，不可当成零或互相替代。
- 覆盖状态必须限制到受支持的固定值，不能拼接用户输入 SQL。
- 原因名必须白名单化，JSON 路径只能来自固定映射。
- 数值边界需拒绝负数、过大值和 min > max。
- 筛选更改和清除必须重置序列游标；后续分页保留当前筛选。

## Tasks

### Task 1: Store 历史过滤

**Files:** Modify `internal/taskstore/cloud_scan_history.go`; Test `internal/taskstore/cloud_scan_history_test.go`.

- [x] 写 RED：按 coverage status、min/max incomplete count、reason 过滤；验证 NULL 旧值、零计数和稳定分页。
- [x] 定向运行测试确认失败。
- [x] 扩展 `CloudScanRunFilter` 与 SQL 参数化谓词；reason 映射到固定 JSON 列名。
- [x] 运行 taskstore 定向回归。

### Task 2: API 参数验证

**Files:** Modify `internal/intakeapi/server.go`; Test `internal/intakeapi/server_test.go`.

- [x] 写 RED：支持状态、范围和原因参数组合；非法值、超界/倒置范围与重复参数返回 400。
- [x] 定向运行测试确认失败。
- [x] 解析并校验固定筛选字段，传给 Store，保持响应白名单和游标格式。
- [x] 运行 intakeapi 定向回归。

### Task 3: 网页筛选和分页

**Files:** Modify `internal/webui/index_cloud.html`; Test `scripts/webui.test.cjs`.

- [x] 写 RED：覆盖状态、目录数范围和原因筛选传到 API；旧历史选项可筛；分页保留筛选；清除返回第一页。
- [ ] 定向运行 WebUI 测试确认失败。
- [x] 增加表单控件并接入现有 filter state。
- [x] 运行完整 WebUI 测试。

### Task 4: 发布验证与路线记录

**Files:** Modify `docs/ai/progress.md`, `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/release-versioning.zh-CN.md`.

- [ ] 跑全仓 Go 测试、vet、WebUI/userscript 和 Windows AMD64/Linux AMD64/Linux ARM64 预览打包门禁。
- [x] 跑全仓 Go 测试/vet、WebUI 162/162、userscript 3/3 和 Windows AMD64/Linux AMD64/Linux ARM64 预览打包门禁。
- [x] 验证最终 ZIP 清单及二进制版本；更新同一 `.141` 包，记录最终 SHA、源码快照与本机/外部验收边界。
- [x] 路线在全部代码与测试门禁通过后标记 `.141` 已交付。
