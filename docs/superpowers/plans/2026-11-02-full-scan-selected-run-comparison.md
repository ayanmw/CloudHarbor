# 全盘扫描历史指定轮次对比实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 允许用户从扫描历史分页中挑选任意两轮完成扫描，计算并导出可审计的定向覆盖比较。

**Architecture:** 历史 API 现有摘要进入页面内 `Map`，选择最多两条完成运行并跨分页保留；浏览器按 sequence 排序并对 immutable 快照计算差值。对比与 JSON 下载均纯本地，不调用新增 API。

**Tech Stack:** 原生 JavaScript WebUI、Node `node:test`。

**Spec:** `docs/superpowers/specs/2026-11-02-full-scan-selected-run-comparison.zh-CN.md`

## Global Constraints

- 只比较认证历史页返回的同账号完成扫描摘要。
- sequence 小者是前序；所有差值为后序减前序；NULL 不得按零解释。
- 仅用户显式触发 JSON 下载；不得新增网络请求、SQLite 写入或百度操作。
- 页面只保留两条选择；刷新页面清除选择。
- 导出不含 `account_fingerprint`、凭证或网盘文件内容/路径。

## Review Focus

- 跨页选择不重复计数，且稳定绑定 immutable `run_id`。
- 同一 run 不能占用两侧选择；未完成运行不可勾选。
- 用户先选新轮时仍正确排序和定向计算差值符号。
- 历史摘要、覆盖快照、原因快照分别缺失时输出对应的不可比较说明，不填补零。
- JSON 导出中的 run ID 和任意字段按 JSON serialization 处理，不拼接到 HTML。

---

### Task 1: 增加双轮选择和跨分页状态回归

**Files:**
- Modify: `internal/webui/index_cloud.html`
- Test: `scripts/webui.test.cjs`

- [x] 先写 WebUI RED 测试：两条完成轮可选择、第三条不能加入、非完成轮不可选、换页仍显示已选和清除可用。
- [x] 执行对应 node 测试并确认失败在功能缺失。
- [x] 增加行内选择控件和页面内 Map；每次历史响应按 run_id 更新候选，不因翻页删除候选。
- [x] 定向 WebUI 用例通过。

### Task 2: 计算指定轮次差值与空值语义

**Files:**
- Modify: `internal/webui/index_cloud.html`
- Test: `scripts/webui.test.cjs`

- [x] RED 测试先选新轮/后选旧轮，验证前序后序按 sequence 自动排序、未完整数/原因正负号、前后 UTC。
- [x] RED 覆盖无快照、单轮、原因缺失、相同 run ID 和恶意文本。
- [x] 实现纯函数比较：coverage 摘要要求两轮 status/count/observed UTC 非空；reason delta 要求双方原因对象存在。
- [x] 显示可理解的缺失原因且安全转义；通过定向回归。

### Task 3: 显式导出所选两轮 JSON

**Files:**
- Modify: `internal/webui/index_cloud.html`
- Test: `scripts/webui.test.cjs`

- [x] RED 测试确认选择未完成、比较完成、只在用户点“导出比较 JSON”时触发下载，未多发 fetch。
- [x] 导出按明确白名单取字段，包含两轮原始摘要、observed UTC 和 comparison；不含账号指纹。
- [x] 验证浏览器 Blob 下载名称固定且不包含远端路径；导出后按钮可继续使用。

### Task 4: 记录路线并发布 `.143`

**Files:**
- Modify: `docs/ai/cloud-management-roadmap.zh-CN.md`, `docs/ai/progress.md`, `docs/ai/release-versioning.zh-CN.md`
- Create: `docs/superpowers/specs/2026-11-02-full-scan-selected-run-comparison.zh-CN.md`
- Create: `docs/superpowers/plans/2026-11-02-full-scan-selected-run-comparison.md`
- Create: releases 同级 `.143-validation.md` 和 ZIP

- [x] 达到至少五个用户结果后整理 `.143` 版本记录。
- [x] 运行 WebUI/userscript、全仓 `go test ./...`（本次 Go 包结果为缓存命中）、`go vet ./...` 与 Windows AMD64/Linux AMD64/Linux ARM64 包门禁。
- [x] 停止 `.142` 后将唯一真实 `.nas-downloader` 移到 `.143`；移动操作是原目录改名移动，目标含 39 个文件，`.142` 源路径已不存在；不复制或清理历史测试 release。
- [x] 启动 `.143` 做本机 SQLite、health、授权就绪、空队列、扫描 worker 未运行、历史页面 HTTP 只读核验。
- [x] 写验证记录和最终哈希；本机 mock/UI 不称为真实历史对比或 NAS/SMB/ARM64 实机验收。

## Scope self-review

五类验收输入（选择分页、排序方向、非完成/重复 run、快照 NULL 和导出边界）分别落入 Tasks 1–3；无需服务端/SQLite schema 改动。Task 4 维护五项版本门槛、测试和唯一运行数据迁移。
