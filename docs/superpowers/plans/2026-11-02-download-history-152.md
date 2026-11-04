# .152 Download History Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** 下载记录在重启后仍可查询，清楚区分历史观察、本地发布与当前在线状态。

**Architecture:** 根目标快照扩展现有 job JSON 并原子追加事件；文件清单和结果保存于 SQLite，通过有界查询提供界面明细。运行状态继续由现有调度器管理。

**Tech Stack:** Go、SQLite、Vue。

**Spec:** docs/superpowers/specs/2026-11-02-download-history-152.zh-CN.md

## Global Constraints

- 主目录内联，不创建 worktree，不 push，不复制授权。
- 五项完成后才发 .152；旧任务缺失证据不回填。
- 历史目标仅展示，不作为文件操作路径；未知远端结果继续隔离。

## Review Focus

- 保存失败后不能继续下载或远端后处理。
- 旧任务空字段不能按当前设置推算成历史事实。
- 新尝试和旧清单身份变化不能串用完成记录。
- 发布前崩溃不能仅凭 complete.json 显示完成。
- 大目录查询不能恢复全量内存任务。

### Task 1: 根目标快照

Files: `internal/downloader/root_history.go`, `root_history_test.go`, `scheduler.go`, `downloader.go`, `task_workspace.go`。

Interface: `recordRootDestination(info baiduapi.FileInfo, local string) error`；`RootDestination` 包含 path、attempt_id、recorded_at_utc；随 job 保存并由 Task/工作区 DTO 输出。

- [x] 写关闭重开、旧任务无快照、保存失败回滚及重复调用测试，先运行确认失败。
- [x] 在 runRoot 有效根路径校验后、本地处理前记录；事件 `download_destination_recorded` 与 job 原子保存。相同尝试同路径不重复追加。
- [x] 运行测试并确认读展示不检查或改写本地路径；检查 diff。

### Task 2: 持久化目录清单

Files: `internal/taskstore/download_manifests.go`、`download_manifests_test.go`、`migration_download_manifests.go`、`sqlite.go`、`store.go`；下载接线 `internal/downloader/download_manifest.go`、`directory_plan.go`。

Interfaces: `BeginDownloadManifest(ctx context.Context, manifest DownloadManifest) error`；`AppendDownloadManifestEntries(ctx context.Context, id string, entries []DownloadManifestEntry) error`（每批最多 512）；`PublishDownloadManifest(ctx context.Context, id string, at time.Time) error`；`CurrentDownloadManifest(ctx context.Context, rootID int64) (*DownloadManifest, error)`；`DownloadManifestEntries(ctx context.Context, id string, offset, limit int) ([]DownloadManifestEntry, error)`（limit 1..200）。清单 ID 为独立 32 位小写 hex，与尝试 ID 分开；条目保存身份、路径、名称、类型、大小、mtime、云端 hash、本地相对路径及观察 UTC。

Schema v42：`download_manifests` 保存根任务外键、尝试、开始/发布 UTC；`download_manifest_entries` 用 `(manifest_id,fs_id)` 主键及 `(manifest_id,path)` 唯一约束；`download_manifest_current` 原子指向当前完整清单。发布前必须有根条目且所有非根父项存在并为目录。新准备失败时旧指针不变。迁移只建空表，不回填历史；测试 v41 正常升级和数据库提交后 manifest.json 尚未更新的恢复。

- [x] 阅读现有 schema 迁移及批量写约定，确定 manifest/entry 表、索引和事务接口，补充精确接口到本节后实现。
- [x] 测试半份清单不发布、批次失败回滚、关闭重开及身份重复；再实现准备阶段有界写入与最终发布。

### Task 3: 本地完成证据

Files: `internal/taskstore/download_manifest_results.go`、`download_manifest_results_test.go`、`internal/downloader/download_manifest.go`、`transfer.go`，以及 schema v42 初始化/迁移的清单建表常量（本版尚未发布，真实库未升级）。

Interface: `RecordDownloadManifestResult(ctx context.Context, manifestID string, expected DownloadManifestEntry, result DownloadManifestResult) error`；结果包含 `kind`（`published` 或 `local_verified`）、实际内容 SHA256、`recorded_at_utc`。保存时核对 manifest 已发布、文件身份/路径/size/mtime/cloud_md5/relative_path 一致，目录不接受文件完成结果。独立结果表用 `(manifest_id,fs_id)` 绑定清单条目；重复一致结果不改写首次记录时间，冲突结果拒绝。

接线要求：新文件在 `publishDownloadedFile`、目录同步和临时文件清理全部成功后保存；复用分支在 SHA256 核对和目录同步成功后保存。不能用 `complete.json` 写入成功替代结果。结果保存失败向上传递，让根任务跳过 postprocess；下次尝试重新枚举并经现有文件完整验证生成本次证据，不回填之前的实际完成时间。

- [x] 定义绑定 manifest/远端身份的文件发布与复用验证结果接口；测试发布前中断不完成、保存失败阻止后处理。
- [x] 接入最终发布同步成功及已有文件完整验证分支，验证跨重启结果与失败路径。

### Task 4: 根汇总恢复

Files: `internal/taskstore/download_manifest_summary.go`、`download_manifest_summary_test.go`、`internal/downloader/download_history.go`、`download_history_test.go`、`task_workspace.go`、`task_workspace_test.go`、`frontend/src/App.vue`。

Interfaces: `CurrentDownloadManifestSummary(ctx context.Context, rootID int64) (*DownloadManifestSummary, error)` 返回已发布清单、根身份及 files/folders/size/completed/completed_bytes，单个只读事务保持一致；`Downloader.GetRootDownloadHistory(ctx context.Context, rootID int64) (*RootDownloadHistory, error)` 返回 `source=current_attempt/previous_attempt/unrecorded`。只有尝试 ID 和根身份均相符才标记 current_attempt，其他完整清单是历史观察。

工作区先保留现有即时任务展示；仅 current_attempt 使用持久化清单总数和已记录完成数，叠加本次非完成活跃文件的瞬时进度/速度。previous_attempt 的汇总放入历史依据字段，本轮数量留空直到新清单发布，避免旧内存子项冒充当前准备完成；reason 区分尝试变更与根身份变更。重启后不把清单条目插入运行任务内存。读取失败返回 HTTP 503，不能悄悄退成 0 项。

汇总实现补充：发布清单时一次聚合写入 `download_manifest_totals`，结果插入触发器在同事务增加 completed/completed_bytes；轮询只按主键读取汇总，不反复遍历大目录。字段是可更新投影，完成依据仍是只追加结果表。失败事务必须同时回滚结果和汇总。

- [x] 测试新旧清单切换、旧任务无记录、完整/不完整汇总；实现 SQLite 聚合与工作区根行来源标记。

### Task 5: 明细查询和交付

接口：`RecordDownloadManifestState(ctx,id,expected,status,summary) error` 保存本进程文件状态；`DownloadManifestFilePage(ctx,id,status,query,offset,limit,activeAllowed) (DownloadManifestPage,error)` 最多 200 行，SQLite 完成筛选/排序/分页。新状态表只保留未完成项，结果提交事务删除对应状态投影；完成证据仍只追加。状态包含 run_id，本轮下载中状态在重启或根任务停止后降为 pending 并标记 interrupted，不自动变成完成。搜索使用 Go 预先小写化的 search_path 和 SQL instr，支持 Unicode、字面量百分号/下划线。

下载开始和失败接入状态投影，取消后用有界脱离取消上下文保存中断状态；不存原始下载 URL/令牌或未净化错误。页面只叠加当前页且身份匹配的即时进度，状态筛选/顺序以数据库读取快照为准。工作区只复制根任务和活跃文件；不会为了历史分页复制全部内存子项。旧清单只读展示，只有本轮目标快照匹配才推导展示本地文件路径，不能拿当前配置反推历史路径。

- [x] 定义分页/搜索/状态查询 DTO，测试有界查询、Unicode、历史中断状态及来源时间。
- [x] 接入 Vue 明细与证据提示；完成前端和 Go 验证、独立审查、三平台打包与版本说明。

## Ledger

- 2026-11-02：基线 6ce8384，工作区干净；已读根任务持久化、目录准备、明细汇总与凭据发布顺序。
- Ruling: 后续表接口在 Task 2 阅读迁移接线后定稿 — 避免先猜接口再扩大修改 — 后续任务开始前必须补齐，当前不能据此宣称完整计划已可执行。
- 2026-11-03 Task 1：先确认缺失函数/字段导致测试失败，再实现；三个针对性 Go 测试通过，覆盖 SQLite 重开、旧值留空、去重、追加不同尝试事件、返回对象隔离、保存失败阻止 runRoot。Vue 信息抽屉展示 UTC 与历史目标说明，构建及 18/18 前端交互通过；vet 通过。全量 Go 回归通过（downloader 208.136s、taskstore 130.741s）；最终追加的针对性测试亦已单独重跑通过。
- 2026-11-03 Task 2：先观察缺失 API 编译失败及下载准备未发布清单的行为失败，再实现。清单/迁移针对性测试通过；覆盖指针事务失败回滚、旧完整清单跨重启保留、重复身份整批回滚、孤立子项拒绝、已发布清单禁止 API 追加、v41 正常升级/manifest 落后恢复、600 文件跨 512 条批次和 200 条分页。根快照来源与本轮目录列表来源分开；不会凭旧快照宣称本轮在线核对。vet 通过；首次全量回归 downloader 227.658s 及其余包通过，taskstore 仅旧版本断言失败；修正后 taskstore 全包重跑通过（140.681s）。最终打包前仍需在全部五项完成后重新运行完整门禁。
- Ruling: v40 升级测试最终 schema 由写死 41 改为当前常量 — Open 应应用所有后续迁移，缺失冷却记录断言保持 — 未放宽历史真实性要求；先确认 `version=42 integrity=ok` 导致旧断言失败，再修正并重跑通过。
- 2026-11-03 Task 3：先运行缺失 API 的存储测试及未接线的下载行为测试，确认失败后实现。结果为独立表、复合外键绑定条目、追加后禁止 UPDATE/DELETE；首次相同 SHA256 证据保留，冲突摘要拒绝。测试覆盖八类身份变化、草稿拒绝、SQLite 重开、发布/复用分支、receipt 已写但发布前取消、保存失败阻止远端核对/操作、保留已发布文件并在下一尝试校验复用且不重下。针对性测试和 vet/diff 检查通过，全仓回归通过（downloader 257.764s、taskstore 158.940s）。最终追加的恢复/身份矩阵与 schema 字段校验亦单独重跑通过；五项全部完成后还需最终完整门禁。
- Ruling: 用私有 context 值绑定已发布清单的固定 ID，而非每个文件按路径重新找当前清单 — 保证本次工作线程关联同一清单，避免重试/并发串用 — 直接本地帮助函数没有根清单上下文时维持原行为，正式 runRoot 和目录下载必须先发布再绑定。
- Ruling: 新清单键将 Windows filepath.Join 的相对路径转为 slash — 集成测试发现 Windows 目录子项身份比较失败 — 仅转换新历史键，不改变已有 complete.json 的相对路径兼容规则。
- 2026-11-03 Task 4：清单汇总、关闭重开、旧身份/旧尝试分类、空历史、HTTP 503、已提交结果不重复叠加活跃进度均通过针对性测试；真实 HTTP handler 从 SQLite 重开后的任务恢复汇总，超过 2^53 的根 ID 保持字符串精度，运行任务 map 未重新装入历史子项。前端构建与 19/19 交互、vet、diff 检查通过。首次本轮全仓回归 downloader 407.124s、taskstore 305.907s 和其余内部包通过；主包授权刷新测试有一次 3 秒启动等待超时，隔离同子项连续 3 次通过，未改超时断言；主包单独全量重跑通过（21.081s）。此现象暂记为未稳定复现的计时敏感项，不能据此宣称已修复；最终发布门禁需再次覆盖。新增投影及最终补充测试另有针对性通过记录，最终发布前仍须完整门禁。
- Ruling: 发布时一次聚合，逐文件结果事务同步更新汇总投影 — 每次页面轮询扫描整个目录会随文件量增长 — 新增空投影表与触发器纳入尚未发布的 schema v42，不迁移真实账号库；重复结果不增计数、发布失败/结果失败不留下半份汇总。
- 2026-11-03 Task 5：SQLite 文件分页/排序/状态筛选和 Unicode 字面量搜索、重启后 downloading 降为 pending/interrupted 均通过。运行状态保存固定净化分类，不持久化原始网络 URL/凭据；取消后有界保存终止状态。工作区仅复制根和活动文件，历史页不会重建全量任务；同页即时进度只叠加本轮相同身份。文件详情含观察来源/UTC/本地结果/SHA256，不再请求子文件不存在的根任务事件。前端 20/20、定向 Go/HTTP/迁移测试、独立 fresh-context 审查通过。桌面和 390px 窄屏模拟页面可见验收通过，截图为 JPEG，临时标签页/Node 18091 服务关闭且视口复原。完整测试与三平台打包尚在最终门禁中。
- Ruling: 详情摘要的重复标题/留白压缩并按已封存清单改文案 — 实际 1265×712 可见验收发现列表在首屏下方 — 只作用于详情桌面布局，移动规则保留；复核及截图确认已在首屏显示文件行。
- Ruling: 发布脚本采用 `go test -p 1 ./...` — 同步 SQLite fixture 在包级并行回归中曾伴随授权刷新等待超时，串行减少独立包的磁盘写交叠而不改测试内容/原超时断言 — 代价是冷缓存发布验证更长；不据此宣称已证明或修复该偶发计时问题。

- 2026-11-03 最终发布：CGO_ENABLED=0 的完整 Go 串行门禁通过（主包 22.426s、downloader 258.630s、taskstore 204.378s、webui 1.204s），vet、前端 20/20、legacy 172/172、userscript 3/3、三平台构建通过。首轮 ZIP 13 项、12/12 载荷哈希、563 项源码内容快照和 PE/ELF 架构检查通过；最终包在相同版本内同步本记录后重打并复核，哈希以外部 validation.md 为准。独立只读审查及桌面/窄屏模拟可见验收完成。
- 验证过程记录：早先 CGO_ENABLED=1 全仓检查在前端重建后出现旧 embed 文件已被替换的编译失败；这是验证期间修改资源造成的，未把该轮算作通过。最终发布脚本按前端构建完成后再运行 Go 门禁，资源冻结且完整通过。并行回归中的授权刷新计时敏感项保留原断言，本轮串行完整主包通过，不宣称证明根因。
- 真实边界：健康只读核对仍为 .150、授权 ready；真实库 schema 41、source_unknown 1、旧任务事件仍 5 条。新包无 .nas-downloader，未复制/迁移授权、未执行真实百度写操作、未做独立二进制启动或 NAS/SMB 验收。模拟 Node PID 71472/端口18091 和临时标签页已关闭、浏览器尺寸已恢复。
