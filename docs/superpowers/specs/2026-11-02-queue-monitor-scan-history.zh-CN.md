# 自动队列扫描历史与重启恢复设计

## 意图

自动下载依赖后台定期检查百度监控目录。现在 `/api/health` 的最近扫描时间、成功时间和错误只保存在 `main.go` 的进程内存中；重启后历史消失。另一个可复现的语义缺口是 `ScanAndDownload` 在 maintenance 或并发扫描时返回 `nil`，`runScanner` 会把没有执行的扫描记为成功。

本版为自动队列巡检增加独立于全盘库存扫描的 SQLite 运行历史。管理员可从监控页进入历史页，按状态和 UTC 日期查找扫描记录，展开单轮事件并导出其本地证据。运行结果与事件跨重启保留；历史目录路径按扫描开始时快照保存。

## 目标

1. 每次实际开始的自动队列扫描，以本地绑定百度用户 ID 的 SHA-256 指纹和 run ID 关联 SQLite 摘要及 append-only 事件；该指纹跨 OAuth access token 刷新保持稳定，且不会写入 API/UI/export。开始、完成状态和事件在同一 SQLite 事务提交。
2. 记录程序观察的 `started_at_utc` / `finished_at_utc` / `recorded_at_utc`，以及百度 `ListDir` 成功返回后的 `cloud_observed_at_utc`。失败或未执行时不伪造百度观察时间。
3. 新进程开始同账号的下一轮扫描时，将遗留 `running` 标记为 `interrupted`，追加恢复事件；不把中断记录推断为成功或失败，也不重发任何百度写操作。
4. 维护门禁和 scan mutex 忙碌返回明确的 skip 结果，不能更新时间为最近成功。活动扫描仍由现有准入互斥控制。
5. 提供认证、账号隔离、稳定 cursor 分页的只读 API；状态和 UTC 起止日期筛选；单轮事件列表及有界、全有或全无 JSON 导出。API DTO 不导出 `account_fingerprint`。
6. 当前健康快照从持久历史派生；监控页保留“本轮正在执行”的进程状态，历史页区分历史状态与当前 worker 状态。

## 非目标

- 不把全盘库存扫描 `cloud_scan_runs` 混作自动队列扫描；两者覆盖范围和含义不同。
- 不把错误原文、access token、secret、SignKey 或内部账号指纹暴露到 API/UI/export。
- 不新增自动任务重试、网盘写操作、自动放行 unknown 结果或历史删除入口。
- 不声称百度列表是原子快照；`cloud_observed_at_utc` 只表示本机收到该次目录响应的观察时间。
- 不以本机模拟验收替代真实百度或 NAS/SMB 行为。

## 方案与取舍

1. 仅恢复当前进程内存快照：改动小，但重启仍丢失结果和证据，不满足需求。
2. 把最近结果序列化到通用 app settings：能跨重启读取，但缺少每轮稳定身份、不可变事件、历史筛选和一致导出，容易把不同账号/不同时间语义混在一起。
3. 新建账号隔离的自动队列扫描摘要/事件表，沿用现有 SQLite 事务、keyset 游标、事件分页和显式 DTO 导出约定。推荐此方案：语义清晰，能审计中断，也不污染全盘库存扫描历史；代价是增加一次 SQLite schema 迁移和只读管理页/API。

本次采用方案 3。迁移只新增表、索引和 append-only 触发器，不回填无法证明的旧运行历史。旧版本没有队列扫描 run ID，因此历史从升级后开始。

## 状态与证据

- 摘要状态：`running`、`success`、`failed`、`interrupted`。
- 事件类型：`started`、`cloud_listed`、`completed`、`failed`、`interrupted`。
- 扫描开始先持久化 `started`。成功目录响应带 `cloud_observed_at_utc`；扫描完成/失败事件记录本机 `recorded_at_utc`。
- 错误只分类为固定 code：`authentication_wait`、`provider_error`、`queue_error`、`scan_interrupted`、`maintenance`、`already_running`。对外不返回底层错误文本。
- 维护/并发 skip 发生在没有实际扫描时，不创建 run 摘要，也不改最近一次扫描/成功时间；当前 worker 和维护状态单独显示。
- 进程重启发现遗留 `running` 时，只有在同账号启动下一次扫描的事务中才标为 `interrupted`，以避免启动时无法确定账号而错误改写其他账号历史。
- 事件摘要包含队列目录快照、状态/错误 code 和本地 UTC；不复制百度完整文件列表或 token。

## API 与 UI

- 登录保护的只读历史列表，按 `status`、`started_from_utc`、`started_through_utc` 筛选，按不可变 `sequence` cursor 稳定分页。
- 单轮事件按序 cursor 分页；run summary 与事件导出使用同一 SQLite 只读事务，最多 10,000 条事件，超限拒绝整份。
- 监控页增加“自动队列扫描历史”入口；独立历史页按需加载，不阻塞任务页轮询。显示队列目录、状态、起止本机 UTC、百度列表观察 UTC、耗时和事件类别；导出必须显式点击。
- API/UI 只显示标准错误 code 对应的安全中文提示，不显示敏感或不稳定的 provider 原文。

## 验收

- SQLite：schema 33→34 原子迁移、迁移中断重开、安全性/账号隔离、run 与 event 原子性、append-only、跨重启读回、旧历史不补造。
- Downloader/main：实际成功、认证失败、列表失败、reconcile 持久化失败均保留正确时间来源；maintenance/scan mutex skip 不更新成功；重启旧 running 转 interrupted。
- API/UI：参数拒绝、账号隔离、稳定分页与筛选、导出同快照/上限、错误文本与账号指纹不泄漏、迟到页面响应不覆盖当前选择、用户操作前不自动导出。
- 发布：WebUI、userscript、全仓 Go 测试/vet、Windows AMD64/Linux AMD64/Linux ARM64、ZIP/SHA/source snapshot/无运行数据检查。
- 外部边界：真实百度账号、真实浏览器及 NAS/SMB 未做本地验收时必须明确记录为未验收。
