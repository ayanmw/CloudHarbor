# .153 Download Browsing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Use checkbox tracking and one final fresh-context review.

**Goal:** 提升大下载队列查询效率，保持明细翻页、刷新与回退的浏览条件一致。

**Architecture:** 调度器先选根任务页，再补当页汇总；SQLite 用总数/未完成投影计算状态计数；API 和 Vue 用预期清单 ID 防止跨轮次翻页。沿用当前权限、状态和数据证据边界。

**Tech Stack:** Go 1.22、SQLite、Vue 3 / TypeScript。

**Spec:** docs/superpowers/specs/2026-11-03-download-browse-153.zh-CN.md

## Global Constraints

- 五项完成再发布 .153，修订仍进入同版；主目录内联、不创建 worktree、不自动提交或 push。
- .152 包和源码快照冻结；真实状态保持 .150，不复制授权或自动验收真实云端写操作。
- 查询只读；未知源项结果保持隔离；历史缺失不推算。

## Review Focus

- 十万根候选与极大 offset：候选不全部复制成 Task，不为非当前页查询历史。
- states 中旧 run_id 与结果事务提交：状态计数不重复、不把旧下载中当作活跃。
- 页间 manifest 切换及刷新中的旧响应：不拼接不同轮次，不静默跳到新清单旧页码。
- URL Unicode、保留字符、超范围 ID/offset：白名单恢复且只读，不能夹带操作。
- 剪贴板拒绝、空历史目标：明确反馈且不按当前配置生成路径。

### Task 1: 根任务候选先分页

Files: `internal/downloader/root_workspace_page.go`、`root_workspace_page_test.go`、`download_history.go`、`task_workspace.go`。

Interface: `GetRootWorkspacePage(offset, limit int, status, query string) (TaskPage, error)` 只从 queue.jobs 选择，返回当前页 Task、所有根总数和未筛选根状态计数；排序保持 workspacePriority/path/精确 ID。`GetTaskWorkspaceRoot(ctx context.Context,id int64) (*Task,error)` 只取单根及其历史，用于明细，未知根返回明确 not-found。

- [x] 写大候选、状态/路径筛选、相同路径精确 ID、子项排除与高 offset 测试，先确认失败。
- [x] 在现有 queue/task 锁顺序内选择候选，仅复制最终页；抽出历史 enrichment 接线，只查询当前页或指定根。
- [x] 运行针对性排序/权限/HTTP 测试，验证总览和明细不再走全根历史 enrichment。

### Task 2: 状态计数不遍历完整文件清单

Files: `internal/taskstore/download_manifest_page.go`、`download_manifest_page_test.go`。

Interface: 保持 `DownloadManifestFilePage` 签名和响应；空搜索总数从 totals 读取，非 pending/done 计数从未完成 states 解析 run_id 与 activeAllowed；pending=files-completed-其它非 pending 状态。搜索时保留 SQL 准确命中总数；各项查询仍在同一只读事务。

- [x] 覆盖旧 run、根暂停、失败/取消、重复结果、状态投影清除、空清单及字面量搜索，先验证新计数路径测试失败。
- [x] 实现并检查 EXPLAIN/读表路径，空搜索计数不扫描 entries/result 全量；投影矛盾显式失败。
- [x] 运行相关事务/重启和分页测试。

### Task 3: 固定清单翻页

Files: `task_workspace.go`、`task_workspace_history_test.go`、`frontend/src/App.vue`、`frontend/scripts/ui-workspace.test.mjs`。

Interface: task-workspace GET 可选 `manifest`（32 位小写 hex）；请求清单与该根当前可展示清单不符时返回 409 JSON `code=manifest_changed`，不读取其他根的清单。Vue 保留旧数据，提示显式重载并归零 offset；根切换清空预期清单。

- [x] 写页间发布新清单、旧请求晚到、格式不合法与无清单起始状态测试。
- [x] 接入 API 校验和 Vue 提示，不能自动将旧 offset 用于新清单。
- [x] 运行 Go HTTP 和前端行为测试。

### Task 4: 浏览条件 URL 恢复

Files: `frontend/src/download-route.ts`、`App.vue`、`scripts/ui-workspace.test.mjs`。

Interface: URL 白名单 task/status/q/offset/manifest；根/manifest ID 精确保留字符串，q 上限 128 Unicode code points，offset 非负整数。根/筛选切换合理归零页码；popstate 不追加历史、不发送操作请求。

- [x] 覆盖刷新/后退、Unicode/保留字符、非法值和混入操作参数测试，先失败。
- [x] 实现规范化和 App 接线，明确 push 与 replace 边界。
- [x] 构建并验证最终 bundle 的实际行为。

### Task 5: 信息面板复制与交付

Files: `frontend/src/App.vue`、`style.css`、`scripts/ui-workspace.test.mjs`、release/TESTING.md。

- [x] 测试网盘/本地已知目标复制、空路径禁用、浏览器拒绝及 aria-live 反馈。
- [x] 接入两个复制入口，沿用现有信息面板风格，避免重复大块数据；桌面/窄屏模拟可见验收。
- [x] 最终独立审查、完整 Go/vet/前端与三平台门禁、ZIP/源码快照校验及版本说明；五项齐备后才创建 .153 包。

## Ledger

- 2026-11-03 发布门禁：完整 `go test -p 1 ./...` 通过（主包 23.250s、downloader 280.351s、taskstore 212.276s）；vet、前端 29/29、legacy 172/172、userscript 3/3、Windows AMD64 / Linux AMD64 / ARM64 构建通过。ZIP 及发布目录载荷、源码快照逐项哈希、三平台文件头校验通过；最终包哈希以 releases 同级 .153-validation.md 为准。文档更新后同版本重打，旧 .152 不变；不启动真实账号或新打包程序。

- 2026-11-03 可见验收：仅本机 GET 模拟夹具，桌面与 390×844 信息面板布局正常，复制网盘路径出现成功反馈；点击已完成筛选后 URL 状态同步，浏览器后退恢复全部文件。截图位于 releases 同级 .153-desktop.jpg/.153-mobile.jpg。验收标签页关闭、viewport 重置、模拟服务结束；未连接真实百度账号。完整门禁待执行。

- 2026-11-03 Task 4/5：URL 白名单恢复、精确 ID、128 Unicode code points、超范围页码 replace 恢复、前进后退只读、路径复制/拒绝/空路径均已实现；最终 bundle 前端 29/29 通过。独立审查发现轮询误用搜索草稿，先复现再拆分 appliedSearch；可见页面轮询回归通过，审查复核关闭 P2。桌面/窄屏可见验收与完整打包门禁仍待执行，不提前宣称交付。
- Ruling: 用户确认正常扫描持续进行，只有明确限流证据才退避；本轮次数上限是可选本地控制，不是百度官方固定额度。当前 .153 不夹带新的扫描策略修改，后续以真实返回证据决定降频规律。

- 2026-11-03：.152 最终包冻结并完成独立校验后进入下一批规划。当前只创建 .153 spec/plan；未修改 .152 捕获的源码文件，也未创建 .153 发布目录。
- 2026-11-03 Task 1：先确认缺失根分页接口编译失败，以及只请求第一页仍被其他页损坏投影拖成 503 的行为失败，再实现。十万根候选、5 行分配上限、精确 ID/状态/路径顺序、Unicode 小写筛选、子项排除、MaxInt offset、权限及 HTTP 测试通过。健康第一页/指定根不读取损坏的其他根；请求损坏项仍 503，未知根为 404。主包全量 21.273s、定向 downloader、vet/diff 通过。.152 ZIP 哈希仍为 CFEAF994D89F3361FE308A9DB05DAC261FE54493AC911C723780E69B08265EE0；开发源码现已进入 .153，旧包不重打。
- 2026-11-03 Task 2：先确认新计数 helper/SQL 缺失导致测试失败，再实现。总数/完成数从 totals 主键读取，仅对 states 中未完成观察分组；旧进程/根停止时 downloading 归入 pending。空搜索总数按计数计算，空结果直接返回；非空搜索仍精确 SQL 筛选。EXPLAIN 确认计数不读 entries/results。覆盖投影矛盾显式错误、空清单、全部完成、失败/授权/取消切换、完成触发器清除状态、旧 run 及 Unicode 字面量搜索。定向 taskstore 4.540s、downloader 3.524s、主包全量 24.420s、vet/diff 通过。未升级 schema、未创建 .153 包或接触真实授权。
- 2026-11-03 Task 3：先确认 API 缺少 manifest_id 及客户端未传预期清单的测试失败，再接线。GET 可选小写 hex manifest，要求绑定根；清单不符时只返回 409 manifest_changed，不读取其他根。跨重启发布不同清单后旧 offset 请求被拒绝；无历史根返回空标识、不补造清单，携带不存在标识被拒绝。Vue 固定已返回 ID，冲突保留旧行及旧页码，禁用分页/不可靠任务操作，停止自动 GET 重试，显式载入清空预期 ID 并从第 1 页开始。延迟旧响应不能覆盖后续导航选中的 ID。前端构建及 22/22 行为测试、相关 Go HTTP 3.424s、vet/diff 通过；五项完成后再最终审查与完整发布门禁。
- Ruling: 首次读取尚无清单时不固定一个虚构空 ID；只有实际返回的清单 ID 才作为后续预期值 — 旧任务没有可用历史，新增清单后允许首次观察 — URL 持久化和规范化留给 Task 4，不在 Task 3 宣称刷新/后退条件恢复已完成。
