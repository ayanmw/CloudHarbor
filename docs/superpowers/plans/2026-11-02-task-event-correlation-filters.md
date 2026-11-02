# 任务事件关联 ID 筛选 Implementation Plan

> 按已授权 inline 路线在主目录逐任务实现；不创建 worktree、不提交/推送。规格：`docs/superpowers/specs/2026-11-02-task-event-correlation-filters.zh-CN.md`。

**目标：** 扩展 `.144` 单任务事件时间线，按 operation ID / attempt ID 精确筛选，并贯穿计数、稳定分页、刷新及有界脱敏证据导出，作为 `.145` 候选。

**架构：** `taskstore.TaskEventFilter` 添加两个精确 ID 条件，SQLite 参数化 AND 到现有 job/type/date 查询；只读事务继续读取完整匹配总数和 sequence 页面，export 复用同一过滤器。HTTP 对 ID query 做 singleton 和长度/空白/control 校验；WebUI 表单可手动输入，事件卡片提供“一键筛选”，状态/API 保持既有刷新与迟到响应保护。

## 全局约束

- 不新增表、列、schema 版本、依赖或百度 API 请求。
- 不把 ID 作为 SQL/HTML 原文拼接；参数化 SQL、HTML attribute/text 转义、现有敏感 JSON 遮蔽保持。
- 对 job ID 的事件查询隔离不变；ID 不存在返回空匹配，任务不存在 404，store 不可用 503，筛选导出 >100 返回 413。
- `.144`真实状态仅在新包通过全门禁后移动；不复制 sidecar，不清理历史 release，不执行远端写操作。

## Review Focus

- 单 task AND 限定不能因 operation/attempt 条件退化成跨任务查询。
- 分页 cursor 在 correlation ID 过滤下仍以原 sequence 排他边界工作。
- 参数边界涵盖 0、1、128、129 字节、多值、空白、控制字符、SQL 特殊字符。
- 事件卡片 action 保留 kind/UTC 日期，并清理另一种 correlation ID；迟到旧请求不能覆盖当前筛选。
- 导出只包含匹配 ID 和其它筛选结果；100 条通过、101 条拒绝不截断。

## Task 1: Store 关联 ID 查询和导出（TDD）

**Files:** `internal/taskstore/task_event_filter.go`、`internal/taskstore/task_event_filter_test.go`。

- [ ] 先加 RED：operation、attempt、二者 AND、与 type/date 组合、不同 task 隔离和 ID 无匹配结果。
- [ ] 验证 RED 因 `TaskEventFilter` 无 ID 字段失败。
- [ ] 实现两个精确参数化 `event.operation_id=?` / `event.attempt_id=?` 条件；只读 count/page/export 继续用同个 builder 与快照。
- [ ] 覆盖 ID 过滤的稳定 cursor、追加事件不移动旧页，以及导出 100 成功/101 全量拒绝。
- [ ] 运行 `go test ./internal/taskstore -run 'TaskEventFilter|FilteredTaskEvent|EventPageFiltered' -count=1`。

## Task 2: HTTP 参数校验、分页与导出（TDD）

**Files:** `main.go`、`main_test.go`。

- [ ] 先加 RED：ID 单值/长度/空白/control/SQL 字符串、重复和未知 query；页面 `total_matches` 以及 export 同筛选。
- [ ] 增加 0 命中、跨任务隔离、task 404、store closed 503、100/101 export 413 的 API 回归。
- [ ] 实现只接受 1..128 UTF-8 字节、无首尾空白/control 的 ID query；bad query 400，Store 仍参数化精确匹配。
- [ ] 运行 `go test . -run 'TaskEvent.*(Filter|Correlation|Export)' -count=1` 与 `go test ./internal/downloader -run 'TaskEvent' -count=1`。

## Task 3: 事件卡片聚焦 ID（TDD）

**Files:** `internal/webui/res/shared.js`、`internal/webui/res/shared.css`（若现有样式足够则不改 CSS）、`scripts/webui.test.cjs`。

- [ ] 先加 RED：ID 输入进入请求；点击卡片 operation/attempt 控件设置指定 ID、清除另一项、保留 kind/date，并回最新页。
- [ ] 验证筛选保留在 refresh/older/newer/export；迟到响应隔离；ID 含引号/尖括号安全转义；清除筛选清掉所有条件。
- [ ] 实现 correlation ID 控件、事件卡片操作按钮和 query 参数；空 ID 不显示 action。
- [ ] 运行 `node --test --test-name-pattern='task event' scripts/webui.test.cjs`。

## Task 4: 发布 `.145` 候选

**Files:** `docs/ai/cloud-management-roadmap.zh-CN.md`、`docs/ai/release-versioning.zh-CN.md`、`docs/ai/progress.md`、本规格/计划、releases 同级 validation 与 ZIP。

- [ ] 五项用户结果均有 Store/API/UI 依据；更新路线图和进度。
- [ ] 运行 WebUI、userscript、`go test ./... -count=1`、`go vet ./...` 与 `scripts/package.ps1 -Version v0.8.30-preview.145 -AllowDirty`。
- [ ] 校验三平台、ZIP 13 项/12 载荷哈希、SOURCE-SNAPSHOT 和无私有状态数据。
- [ ] 只读核验 `.144`任务为空/扫描空闲后停止程序，唯一 `.nas-downloader` 原位移至 `.145`；启动 `.145` 并核对 SQLite integrity、授权与路径配置。
- [ ] 记录精确 ZIP/SOURCE-SNAPSHOT 哈希及未验收边界；保留 `.144` 历史 release，不提交 Git。
