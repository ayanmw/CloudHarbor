# 下载任务事件筛选与证据导出 Implementation Plan

> **For agentic workers:** 按 `superpowers:executing-plans` 在主目录逐任务实现。每一步完成后更新复选框；不提交 Git。

**目标：** 在既有单任务 SQLite 事件时间线中按事件类型与本机记录 UTC 日期筛选，并提供稳定分页、匹配数和同快照有界脱敏导出。

**架构：** Store 接受固定事件类型与 UTC 微秒区间，使用只读 SQLite 事务返回筛选总数和序列分页；事件导出复用同一构造器和过滤器。Downloader 暴露薄封装，HTTP 校验类型/日期/游标，WebUI 每个任务面板单独保存筛选值并将其带入刷新、较新/较旧分页与显式导出。

**技术栈：** Go、modernc SQLite、标准 `net/http`、现有 vanilla JavaScript、Node WebUI 测试。

**规格：** `docs/superpowers/specs/2026-11-02-task-event-filtering.zh-CN.md`

## 全局约束

- 不新增 SQLite schema、索引、依赖或 API 以外的写操作。
- 日期按 UTC `YYYY-MM-DD` 解析；起始日含、结束日含整日，SQL 结束边界为下一日零点排除。
- 事件按稳定 sequence 分页；HTTP 单页 1..50 项，筛选导出最多 100 项，超限整份拒绝。
- 不把 recorded UTC 改称云端时间，不调用百度 API，不自动重试或操作任务状态。
- 保留本机已有 dirty/untracked 修改；不建 worktree，不提交/推送，不改动 `.nas-downloader` 真实账号数据。

## Review Focus

- 无过滤参数的既有路径保持默认全量行为和稳定分页：任务事件 Store/API 兼容回归。
- 日期范围正好命中日界线且结束日包含整天：SQLite 微秒单位边界测试。
- 日期逆序、闰日、无效格式和重复参数：HTTP 输入校验测试。
- 新事件追加后较早游标分页不会重复/跳过旧匹配项：Store keyset 测试。
- 过滤导出 100/101 条、单快照追加边界和敏感 evidence：全有或全无导出与 API 遮蔽测试。

---

### Task 1: Store 过滤分页与同快照导出

**Files:**
- Modify: `internal/taskstore/store.go`
- Create: `internal/taskstore/task_event_filter.go`
- Create: `internal/taskstore/task_event_filter_test.go`
- Modify: `internal/taskstore/task_events_export.go`（只在确需共用导出事务时）

- [x] 增加失败测试：事件类型、UTC 起止区间与组合过滤得到精确 `total_matches`、当前页、next cursor；旧/新追加事件不改变既有游标页。
- [x] 增加结束日整日包含、起始午夜、微秒边界、空结果及不存在任务用例。
- [x] 增加同一 SQLite 只读快照导出用例：过滤导出 100 条通过、101 条整份返回 `ErrTaskEventEvidenceExportTooLarge`，不返回截断列表。
- [x] 运行定向 Store 测试，确认新过滤用例在实现前失败。
- [x] 实现类型白名单/日期界限过滤模型与同一只读事务计数+分页方法；保留旧 `EventPage` 与无过滤导出兼容入口。
- [x] 实现同过滤器、同 SQLite 只读快照的有界导出；原有导出/重启相关回归和新测试通过。

### Task 2: Downloader 与 HTTP 查询和导出接线

**Files:**
- Modify: `internal/downloader/downloader.go`
- Modify: `main.go`
- Modify: `main_test.go`
- Modify: `internal/downloader/task_event_page_test.go`

- [x] 增加 HTTP/API RED 用例：白名单类型、UTC 日期格式、日期顺序、重复/未知 query、分页边界与默认兼容。
- [x] 覆盖任务不存在 404、导出超限 413；确认坏筛选参数被拒绝。
- [x] 实现 query 解析为规范 UTC filter，GET page 返回 `total_matches`；export 使用同参数、返回匹配总数及标准化过滤摘要。
- [x] 对过滤导出 evidence 沿用既有敏感字段遮蔽与 `no-store`。
- [x] main/downloader/Store 定向测试通过；事件 API 不调用 Provider。

### Task 3: 任务事件面板筛选

**Files:**
- Modify: `internal/webui/res/shared.js`
- Modify: `internal/webui/res/shared.css`
- Test: `scripts/webui.test.cjs`

- [x] 增加 WebUI RED 测试：类型/日期值进入请求，显示 `total_matches`，应用或清除回到最新页。
- [x] 验证较旧分页及手动刷新保留过滤条件，迟到的不同筛选响应不会覆盖当前面板；增加较新分页由既有逻辑覆盖。
- [x] 验证导出仅在显式点击时发出当前筛选参数；401/413/通用失败提示、下载文件处理和敏感数据遮蔽保持正确。
- [x] 实现面板内表单、固定类型中文标签、UTC 日期控件、命中数和清除动作；事件时间字段继续按现有语义分别展示。
- [x] 定向 WebUI 测试通过；事件文本保持 HTML 转义。完整 WebUI/userscript 门禁在 Task 4 执行。

### Task 4: 达门槛后发布 `.144`

**Files:**
- Modify: `docs/ai/cloud-management-roadmap.zh-CN.md`
- Modify: `docs/ai/progress.md`
- Modify: `docs/ai/release-versioning.zh-CN.md`
- Modify: `docs/superpowers/specs/2026-11-02-task-event-filtering.zh-CN.md`
- Modify: `docs/superpowers/plans/2026-11-02-task-event-filtering.md`
- Create: releases 同级 `v0.8.30-preview.144-validation.md` 和 `v0.8.30-preview.144.zip`

- [ ] 五项用户结果都有 Store/API/WebUI 回归依据后才标为 `.144` 交付。
- [ ] 运行 `scripts/package.ps1 -Version v0.8.30-preview.144 -AllowDirty`；确认 Go tests、vet、WebUI/userscript、Windows AMD64/Linux AMD64/Linux ARM64 构建通过。
- [ ] 核对 ZIP、12 项载荷哈希、源码快照和包内无私有运行数据；Git diff 检查不包含本批以外编辑。
- [ ] 真实账号只读检查只在当前进程健康、队列空且服务路径确认后进行；不触发下载、网盘列表或写操作。不得以本机检查声称 NAS/SMB 验收。

## 批次验收

五项独立结果：事件类型筛选；本机 UTC 日期范围筛选；匹配总数；筛选条件稳定贯穿分页/刷新；同快照筛选导出及明确超限边界。相关来源：`docs/superpowers/specs/2026-11-02-task-event-filtering.zh-CN.md`。
