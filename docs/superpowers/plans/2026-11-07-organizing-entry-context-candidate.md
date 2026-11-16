# 整理入口与浏览上下文衔接实施计划（v0.8.32）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让当前目录去重、两个目录比较和返回原浏览位置形成完整旅程，复用已有只读分析与清理预览。

**Architecture:** 在已有 Vue 工作台接入明确入口，复用当前已确认的列表选择与路由。用一个受约束的 `return_files` 路由字段保留浏览上下文，不新增后端任务、数据库表或写入策略。

**Tech Stack:** 现有 Vue 3 / TypeScript / Vite / Node 内置测试与 JSDOM；Go API 不改合同。

**Spec:** `docs/superpowers/specs/2026-11-07-organizing-entry-context-candidate.zh-CN.md`

## 全局约束与启动门禁

- [x] 2026-11-08 用户明确要求开始下一个版本：v0.8.32。已归档 v0.8.31 源码，按本计划实施；用户验收反馈到达后同版修复。
- [x] 检查工作区、暂存区和唯一账号目录，保留无关改动；不创建 worktree。不自动提交/push；切版归档/启动标签仅按项目的明确切版授权执行。
- [x] 本批三个结果为上限，不添加全盘扫描、自动删除、选择持久化、收藏或新网盘 API。
- [x] 不自动 POST，不复原写确认；未知结果不重发，线上核对门禁不变。

## Task 1: 当前目录去重及安全返回路由

修改：`frontend/src/cloud-files-route.ts`、`CloudFilesWorkspace.vue`、`CloudToolsNav.vue`、`CloudCleanupWorkspace.vue`、`DuplicatePreview.vue`。

测试：`frontend/scripts/cloud-files-navigation.test.mjs`（已有 TypeScript 路由解析/序列化测试）、`ui-cloud-files.test.mjs`、`ui-cleanup.test.mjs`、`ui-layout.test.mjs`。不把旧版 `cloud-route.test.mjs` 当作新版路由测试。

接口：消费 `readCloudFilesRoute(search)` 和 `cloudFilesRouteURL(route: CloudFilesRoute)`；产出 `validCloudWorkspaceReturn(value: string): string`（无效返回空字符串）及当前工作台内 `cleanupDirectoryHref`。调用方先校验 `return_files` 参数最多一个；helper 校验参数值，不能由 `.get()` 静默选取重复参数。

- [x] 新增失败回归：成功目录的入口链接包含精确范围；进入清理页不自动扫描/预览/选择/执行；返回 URL 保留已提交浏览状态。
- [x] 路由回归覆盖 `/cloud/files`、中文目录、排序/混排/分页、缓存搜索、外站 URL、`//host`、`#fragment`、重复/嵌套返回参数；已有缓存搜索返回合同保持。
- [x] 实现 `validCloudWorkspaceReturn(value: string): string`，由已有 `readCloudFilesRoute` 与 `cloudFilesRouteURL` 规范化；只在 URL 交界处校验，不另建导航框架。
- [x] 增加当前目录的 `cleanupDirectoryHref`；在读取/错误/认证及操作忙碌时禁用，有名称和就近说明。
- [x] 清理工作台验证 `directory` 与 `return_files`，把实际范围传给上下文返回；显式无效范围不退回 `/`，保留用户明确修正入口。
- [x] 构建后运行新增与既有对应回归；只审本任务 diff。提交与否遵循用户当前授权，不为流程自动提交。

新增测试名：`workspace return accepts canonical internal browsing only`、`directory cleanup entry preserves explicit scope without writes`、`explicit invalid cleanup scope never expands to root`。分别断言外站/嵌套结果为空、链接范围精确且初始请求全为GET、明确无效范围禁用预览且显示原因。

在 `frontend` 目录执行：`npm run build`，然后 `node --experimental-vm-modules --test scripts/cloud-files-navigation.test.mjs scripts/ui-cloud-files.test.mjs scripts/ui-cleanup.test.mjs scripts/ui-layout.test.mjs`。实现前新增断言应失败；实现后零失败。

## Task 2: 两目录比较入口

修改：`CloudFilesWorkspace.vue`、`DirectoryAnalysisWorkspace.vue`；复用 `selectedEntries`、已有 `left/right` 和报告生成流程。

测试：`ui-cloud-selection.test.mjs`、`ui-directory-analysis.test.mjs`。

接口：消费现有 `selectedEntries: ComputedRef<CloudFileEntry[]>` 和任务1返回校验；产出工作台内部 `selectedComparisonRoots: ComputedRef<[string,string]|null>`、`comparisonUnavailableReason: ComputedRef<string>`、`comparisonHref: ComputedRef<string>`。有效左右目录按绝对路径字符串顺序固定；无效时 roots=null、href为空且原因非空。

- [x] 失败回归：恰好两目录、稳定左右顺序、中文路径；一项/三项/文件混入/重复/父子/相似前缀分别有准确结果。
- [x] 实现 `selectedComparisonRoots` 与 `comparisonHref`，只基于当前成功快照。列表刷新失效后不使用旧选择；过期响应不能恢复入口。
- [x] 明确的比较链接只预填左右目录；不得在加载、刷新、BFCache 或返回时自动生成报告。
- [x] 报告查询和 `route(id)` 保留校验后的 `return_files`；同步来源的旧导航保持，缓存报告仍明确历史性质。
- [x] 构建并运行对应回归，确认没有清理/同步执行请求，检查范围和来源一致性。

新增测试名：`two selected directories create a stable readonly comparison link`、`overlapping or mixed selections explain why comparison is unavailable`、`comparison navigation never creates a report automatically`。断言 `/a` 与 `/ab` 可比较、`/a` 与 `/a/sub` 不可比较，交换列表顺序后默认左右不变，进入分析只GET且左右准确。

在 `frontend` 目录执行：`npm run build`，然后 `node --experimental-vm-modules --test scripts/ui-cloud-selection.test.mjs scripts/ui-directory-analysis.test.mjs`。实现前新增入口断言失败；实现后零失败。

## Task 3: 跨工作台返回与用户旅程验收

修改：`DuplicatePreview.vue`、`DirectoryAnalysisWorkspace.vue`、`CloudToolsNav.vue` 及必要的局部样式。

测试：`ui-cleanup.test.mjs`、`ui-directory-analysis.test.mjs`、`ui-layout.test.mjs`、`ui-cloud-files.test.mjs`。

接口：沿用任务1的 `validCloudWorkspaceReturn` 和 `return_files` 字段；让现有 `compareParents(...)` 的输出及分析页 `route(id)` 只携带已校验的浏览返回值，不新建通用导航状态服务。

- [x] 失败回归：去重组的“比较所在目录”继续携带原浏览返回位置；刷新/翻页和打开历史报告不借用其他来源。
- [x] 返回只保留目录、排序、混排、分页、搜索等已提交状态；不携带删除勾选、待确认批次、未提交表单或任何写动作。
- [x] 过期浏览快照按既有一次恢复合同处理；失败保留历史观察与就近错误，不无限重建、不伪装成新观察。
- [x] 390 视口、长路径、Tab/Enter/Esc 和 Header 下焦点位置实际复查。表格局部滚动，不靠隐藏证据避免溢出。
- [x] 唯一真实账号、自有测试目录只读旅程复查；需要生成报告时明确记录本地报告写入，不执行清理/同步/上传。结束服务并恢复原队列状态。
- [x] 完整前端、Go/vet 与三目标门禁、包/源码/编译身份/数据保护核对；同一活动版本覆盖程序白名单，不覆盖账号或下载数据。

新增测试名：`duplicate comparison returns to its original committed browsing state`、`late report responses cannot replace the active return context`、`restoring a workspace never restores write intent`。断言返回保留排序/混排/页码或搜索，迟到响应不改变上下文，pagehide/pageshow与BFCache不增加POST或恢复清理选择。

在 `frontend` 目录执行 `npm run build`，再运行上述四个UI测试文件及 `npm test`。真实UI记录具体观察构建、390视口宽度、键盘路径和请求范围；最后在仓库根执行 `powershell -NoProfile -File scripts/package.ps1 -AllowDirty`，必须完整门禁退出0并独立核对产物/数据后才交付。

## 复审重点

1. 手工全范围入口与显式无效范围不得混淆。
2. 返回 URL 是站内浏览状态，不是开放跳转或可执行请求。
3. 列表、去重范围、左右目录和返回位置各自绑定明确来源，不借用迟到响应。
4. 现有独立报告及缓存代次保持，浏览状态不能成为删除凭据。
5. 清理和同步的既有危险操作确认保持，不能因快捷入口绕过。

## 交付判据

三个入口旅程可实际演示，默认只读，无新增后端状态机，相关失败边界有回归；每项完成都有源码、测试及可见交互记录。2026-11-08已获切版授权；以本文件勾选和版本说明区分源码、自动化与真实验收。

2026-11-08 真实UI追加回归：待刷新提示保留选择时，新整理/比较入口一度仍可用；新增失败回归后补齐 returnNotice 门禁，191项相关回归和独立增量审查通过。同源临时构建真实复查通过，最终正式门禁按原标准串行通过：前端692、旧网页176、浏览器脚本3、Go/vet及三平台；56个账号文件更新前后SHA一致。
