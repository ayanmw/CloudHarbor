# 预览版版本管理规则

## 最近交付版本

### v0.8.30-preview.149（已交付：自动监控请求退避与重启恢复，5/5）

自动监控目录可重试失败遵循 `Retry-After` 与有界指数退避；重试截止按账号持久到 SQLite v41，跨重启延续；冷却内不调用百度；成功后清除退避；health 与任务页显示绝对重试 UTC。发布门禁通过：Go 全仓测试/vet、WebUI 172/172、userscript 3/3、三平台构建、ZIP 12 项载荷哈希与 491 项源码快照。真实状态单副本从 `.148` 移至 `.149`，SQLite 完整、队列为空、授权可用，启动自动监控只读列表成功。ZIP SHA-256 `D3E87C4AD29E3D5EF9F9D7080A96425F1C411A0260834FB31BB024181C58BE8D`；NAS/SMB 和真实限流环境未验收。详见 releases 同级验证记录。

### v0.8.30-preview.128（已交付：自动监控扫描历史与重启恢复，6/6）

1. SQLite 持久记录每次实际自动巡检，并按稳定的已绑定百度账号哈希隔离；令牌刷新不会切断历史。
2. 本机开始/结束/记录 UTC 与成功返回百度目录后的观察 UTC 分开保存；失败时不虚构云端观察时间。
3. 进程崩溃遗留的 `running` 在同账号下次扫描启动时标为 `interrupted` 并追加事件，不推断成功或重发网盘操作。
4. maintenance 或扫描互斥导致的 skip 明确返回，不创建成功记录或更新时间。
5. `/api/health` 从 SQLite 读取跨重启的最近扫描与最近成功时间；当前 worker 状态仍实时来自进程。
6. 新增只读历史页/API：状态与 UTC 日期筛选、稳定分页、事件详情分页和单轮显式 JSON 导出；不发起百度请求。

全仓 `go test ./... -count=1`、`go vet ./...`、WebUI 141/141、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 交叉构建、ZIP 13 项/12 项载荷哈希与 443 项源码快照均通过。真实账号只读验收已通过：唯一状态目录从 `.100` 移到 `.128`，25 个文件逐项 SHA-256 一致、旧目录已不存在；schema v24→v34 与 SQLite integrity `ok`；health `ok`、OAuth ready、任务/下载均为 0；真实监控目录扫描事件为 started → cloud_listed → completed，时间字段各自有 UTC 值。本轮只读目录列表，没有调用网盘写操作。Windows 与 WSL Linux AMD64 `--version` 正确；ARM64 未在原生设备执行。没有 NAS/SMB 验收。详细 SHA 与边界见 releases 同级 `v0.8.30-preview.128-validation.md`。

## v0.8.30-preview.129（已交付：自动监控健康状态与扫描历史一致性，5/5）

- **已完成 1/5：自动监控健康状态使用一致证据。** SQLite 同一只读事务读取最近运行与最近成功，避免完成瞬间拼出“运行中但已有本轮成功时间”；若本进程最新实际扫描失败但事件未写入，`/api/health` 仍显示降级和固定错误码，活动扫描不渲染过期错误，最近成功时间仍取自持久历史。
- **已完成 2/5：扫描历史暂时不可读时不再报告监控正常。** API 明确返回历史可读标记和固定错误分类；若使用进程内存中的最近成功时间，会标明来源为 runtime；两种任务页提示“扫描历史暂不可读”，不将内存证据表述为已持久化状态。Go 投影回归和 WebUI 回归通过。
- **已完成 3/5：内存回退的成功时间按 UTC 返回。** 本机时区时间先转换为 UTC 再进入 `last_successful_scan`，与页面的 UTC 标签一致；非 UTC 时区回归先失败后通过。
- **已完成 4/5：启动/恢复状态不会被旧成功时间覆盖。** 历史中存在未结束的 `running` 而本进程尚无活动 worker 时，任务页显示“目录监控正在启动”并同时保留最近成功 UTC，不再误显示正常。WebUI 回归先失败后通过。
- **已完成 5/5：慢健康请求不会按轮询周期叠加。** Poll/WebSocket 任务页共享单请求门禁；响应未完成时跳过后续定时 tick，完成后下一轮恢复。WebUI 回归先观察到 2 个并行请求，再验证最大并发为 1。

全仓 `go test ./... -count=1`、`go vet ./...`、WebUI 141/141、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建与 ZIP/source snapshot 门禁通过。真实账号、NAS/SMB 与 ARM64 原生运行的验收边界记录在 releases 同级 `v0.8.30-preview.129-validation.md`。

## v0.8.30-preview.130（网盘分页与目录缓存重核一致性，5/5 已交付）

1. 指定目录刷新遇到 provider 单页重复 `fs_id` 或 staging 跨页重复时，有界回读第一页；每目录上限两次、事件持久化和每次 GET 前额度预留保持不变。两种集成回归先失败后通过。
2. 实时网盘浏览遇到非首页重复身份时回锚第一页并展示既有 `boundary_changed` 提示；仅此类重复触发一次额外 GET，首页重复和其它错误保持失败。API 定向回归通过。
3. 目录刷新历史持久化重核原因码，区分上一页边界不匹配、百度单页重复 ID、本地暂存跨页重复 ID；UI 显示固定中文说明。SQLite v34→v35 加列迁移通过，旧历史不臆造原因。
4. 全盘扫描历史也持久记录并显示页边界变化/百度单页重复 ID 原因；SQLite v35→v36，加列迁移通过，旧事件保持空原因。
5. 实时列表回锚提示具体原因；API/UI使用固定原因码区分页首边界变化和百度单页重复 ID，不增加回读请求数。

发布门禁：WebUI 143/143、userscript 3/3、全仓 Go 测试/vet、Windows AMD64/Linux AMD64/Linux ARM64 构建、ZIP/SHA256/source snapshot 检查通过。首次全仓测试的 SQLite 故障注入测试受并发 DDL 影响；改为扫描启动前安装并限定第二页的触发器后，回归重复 10 次和全仓测试均通过。随后按用户授权将唯一真实状态目录移至 `.130`，schema v34→v36 与真实账号只读启动验收通过，未调用远端写操作；NAS/SMB 与 ARM64 原生运行未验收。最终包校验值见 releases 同级 `v0.8.30-preview.130-validation.md`。

已交付的全盘扫描边界重核、已有缓存历史/状态不重复计数。继续累计同一分页/缓存一致性链路的新独立用户结果，达到五条才打包；单纯重构和外部 NAS 环境不计入门槛。

### v0.8.30-preview.127（已交付：后台队列与监控可观测性，5/5）

1. 写入 `source_operation_sending` 失败时，恢复原任务状态、错误和重试截止；由于百度请求尚未发送，不显示结果未知或隔离源。
2. 成功扫描将任务判为 `source_missing` 时，如果事件未能写入 SQLite，恢复扫描前的状态、错误和重试截止。
3. 新任务分批写入时，后续批次失败后仍显示此前已成功提交的批次；失败批次未落库的任务不发布到内存列表。
4. 任务页轮询与 WebSocket 清楚区分故障停止和维护暂停：故障时提示并禁用暂停按钮；路径迁移/远端维护期间显示维护中，不再误称队列运行；具体错误仍从健康诊断查看。
5. 任务页展示扫描进行中/最近扫描结果和最近成功扫描 UTC；扫描错误码映射为固定文案，乱序返回的旧健康响应不会覆盖新结果。
6. 轮询版状态请求串行化定时刷新，慢请求不会按 2 秒周期无界叠加；请求失败或 WebSocket 断开时标记状态暂不可读并禁用队列控制，不保留旧的运行状态或误称队列已停止。

前三种持久化失败路径按共同根因合计一条；队列可观测性、云端扫描状态、定时轮询有界串行、断连后不展示陈旧队列控制状态分别计一条。Node 139/139、userscript 3/3、全仓 `go test ./... -count=1`、`go vet ./...`、Windows AMD64/Linux AMD64/Linux ARM64 构建、ZIP 13 项/12 项载荷哈希及源码快照核验均通过。详细 SHA-256 与边界见 releases 同级 `v0.8.30-preview.127-validation.md`。仅本机/SQLite/mock/UI 验收，没有真实百度账号或 NAS/SMB 实测。

### v0.8.30-preview.126（已交付：队列 SQLite 写失败状态投影一致性，5/5）

1. claim 事务失败恢复领取前的任务状态、次数、AttemptID、重试截止及容量信息。
2. 授权等待事件失败只回滚当前 job，不撤销本批已成功提交的历史。
3. 云端身份更新事件失败恢复旧 FileInfo 和调度状态。
4. 授权恢复事件失败保留 auth_wait，不显示未持久化的 pending。
5. 收尾事件保存失败不覆盖此前已持久化的暂停/取消/授权或 source 状态。

五项红绿回归及全套发布门禁已通过；ZIP/SHA-256/source snapshot 明细见 releases 同级 `v0.8.30-preview.126-validation.md`。设计与实施计划：`docs/superpowers/specs/2026-11-02-queue-persistence-failure-projection.zh-CN.md`、`docs/superpowers/plans/2026-11-02-queue-persistence-failure-projection.md`。

### v0.8.30-preview.125（已交付：任务暂停/取消状态与持久化回滚，5/5）

1. 目录暂停时将直接和嵌套未完成中断后代从 context-canceled failed 修正为 paused，保留 done、真实 failed 和相似前缀外任务。
2. 取消目录时将已暂停/本次中断的后代改为 cancelled，避免取消后继续显示 paused 或 failed。
3. Cancel 写 SQLite 失败恢复原内存投影。
4. Retry 写 SQLite 失败恢复原状态、尝试/失败计数、错误、截止时间和容量证据。
5. Cancel retry_wait 事件记录已移除的原重试截止 UTC。

全仓 Go 测试/vet、WebUI 133/133、userscript 3/3、三平台构建以及 ZIP/哈希/源码快照检查已通过；完整结果见 releases 同级 `v0.8.30-preview.125-validation.md`。设计与计划见 `docs/superpowers/specs/2026-11-02-directory-task-pause-consistency.zh-CN.md`、`docs/superpowers/plans/2026-11-02-directory-task-pause-consistency.md`。

### v0.8.30-preview.124（已交付：单个下载任务暂停与继续，5/5）

1. 单项 paused 状态保存到现有 SQLite 队列记录，进程重启后继续暂停。
2. 活动任务暂停先保存意图，再中断传输；保留 partial，恢复走 Range 与 SHA-256 完整性检查。
3. `retry_wait` 暂停/恢复保留原截止、失败数和尝试数；单项控制不解除全局暂停或 maintenance。
4. 管理员认证的任务 pause/resume API 返回权威状态与能力字段，unknown/confirmed source operation 拒绝改变。
5. Poll/WebSocket 提供单项暂停、继续、筛选和暂停计数；请求失败不乐观切状态。

设计与实施计划：`docs/superpowers/specs/2026-11-02-per-task-download-pause.zh-CN.md`、`docs/superpowers/plans/2026-11-02-per-task-download-pause.md`。

### v0.8.30-preview.123（已交付：下载队列暂停与恢复，5/5）

1. SQLite 持久化用户暂停状态和最后切换 UTC，重启继续暂停。
2. 暂停停止新任务领取，已有下载自然完成。
3. 定时/手动扫描继续对账，暂停期间的新项目保持 pending。
4. 管理 API 幂等控制状态，维护门禁与队列故障不能被恢复绕过。
5. Poll/WebSocket 页面一致显示暂停状态、活动数和显式控制。

设计与实施计划：`docs/superpowers/specs/2026-11-02-download-queue-pause.zh-CN.md`、`docs/superpowers/plans/2026-11-02-download-queue-pause.md`。

### v0.8.30-preview.122（下载空间诊断与恢复提示，5 项候选）

1. 失败任务空间摘要写入 SQLite 队列并跨重启读回，旧队列 JSON 兼容。
2. 任务列表区分下载前容量不足、容量不可读及写入时磁盘满。
3. Poll 与 WebSocket 任务页显示路径、required/available/reserved 精确字节和易读容量、本机核对 UTC；未知空间不渲染负值。
4. 诊断只保留手动 Retry，不自动请求、清理或重试。
5. SQLite 任务事件以专用卡片呈现容量证据，保留结构化导出和既有 provider/cloud/response 时间语义。

已交付。全仓 Go 测试/vet、WebUI 131/131、userscript 3/3、三平台构建与 ZIP/source snapshot 检查通过；具体 SHA-256 和边界见 releases 同级 `v0.8.30-preview.122-validation.md`。仅本机/SQLite/mock/UI，真实百度账号及 NAS/SMB 未验收。

## 发布门槛

每个新 preview 必须累计至少 **5 条彼此独立的功能、改进或 Bug 修复**，三类可混合计数。每项都必须能单独描述改动结果并有对应验收标准；把同一项拆成多个接口、文件或测试不增加计数。

同一根因的一组修复按 1 条 Bug 修复计；同一用户结果的一组小改动按 1 条功能/改进计。单纯重构、改文案、改构建脚本和内部性能整理不单独计入门槛；只有能说明用户可见结果并有验收证据的改进才计数。

未达到 5 条的需求和修复继续进入同一个未编号候选批次，不另开 preview。优先把同一主题和同一业务链路的工作合并规划、实现和验收，例如目录缓存的扫描、刷新历史、摘要和明细应作为一个连续的目录缓存批次评估，避免每个相邻小功能各发一版。候选批次记录主题、范围、独立事项清单及逐项验收证据；只有累计达到 5 条后才确定并递增版本号。验证副本、测试数据和临时构建目录不算发布版本。

## 同版本迭代

Preview 是一个可反复修正的候选版本。测试失败或验收发现问题时，修复源码后继续更新同一个 preview 版本号，并重复完整测试和打包检查；不得仅为规避测试失败而递增版本号。

当前候选版本达到发布门槛后，允许把已知但非阻断的问题列入下一批路线图；该问题必须保留在下一批范围和验收项中。影响数据安全、账号安全或可能误删/误移动远端内容的问题属于阻断项，不以“已知缺陷”名义带过。

更新已存在的 preview：

```powershell
.\scripts\package.ps1 -Version v0.8.30-preview.N -UpdatePreview
```

`-UpdatePreview` 仅接受 `-preview.N` 版本。脚本先在临时目录完成测试和构建；成功后才替换该预览目录中由打包脚本管理的程序文件和 ZIP。目录内其他数据（例如 `.nas-downloader`、下载文件和日志）会保留，ZIP 仅由干净的打包暂存目录生成，不包含运行数据。若文件正在运行而无法替换，脚本应失败并回滚；停止对应程序后再运行同一命令。未通过验证时不得覆盖现有包。

已发布的历史 preview 目录保留作为历史记录，不回溯合并、改名或删除；重写历史会使已交付包、校验值和验证记录失去对应关系。`.79–.97` 因此继续保留各自的版本号和内容。后续把同主题改进合并到未编号批次，达到 5 条后再交付一个新 preview。

复盘：`.87–.94` 都属于目录缓存刷新及其历史/证据能力，按现行规则在发布前应作为同一候选批次统筹，而不是连续拆成多个 preview。它们已经分别交付，所以仅统一归档到“目录缓存刷新”主题，不重写或替换既有包；后续该主题的新增需求继续累计在一个未编号批次中。

## 下一批路线图

| 候选版本 | 主题 | 主要范围 | 验收状态 |
| --- | --- | --- | --- |
| `v0.8.30-preview.148`（已交付，5/5） | 局部刷新分页漂移证据 | ①类型化捕获 provider/staging 重复 ID 并安全从首页重核；②记录原 offset；③记录确定的重复 fs_id；④固定原因筛选及组合分页；⑤事件页和整轮导出展示 nullable 证据、旧记录不造数 | 全仓 Go 测试/vet、WebUI 172/172、userscript 3/3、三平台 ZIP/SHA/source snapshot 与 `.147→.148` 唯一真实状态目录只读切换通过；详见 `v0.8.30-preview.148-validation.md` |
| `v0.8.30-preview.147`（已交付，5/5） | 跨任务全局事件历史 | ①管理员保护的只读全局事件页/API；②任务、类型、操作/尝试 ID、UTC 日期/精确范围组合筛选；③同事务匹配总数、类型计数和分页，未知类型归并 `other`；④全局序号固定跨页快照并精确支持 int64 与空快照；⑤最多 1,000 条递归脱敏 JSON 整份导出，schema v38→v39 仅新增查询索引 | 全仓 Go 测试/vet、WebUI/userscript、三平台构建、ZIP/SHA/source snapshot 和唯一真实状态目录只读切换通过；详见 `v0.8.30-preview.147-validation.md` |
| `v0.8.30-preview.146`（已交付，5/5） | 任务事件调查工作台 | ①`recorded_at_utc` 精确 UTC 左闭右开时间筛选；②同任务固定 5+锚点+5 上下文窗口；③同快照跨页事件类型计数与快捷筛选；④事件 UTC 当日快捷筛选；⑤复制服务端脱敏后的单条事件 JSON | 全仓 Go 测试/vet、WebUI/userscript、三平台构建、ZIP/SHA/source snapshot 与 `.145→.146` 唯一真实状态目录本机只读切换通过；详见 `v0.8.30-preview.146-validation.md` |
| `v0.8.30-preview.145`（已交付，5/5） | 任务事件关联 ID 筛选 | ①按 operation ID 精确筛选；②按 attempt ID 精确筛选；③从事件卡片聚焦 ID；④与类型/UTC 日期组合且跨页稳定；⑤组合条件有界导出 | 全仓 Go 测试/vet、WebUI 166/166、userscript 3/3、三平台构建及 ZIP/SHA/source snapshot 通过；详见 `v0.8.30-preview.145-validation.md` |
| `v0.8.30-preview.144`（已交付，5/5） | 下载任务事件筛选与证据导出 | ①固定真实事件类型筛选；②本机 UTC 日期范围筛选；③显示匹配总数；④分页/刷新保持条件并丢弃旧响应；⑤同一 SQLite 快照导出当前筛选，100 条上限、超限整份拒绝 | 全仓 Go 测试/vet、WebUI 165/165、userscript 3/3、三平台构建及 ZIP/SHA/source snapshot 通过；未执行百度请求或远端写操作，详见 `v0.8.30-preview.144-validation.md` |
| `v0.8.30-preview.143`（已交付，6/6） | 全盘扫描历史指定轮次对比 | ①跨页选择两轮完成记录；②按 sequence 排序并计算变化；③展示覆盖与九类原因差值/UTC；④历史 NULL 安全呈现；⑤限制两轮且支持清除；⑥显式 JSON 导出不新增请求 | 全仓 Go 测试/vet、WebUI 163/163、userscript 3/3、三平台构建及 ZIP/SHA/source snapshot 通过；真实账号只读启动核验通过，详见 `v0.8.30-preview.143-validation.md` |
| `v0.8.30-preview.142`（已交付，6/6） | 全盘扫描覆盖历史变化对比 | ①按账号和 sequence 关联紧邻前一完成轮；②覆盖状态/不完整目录差值；③九类原因变化，缺证据留空；④相邻 NULL 不跨越或推断；⑤列表及单轮导出包含前后快照和 UTC；⑥UI 显示改善、增加及不可比较原因 | 全仓 Go 测试/vet、WebUI 162/162、userscript 3/3、三平台构建、ZIP/SHA/source snapshot 与只读启动检查通过。详见 `v0.8.30-preview.142-validation.md` |`n| `v0.8.30-preview.141`（已交付，5/5） | 全盘扫描覆盖历史筛选 | ①按历史覆盖状态筛选含未记录；②未完整目录数最小/最大范围；③九类原因及原因未记录筛选；④与扫描状态/UTC 日期组合；⑤分页保留筛选、清除回第一页，固定枚举校验 | Store/API、全仓 Go/vet、WebUI 162/162、userscript 3/3、三平台构建、ZIP/SHA/source snapshot 通过。详见 `v0.8.30-preview.141-validation.md` |
| `v0.8.30-preview.140`（已交付，6/6） | 全盘扫描覆盖原因快照历史 | ①完成时持久化九类原因；②与完成状态、generation、UTC 原子提交；③v37→v38 旧记录留空；④历史列表和证据导出包含原因并排除账号指纹；⑤UI 区分历史原因与当前缓存并标注可重叠；⑥迁移、重启、回滚及全量发布验证 | taskstore/API/UI、全仓 Go/vet、WebUI 161/161、userscript 3/3、三平台构建、ZIP/SHA/source snapshot 通过；真实状态目录迁移、SQLite integrity、OAuth 与本地管理页只读检查通过。详见 `v0.8.30-preview.140-validation.md` |
| `v0.8.30-preview.139`（已交付，6/6） | 全盘扫描覆盖快照历史 | ①完成事务保存当时覆盖状态/未完整目录数/generation/本机 UTC；②完成事件失败回滚完整 run 与快照；③v36→v37 旧记录保留 NULL；④历史页分别标注历史快照与当前缓存；⑤证据导出包含快照；⑥运行和事件 API 不暴露账号指纹 | taskstore/API/UI、WebUI 161/161、userscript 3/3、全仓 Go/vet、三平台构建、ZIP/SHA/source snapshot 均通过；唯一状态目录迁移及真实账号只读启动核验通过。SHA 与边界见 `v0.8.30-preview.139-validation.md` |
| `v0.8.30-preview.133`（已交付，5/5） | 全盘扫描状态与当前缓存覆盖状态分开 | ①同一 SQLite 快照取得扫描运行、覆盖计数、generation、更新时间；②扫描完成但缓存仍 stale 时覆盖仍标为 incomplete；③running/complete/unknown 状态映射明确；④SQLite 读取失败时不返回部分覆盖数据，页面标注当前状态不可读并保留上次读取证据；⑤管理页分开显示运行状态和缓存覆盖且不发百度请求 | 全仓 Go 测试/vet、WebUI 148/148、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建及 ZIP 检查通过；真实账号只读切换通过。详情见 `v0.8.30-preview.133-validation.md` |
| `v0.8.30-preview.134`（已交付，5/5） | 缓存覆盖不完整原因分布 | ①同一 SQLite 快照统计扫描/目录状态；②区分缓存目录缺少状态行与状态孤项；③统计父目录未完整；④检测缓存路径关系与根目录覆盖缺失；⑤管理页说明原因计数及结构计数可重叠，状态查看不发百度请求 | 全仓 Go/vet、WebUI 148/148、userscript 3/3、三平台构建及 ZIP/SHA/source snapshot 检查通过；真实账号只读状态读取通过。详情见 `v0.8.30-preview.134-validation.md` |
| `v0.8.30-preview.135`（已交付，5/5） | 缓存覆盖不完整路径清单 | ①账号隔离的本地路径清单；②路径/原因筛选；③generation 绑定的稳定路径游标分页；④状态及 UTC 证据展示；⑤仅本地缓存导航与显式刷新路径填充，不隐式请求百度 | 全仓 Go/vet、WebUI 150/150、userscript 3/3、三平台构建及 ZIP/SHA/source snapshot 检查通过；真实账号只读清单及根目录 GET 通过。详情见 `v0.8.30-preview.135-validation.md` |
| `v0.8.30-preview.136`（已交付，6/6） | 缓存变化后自动更新不完整目录清单 | ①局部刷新完成自动更新；②成功远端变更后更新；③同 run 轮询去重；④保留原因/路径筛选并重置分页代次；⑤失败远端操作不误刷新；⑥清单重读只用本地 SQLite API | WebUI 152/152、userscript 3/3、全仓 Go/vet、三平台构建、ZIP/SHA/source snapshot 与真实账号只读健康/本地清单验收通过。详情见 `v0.8.30-preview.136-validation.md` |
| `v0.8.30-preview.137`（已交付，6/6） | 全盘扫描完成后更新不完整目录清单 | ①扫描 run_id 绑定完成收尾；②历史 complete 不误触发；③每轮扫描只更新一次；④暂停/失败不误报完成；⑤跨页面扫描可被观测；⑥仅已加载清单更新并保留筛选、回第一页 | WebUI 156/156、userscript 3/3、全仓 Go/vet、三平台构建、ZIP/SHA/source snapshot 和真实账号只读健康/本地清单验收通过。详情见 `v0.8.30-preview.137-validation.md` |
| `v0.8.30-preview.138`（已交付，7/7） | 缓存覆盖复核、导出与休眠恢复追赶 | ①原因计数；②一键快捷筛选；③当前页 JSON 导出；④全量筛选 JSON 导出；⑤同一 generation/total 校验、冲突整份中止；⑥10,000 条上限及凭据白名单；⑦挂起恢复后的扫描 generation 追赶 | WebUI 161/161、userscript 3/3、全仓 Go/vet、三平台构建、ZIP/SHA/source snapshot 和真实账号只读健康/本地清单验收通过。详情见 `v0.8.30-preview.138-validation.md` |
| `v0.8.30-preview.132`（已交付，5/5） | 局部缓存刷新完成后的浏览上下文一致性 | ①刷新运行绑定服务端根路径；②仅受影响缓存目录刷新后回第一页；③相关缓存搜索保留关键词并回第一页；④实时网盘视图/无关缓存目录不触发列表或大小读取；⑤旧运行完成响应不能改写当前浏览上下文 | 全仓 Go 测试/vet、WebUI 147/147、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建与 ZIP 检查通过；真实账号只读切换及根目录 GET 成功。SHA-256 与范围见 `v0.8.30-preview.132-validation.md` |
| `v0.8.30-preview.131`（已交付，5/5） | 本地缓存浏览分页代次一致性 | ①列表、总数、目录大小与 generation 同事务快照；②旧代次拦截并不返回旧项；③目录分页冲突后回首页；④搜索分页冲突后回首页；⑤目录/搜索/模式切换隔离迟到响应 | 全仓 Go 测试/vet、WebUI 145/145、userscript 3/3、三平台构建及 ZIP/source snapshot 检查通过；真实账号只读切换通过。ZIP SHA-256 与证据见 `v0.8.30-preview.131-validation.md` |
| `v0.8.30-preview.94`（已交付） | 目录刷新明细检索与导出 | 五项独立交付：按类型筛选、按路径/名称/fs_id 检索、未筛选分类计数、单目录有界筛选导出、复制路径/ID；不新增 schema 或远端操作 | 全仓 Go 测试/vet、WebUI 73/73、userscript 3/3、三平台构建通过；ZIP 13 项/12 项哈希、来源快照、无私有运行数据均已核验。SHA-256 与详情见 `v0.8.30-preview.94-validation.md` |
| `v0.8.30-preview.95`（已交付） | 网盘管理可靠性合并批次 | 入队记录稳定分页；手动入队和目录输入路径保真；目录读取失败/加载期间不留旧可选列表；主网盘管理页忽略迟到的列表/大小响应；本地迁移失败给出占用关闭程序和同路径重试指引 | 五条独立变化已实现并通过全仓 Go 测试/vet、WebUI 82/82、userscript 3/3、三平台构建；13 项 ZIP、12 项载荷哈希、源码快照和私有数据路径已核验。最终证据见 `v0.8.30-preview.95-validation.md` |
| `v0.8.30-preview.96`（已交付） | 本地路径迁移恢复可靠性 | 迁移复制中断恢复（完整目标清理残留 `.part`；同迁移 `.part` 哈希匹配源且目标为严格前缀时恢复；其他冲突保留，合并计一项）；Shutdown 等待 runner 退出后关闭 SQLite；删除隔离源目录前重验目标；隔离/清理阶段失败后安全重试；验证阶段关机取消后保留阶段并续跑 | 五项回归先失败后通过；全仓 `go test -count=1 ./...`、`go vet ./...`、WebUI 82/82、userscript 3/3、三平台构建、版本命令、13 项 ZIP/12 项载荷哈希和 342 项源码快照已核验。证据见 `v0.8.30-preview.96-validation.md`；未验收真实账号写操作及 NAS/SMB |
| `v0.8.30-preview.97`（已交付） | 只读请求限流与扫描恢复可靠性 | 六项独立改进：共享 API pacer 覆盖所有 metadata GET；限时解析/尊重 Retry-After；全盘扫描持久化重试 UTC 截止；局部刷新持久化重试 UTC 截止；成功后清除暂时错误；网页展示本机重试 UTC 与剩余等待；schema 22→23 | 全仓 Go 测试/vet、WebUI/userscript、Windows/Linux AMD64/Linux ARM64 构建与 ZIP/源码快照检查通过；哈希和范围见 `v0.8.30-preview.97-validation.md`。仅本机/mock，未访问真实账号/NAS |
| `v0.8.30-preview.98`（已交付，累计 5/5） | 百度只读请求恢复与重复内容哈希完整性 | ①重复扫描等待时尊重 provider Retry-After 并持久化 UTC 截止；②全盘/局部/重复扫描按当前墙钟每最多 30 秒重算截止且可取消；③云端 MD5 变化优先更新缓存、同事务清除旧 SHA-256 并拒绝并发旧候选回写；④ OAuth 账号 UserID 检查与 Downloader 共用请求 pacer；⑤只读 API 失败安全展示 Retry-After 响应头和等待秒数，不改状态码或写操作错误 | 全仓 Go 测试/vet、WebUI 83/83、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建、ZIP 13 项/12 项哈希、346 项源码快照均通过；验证摘要见 releases 同级 `v0.8.30-preview.98-validation.md`。真实账号只读启动验收通过；限流仅本机/mock，未做云端写操作或 NAS/SMB 验收 |
| `v0.8.30-preview.99`（已交付，5 项） | 大文件重复内容扫描断点恢复 | ①页面展示活动文件路径和文件内字节进度；②SQLite 保存当前候选身份、偏移和 SHA-256 状态；③暂停/进程重启后从已持久化 Range 偏移续算；④暂时网络失败按最后检查点续读且下载流 Retry-After 生效；⑤Range 忽略/响应身份变化时安全重算或失败关闭，并在单事务完成文件哈希与进度 | 全仓 Go 测试/vet、WebUI 83/83、userscript 3/3、三平台构建、ZIP 12 项哈希匹配和真实账号只读启动均通过。SHA-256 与验证边界见 releases 同级 `v0.8.30-preview.99-validation.md`；百度真实 Range 与 NAS/SMB 未验收 |
| `v0.8.30-preview.100`（已交付，5 项） | 重复扫描时线上对象变化对账 | ①成功元数据变化原子更新库存并保存百度观测 UTC；②清除旧 SHA、失效新旧父目录/大小代次并隐藏不完整重复预览；③元数据无唯一对象时保留缓存/哈希并暂停待刷新；④保持未完成候选游标，缓存刷新后重新估算剩余文件/字节；⑤追加旧/新对象与本机/百度时间证据，页面解释受影响路径和恢复方式 | 全仓 Go 测试/vet、WebUI 84/84、userscript 3/3、三平台构建通过；ZIP、源码快照和真实账号只读启动证据见 releases 同级 `v0.8.30-preview.100-validation.md`。百度 Range、远端写操作、大规模哈希和 NAS/SMB 未验收 |
| `v0.8.30-preview.101`（已交付，5 项） | 入队预检与路径维护一致性 | ①失效预检派生为 expired 且不再阻塞改路径；②入队/远端操作与路径维护读写准入互斥并固定目录快照；③旧历史按原路径跨重启读取和核对；④远端移动/删除保护使用准入后的当前路径；⑤连续迁移在准入后读取最新源目录 | 全仓 Go 测试/vet、WebUI 85/85、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 打包通过；ZIP 13 项，无私有运行数据；SHA-256 `1706704e204750ea56c52eeced26fc322e81b7299a2305f491e49c940525d1f6`。仅本机/mock，未验收百度远端写操作或 NAS/SMB |
| `v0.8.30-preview.102`（已交付，5 项） | 管理登录与路径操作可观测性 | ①网盘监控路径表单保留首尾空格；②回收过期登录失败地址，避免限流表满后永久拒绝新来源；③管理页 401 跳登录且保留返回地址；④扫描状态读取失败与“无记录”区分并保留最近成功状态；⑤迁移状态轮询防止重叠请求 | 全仓 Go 测试/vet、WebUI 90/90、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 打包通过；ZIP 13 项/12 项载荷校验、354 项源码快照、无私有运行数据；SHA-256 `108400a6e1e4f82ca5ae85f70704aae4a9d320b0df57b4b844d6b4cfe17fe328`。仅本机/mock，未验收百度远端写操作或 NAS/SMB |
| `v0.8.30-preview.103`（已交付，5 项） | 管理员会话失效后的跨页面恢复 | ①共享一次性登录跳转保留原路径/查询；②操作历史和目录刷新历史导出 401 处理；③入队 API 401 恢复；④OAuth 配置与授权 API 401 恢复；⑤任务页事件/操作/状态与扫描 401 恢复，并停止任务轮询、WebSocket 重连及云管理后台状态计时 | 全仓 Go 测试/vet、WebUI 100/100、userscript 3/3、三平台构建通过；ZIP 13 项/12 项哈希、359 行源码快照、无私有运行数据；发布细节、最终 ZIP 哈希与完整校验结果见 releases 同级 `v0.8.30-preview.103-validation.md`。仅本机验证，未验收百度写操作或 NAS/SMB |

| `v0.8.30-preview.104`（已交付，5 项） | 本地路径迁移与缓存扫描可靠性 | ①迁移检查点失败后按回执恢复；②路径维护与全盘/局部扫描准入竞态修复；③重启恢复重新核验路径别名冲突并持久记录失败；④严格核验 tombstone 与迁移身份，防止损坏回执删除目标目录；⑤源目录盘点后变化时迁移进度按实际复制量结算 | 全仓 Go 测试/vet、WebUI 100/100、userscript 3/3、三平台构建通过；ZIP 13 项/12 项哈希、356 项源码快照、无私有运行数据；最终校验值见 releases 同级 `v0.8.30-preview.104-validation.md`。仅本机/SQLite/mock，未验收真实百度写操作或 NAS/SMB |

| `v0.8.30-preview.105`（已交付，6 项） | 云目录刷新证据一致性 | ①分页项目按各自页面时间记录并跨重启保留；②目录完成时间仍独立使用末页响应时间；③旧活动暂存时间未知明确返回 null，不伪造历史；④UI 显示未知并区分本机记录/目录完成/逐项观察时间；⑤重复 fs_id 拒绝发布并保留原缓存；⑥schema 24→25 中断后可幂等恢复 | 全仓 Go 测试/vet、Web UI 100/100、userscript 3/3、三平台打包通过；ZIP 13 项/12 项载荷校验、358 项源码快照、无私有运行数据。最终 SHA-256 与完整结果见 releases 同级 `v0.8.30-preview.105-validation.md`。仅本机/SQLite/mock，未验收真实百度操作或 NAS/SMB |

| `v0.8.30-preview.106`（已交付，5 项） | 缓存扫描互斥与分页完整性 | ①全盘后续页拒绝重复 fs_id 并保留已提交页；②全盘扫描、局部刷新和重复哈希扫描共同准入且检查跨重启状态；③重复哈希扫描未完成时拒绝远端 mutation；④SHA-256 检查点绑定 provider MD5 防止续传拼接不同内容；⑤mutation 在扫描预留阶段即互斥，关闭状态持久化前的竞态 | 全仓 Go 测试/vet、Web UI 100/100、userscript 3/3、三平台构建通过；ZIP 13 项/12 项载荷校验、368 项源码快照、无私有运行数据。最终校验见 releases 同级 `v0.8.30-preview.106-validation.md`。仅本机/SQLite/mock，未验收真实百度操作或 NAS/SMB |
| `v0.8.30-preview.107`（已交付，5 项） | 缓存扫描恢复与分页身份完整性 | ①恢复扫描时准确显示 retry backoff 阶段；②全盘/局部/重复扫描跨重启保存下一档指数退避，schema 25→26；③重复扫描终止失败清理失效重试状态；④UI 分开显示最近实际等待与当前重试计划；⑤全盘扫描拒绝同页重复 fs_id 并事务回滚 | 全仓非缓存 Go 测试/vet、WebUI 101/101、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过；ZIP 13 项、12 项载荷 SHA-256 匹配、369 项源码快照、无私有运行数据。ZIP SHA-256 `bdcb91cfbb1d4a119f6d404feedba1a4fb83bf23eb96f14a48f37aba6f6cdc07`；完整记录 `v0.8.30-preview.107-validation.md`。仅本机/SQLite/mock，未验收真实百度操作或 NAS/SMB |
| `v0.8.30-preview.108`（已交付，5 项） | 后台扫描一致性与状态恢复 | ①终止失败清除失效退避计划；②持久化分页 lookahead 身份并检测可观察漂移，schema 26→27、旧无证据游标重置；③迁移后 manifest 落后时恢复启动；④云扫描状态轮询防重叠；⑤重复扫描/本地迁移状态临时读取失败时保留最近成功进度 | 全仓 Go 测试/vet、WebUI 104/104、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过；ZIP 13 项、12 项载荷 SHA-256 匹配、源码快照及无私有运行数据已核验。最终 SHA-256 和完整验证见 `v0.8.30-preview.108-validation.md`。仅本机/SQLite/mock，未验收真实百度操作或 NAS/SMB |
| `v0.8.30-preview.109`（已交付，5 项） | 目录缓存完整性证据 | ①全盘扫描不可重试失败时将当前运行占有的部分覆盖状态记为 failed；②明确“遍历完成”不等于远端原子快照；③目录大小双向校验目录身份与覆盖凭据；④搜索覆盖计数识别缺失或孤立目录状态；⑤全盘扫描提交 complete 前核验覆盖图闭合 | 全仓 Go 测试/vet、WebUI 104/104、userscript 3/3、三平台构建通过；ZIP 13 项/12 项载荷哈希匹配、371 项源码快照和私有运行数据排除检查通过。SHA-256 `bc235c9b7ae06ba4f05397350914b6dbeb1477c83695e7f90dde711ff8b6d6cd`；完整边界见 `v0.8.30-preview.109-validation.md`。未验收真实百度账号或 NAS/SMB |
| `v0.8.30-preview.110`（已交付，5 项） | 目录缓存覆盖、新鲜度与确认操作身份一致性 | ①覆盖图补父节点、根完成凭据和路径语义核验；②相同内容刷新仍更新时间证据并保留聚合代次；③终止失败原子清理 staging；④重启/暂停后恢复原目录刷新；⑤文件内容元数据变化时不沿用旧 provider MD5 | 全仓 Go 测试/vet、WebUI 105/105、userscript 3/3、三平台构建通过；ZIP 13 项/12 项载荷哈希匹配、371 项源码快照和运行数据排除检查通过；SHA-256 与边界记录见 releases 同级 `v0.8.30-preview.110-validation.md`。未验收真实百度账号或 NAS/SMB |
| `v0.8.30-preview.111`（已交付，5 项） | 目录缓存扫描恢复边界 | ①展示 worker_active 区分本进程任务与 SQLite 遗留 running；②局部刷新终止失败后可按原范围重启新 run；③全盘扫描与目录刷新共用 worker 槽并互斥；④重复 fs_id 失败后重置当前目录分页边界，支持从首页恢复；⑤SQLite 页提交失败时保留原有效 checkpoint | 全仓 Go 测试/vet、WebUI 109/109、userscript 3/3、三平台构建通过；ZIP 13 项/12 项载荷哈希匹配、371 项源码快照及运行数据排除检查通过；详见 releases 同级 `v0.8.30-preview.111-validation.md`。仅本机/SQLite/provider mock |
| `v0.8.30-preview.112`（已交付，5 项） | 实时网盘分页漂移核对与时间证据 | ①实时目录页携带 limit+1 lookahead 身份并核对下一页边界；②漂移时重新读取第一页并在界面提示重新定位；③拒绝缺少有效身份字段的 lookahead；④边界参数仅传 SHA-256 指纹，避免在 URL 暴露完整下一项路径/名称；⑤实时目录浏览与移动目标选择器显示本机读取完成 UTC | 全仓 Go 测试/vet、WebUI 116/116、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过；ZIP 13 项、12 项载荷 SHA-256、371 项源码快照和无私有运行数据已核验；最终 ZIP SHA-256 `A7F7587A45AEEEE8D12F74DDA3C0C564EA02D86C32D209364D87996D41554C9F`。完整本地验证见 releases 同级 `v0.8.30-preview.112-validation.md`。未完成新版真实账号启动验收；未验收 NAS/SMB |
| `v0.8.30-preview.113`（已交付，5 项） | 全盘扫描额度与历史审计 | ①指定目录刷新跨重启累计请求额度；②全盘扫描跨重启累计请求额度；③SQLite 保存全盘扫描多轮摘要及关键事件；④单轮摘要/事件一致快照 JSON 导出，最多 10,000 条且排除内部账号指纹；⑤按状态和开始 UTC 日期筛选并稳定分页 | 全仓 Go 测试/vet、WebUI 120/120、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过；ZIP 13 项、12 项载荷 SHA-256、386 行源码快照及私有数据排除检查通过；哈希与完整记录见 releases 同级 `v0.8.30-preview.113-validation.md`。仅本机/SQLite/mock，未验收真实百度操作或 NAS/SMB |
| `v0.8.30-preview.114`（已交付，6 项） | 远端操作重启恢复与本地一致性 | unknown/confirmed 结果重启后安全失效缓存并保留隔离；缓存失效失败可重试；状态持久化失败时当前进程仍隔离；恢复事件提供中文语义；确认删除回执恢复未执行队列项且不重发 | 完整打包门禁、WebUI 121/121、userscript 3/3、三平台构建通过；ZIP 13 项/12 项载荷哈希和 395 项源码快照匹配。SHA-256 与边界见 releases 同级 `v0.8.30-preview.114-validation.md`。仅本机/SQLite/mock，未执行真实百度写操作或 NAS/SMB |
| `v0.8.30-preview.115`（已交付，5 项） | 本地路径迁移恢复可靠性 | 隔离改名检查点恢复；部分清理用 SQLite 目标树指纹安全续跑；回退复制异常保留 `.part` 并按严格前缀修复；拒绝与变化后的显式启动配置冲突；相同路径表单提交可重试失败迁移 | WebUI 121/121、userscript 3/3、全仓 Go 测试/vet、三平台构建通过；ZIP 13 项/12 项载荷哈希匹配、397 项源码快照零差异。SHA-256 与逐项验收见 releases 同级 `v0.8.30-preview.115-validation.md`。仅本机/SQLite/mock，NAS/SMB 未验收 |
| `v0.8.30-preview.116`（已交付，5 项） | 本地路径迁移与运行配置恢复体验 | ①最终迁移完成检查点失败后可同进程重试；②启动配置冲突安全拒绝后解除维护门禁；③启动拒绝经 junction/symlink 指向 SQLite 状态目录的下载路径；④不迁移文件而改下载路径前确认文件仍留旧目录；⑤迁移运行时锁定路径表单、终态从服务端读回权威路径 | WebUI 123/123、userscript 3/3、全仓 Go 测试/vet、三平台构建通过；ZIP 13 项、12 项载荷哈希匹配、源码快照零差异、无私有运行数据。验证细节见 releases 同级 `v0.8.30-preview.116-validation.md`。仅本机/SQLite/mock，未验收真实百度账号或 NAS/SMB |
| `v0.8.30-preview.119`（已交付，5 项） | 局部目录刷新历史与请求额度审计补齐 | ①局部刷新运行摘要投影与旧事件回填；②请求额度预留同事务更新摘要；③独立重试计划事件及 UTC 截止/错误摘要；④额度耗尽专属事件且不超额请求；⑤恢复事件记录本次额度追加并在列表/页面/JSON 展示 | 全仓 Go 测试/vet、WebUI 126/126、userscript 3/3、三平台构建通过；ZIP 13 项、12 项载荷哈希匹配、406 项源码快照、无私有运行数据。SHA-256 与验收边界见 releases 同级 `v0.8.30-preview.119-validation.md`。仅本机 SQLite/mock/API/UI 验证，未验收真实百度账号或 NAS/SMB |
| `v0.8.30-preview.120`（已交付，5 项） | 重复文件扫描运行历史与证据 | ①schema 32→33 按账号/run 持久摘要及旧事件回填；②checkpoint、关键事件、摘要同事务且失败整体回滚；③状态/UTC 日期过滤与稳定游标分页；④显式导出有界筛选摘要 JSON，DTO 排除账号指纹；⑤选中 run 查看事件并显式导出单轮证据，脱敏、10,000 条全有或全无 | 全仓 Go 测试/vet、WebUI 129/129、userscript 3/3、三平台构建通过；ZIP 13 项/12 项载荷哈希匹配、410 项源码快照、无私有运行数据。SHA-256 和验收边界见 `v0.8.30-preview.120-validation.md`。仅本机 SQLite/mock/API/UI，未验收真实百度账号或 NAS/SMB |
| `v0.8.30-preview.121`（已交付，5/5） | 下载磁盘空间安全与恢复 | ①下载前按目标卷可用量准入；②并发 reservation 按剩余字节协调；③空间不足/容量不可读分类并记录本机路径、字节与 UTC；④ENOSPC 不自动重试、不发布且保留 partial；⑤释放空间后人工重试 Range 续传并 SHA-256 核验 | 全仓 Go 测试/vet、WebUI 130/130、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过；ZIP 13 项/12 项载荷哈希匹配、418 项源码快照。SHA-256 及边界见 releases 同级 `v0.8.30-preview.121-validation.md`。仅本机/mock/UI，NAS/SMB 未验收 |
| `v0.8.30-preview.122`（已交付，5/5） | 下载空间诊断与恢复提示 | ①任务快照持久化容量诊断并跨重启读取；②区分不足/不可读/写入时磁盘满；③任务列表显示目标路径及 required/available/reserved；④两种任务页安全展示本机 UTC；⑤事件时间线专用诊断卡、保留手动 retry 与原始证据 | 全仓 Go 测试/vet、WebUI 131/131、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过；ZIP 和源码快照、私有数据排除通过。SHA-256 与边界见 releases 同级 `v0.8.30-preview.122-validation.md`。真实百度/NAS/SMB 未验收 |

### v0.8.30-preview.110 发布清单与结果

`.110` 已交付，源码快照、ZIP 哈希和验证边界见 releases 同级 `v0.8.30-preview.110-validation.md`。本批主题为目录缓存覆盖完整性与新鲜度，共 5 项：

1. 覆盖图完整性检查补齐文件/目录父节点、`path`/`name`/`parent_path` 一致性与全盘根覆盖凭据；对应目录大小、缓存搜索和全盘扫描完成门禁。
2. 内容未变化的局部刷新同步目录大小缓存中的云端观察时间，同时保留聚合值、缓存代次和本地计算时间。
3. 局部刷新终止失败时，在失败状态事务中删除不能恢复或发布的 staging 行及待处理队列，同时保留此前已发布缓存。
4. 区分 SQLite 中持久化的 `running` 检查点与本进程实际活跃 worker；重启后无 worker，或主动暂停后仍有待处理目录时，页面显示待继续状态，并可按保存的原目录恢复。
5. 文件远端改名/移动的响应缺少 MD5 且 size/mtime 变化时，不沿用缓存中的旧 MD5；只有内容元数据未变化时才保留旧 MD5，避免把旧内容身份赋给新内容。

偏移分页只能承诺已实现的边界漂移检测，不能称为远端原子快照；百度真实账号与 NAS/SMB 验收仍单独记录。

### v0.8.30-preview.111（已交付）

v0.8.30-preview.111 聚焦“目录缓存分页漂移与恢复边界”，累计五项：①全盘扫描状态区分 SQLite 持久化 `running` 与本进程 worker，并提示无 worker 的检查点可恢复；②局部刷新终止失败后可用保存范围发起新 run，保留旧发布缓存，根目录仍需确认；③全盘扫描与局部刷新互斥，worker 活跃时禁用启动入口，旧 worker 退出前隐藏恢复/重试；④页内或跨页重复 `fs_id` 后清空全盘扫描当前目录失效 offset/边界并允许从首页恢复；⑤全盘/局部页提交采用候选 checkpoint，SQLite 事务失败时保留可恢复的旧边界，不留下无法恢复的 failed 或无 worker running 状态。全仓 `go test ./...`/`go vet ./...`、WebUI 109/109、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建通过；ZIP 12/12 载荷哈希和 371 项源码快照核验通过，无私有运行数据。最终哈希和验证边界见 `v0.8.30-preview.111-validation.md`。真实账号与 NAS/SMB 未验收。

### `.113` 批次形成过程（已达 5/5 并交付）

此处原先列出的指定目录/全盘扫描请求额度和扫描运行历史，是 `.113` 发布前的候选事项 1–3，现已由上方 `.113` 记录归档；不再作为后续未完成事项或重复计数。

- **已完成候选事项 1/5：指定目录刷新可设置跨重启累计列表请求额度**。新刷新设置总额度，恢复时追加；原不限额检查点可从当前已用数起设有限额度。每次百度列表 GET 前由 SQLite 耐久预留，额度耗尽后暂停并保留检查点。状态和事件记录额度快照。schema 27→28；全仓 Go 测试/vet、WebUI 116/116、diff 检查通过。只用本机 mock/SQLite，未在真实百度账号触发限流或额度耗尽。
- **已完成候选事项 2/5：全盘缓存扫描可设置跨重启累计列表请求额度**。全盘扫描页请求前耐久预留，失败 GET 计入；额度耗尽暂停并保留目录队列、当前页游标和边界，恢复时追加额度。新旧 SQLite schema 28→29，状态页显示额度用量。全仓 `go test ./... -count=1`、`go vet ./...`、WebUI 117/117、userscript 3/3 和 `git diff --check` 通过。仅本机 mock/SQLite，未触发真实百度额度耗尽。
- **已完成候选事项 3/5：保存全盘扫描运行历史与关键事件**。SQLite 分页保留每轮摘要，并仅追加开始/恢复/暂停/限额耗尽/计划重试/失败/完成等关键事件；状态摘要同事务更新，不逐页写事件。网页只读查看，证据含 UTC 时间、目录、错误和请求额度；schema 29→30。全仓 Go 测试/vet、WebUI 118/118、userscript 3/3、diff 检查通过。迁移不虚构升级前已经被覆盖的历史；仅 SQLite/mock，未验收真实百度或 NAS/SMB。

`.112` 已完成本机发布门禁并打包；ZIP 哈希及源码快照核验见 releases 同级 `v0.8.30-preview.112-validation.md`。真实账号服务仍运行 `.100`，停止/移动唯一运行目录的操作被运行环境策略阻止，因此不把 `.112` 的本机包验证表述为真实账号启动验收。

### v0.8.30-preview.117（已交付：监控目录切换与未执行队列对账，5/5）

`.116` 已交付并通过完整发布门禁，包与逐项验证见上方清单及 releases 同级 `v0.8.30-preview.116-validation.md`。用户已选择切换前展示受影响任务并确认。本版五项结果：①影响清单确认并在切换时清除旧目录调度资格；②大队列分页并让所有页共享完整摘要；③源缺失事件记录当时监控路径和云端观测 UTC；④百度列表页边界漂移时拒绝完整列表并阻止队列误判，且保留全量目录读取的大偏移能力；⑤保存后明确提示任务暂停至新目录成功扫描。完整 Go 测试/vet、WebUI 126/126、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 构建和包校验通过。最终 ZIP SHA-256、载荷/源码快照核对见 releases 同级 `v0.8.30-preview.117-validation.md`。没有真实百度账号操作或 NAS/SMB 验收。

当前边界：预算只限制用户手动启动的目录及子树刷新，不限制全盘扫描或实时浏览；额度在百度列表 GET 前耐久预留，进程若在 HTTP 发送前崩溃，该次额度不会返还。额度不是百度速率限制，也不提供远端快照保证。真实百度与 NAS/SMB 验收仍分开记录。
| `v0.8.30-preview.53`（历史交付） | 网盘管理与全盘目录缓存 | 网盘文件浏览及远端管理；自动监控/本地下载路径设置与迁移；可续扫的全盘列表缓存和目录大小；重复文件与空目录预览/清理；扫描节流、持久化恢复和关键操作证据 | 功能实现、回归和 v0.8.30-preview.53 测试包已交付；ZIP 载荷校验 12/12。真实账号 CRUD 已用自建空目录验证并确认清理，之后全盘重扫 983 页/8,050 项完成；33 个既有空目录候选只预览未删除。剩余为用户目标环境的 NAS/SMB 迁移/重启恢复验收，以及需页面确认才会启动的约 120 GiB 重复内容扫描 |
| `v0.8.30-preview.54`（候选包已交付） | 已确认网盘变更即时刷新本地清单与目录大小 | 完整且未冲突的 SQLite 缓存上，对确认成功的 mkdir、rename、move、delete 事务性更新对象记录、目录子树、时间证据和 generation；缓存不完整/unknown 时继续失效并重扫/隔离；扫描游标与受影响路径冲突时在远端写入前拒绝；缓存刷新失败以独立 SQLite 事件持久记录原因和时间，不覆盖远端 confirmed | 全仓测试、vet、跨平台构建与 ZIP 检查通过；真实账号有限 CRUD 已 confirmed 并清理，独立空缓存按设计产生刷新 warning。post-CRUD 只读全盘重扫完成：983 页/8,050 项，976 条目录状态 complete，根目录 2,407,342,072,250 bytes，SQLite 完整性 ok。包内最终文档快照在本轮同版本重打包中更新。满足一个完整用户可见功能门槛 |
| `v0.8.30-preview.55`（已交付） | 按目录刷新缓存 | 指定目录及其后代独立扫描和持久化续跑；不改账号级全盘扫描状态；覆盖目录的远端写操作在 provider 写入前拒绝；页面显示独立进度、允许暂停并继续浏览 | `go test ./...`、`go vet ./...`、WebUI 22 项、userscript 3 项、三平台构建通过；真实账号只读刷新 `/NAS_Download_Queue_preview` 完成，1 页/0 项，大小 complete、0 bytes；ZIP 13 项，12 项清单哈希全部匹配，无私有运行数据。ZIP SHA-256：`ea8fa326a76360fce22cf0ba05f40a275b867cd045d3afedd167af54fa3347e8` |
| `v0.8.30-preview.56`（已更新交付） | 本地缓存浏览 | 显式切换实时/缓存列表；缓存浏览显示覆盖状态、观测时间和只读目录内容；全盘缓存直接提供当前页目录的本地聚合大小及完整性 | 首包后在同一 `.56` 内补全目录大小遗漏，未创建过小的新 preview。缓存列表响应只增加可选字段，复用 generation-scoped SQLite 聚合缓存；UI 对不完整覆盖明确标记。Store/WebUI 回归先失败后通过，全量 Go 测试/vet、WebUI 23 项、userscript 3 项、Windows AMD64/Linux AMD64/Linux ARM64 构建通过。真实账号只读核对：OAuth ready、任务 0、监控目录 0 项、缓存 complete 51 项/39 个目录行大小完整；对用户提及的 `/AI量化交易第1期` 再做入队预检返回 HTTP 200/ready，未移动或下载。ZIP 与 SHA-256 见独立 releases 目录 |
| `v0.8.30-preview.57`（同版本更新交付） | 本地缓存全盘搜索 | 按名称或路径搜索账号隔离的 SQLite 缓存；分页显示缓存覆盖状态；结果仅可跳转到实时父目录重新读取；排除自动监控队列目录及子树的空目录清理候选 | 搜索回归覆盖账户隔离、分页、覆盖不足、SQL 字面字符和不触发实时网盘请求。`.57` 真实页面搜索 `CASE-text-to-sql` 返回 17 项；点击结果切到实时模式并加载父目录 4 项。空目录候选为 32 项且不含监控队列子树。真实账号 6 笔随机 CRUD 均 confirmed，清理后实时根目录无残留；同版本修复目录元数据变化导致本地缓存拒绝增量删除的问题。全盘重扫 983 页/8,050 项完成，cache complete、未完成目录 0、SQLite integrity ok。全量 Go 测试/vet、WebUI 24 项、userscript 3 项、Windows AMD64/Linux AMD64/Linux ARM64 构建通过。ZIP SHA-256 和详细证据见 releases 目录 `v0.8.30-preview.57-validation.md` |
| `v0.8.30-preview.58`（已交付） | 已确认网盘改名/移动/删除同步待执行队列 | 以稳定 `fs_id` 更新尚未开始任务的远端路径/元数据；确认删除时记录专用取消事件；保留下载模式、创建时间与重试历史；活动、已尝试、终态或 source-operation 未知任务不重写，避免相同 fs_id 元数据变化时重建任务 | 来源为已批准网盘管理规格中未完成的一项（原实施计划注明“待执行任务路径尚未按 fs_id 重写”）。新增回归先失败后通过；全 downloader 无缓存测试、全仓测试/vet、WebUI 24 项、userscript 3 项、Windows AMD64/Linux AMD64/Linux ARM64 构建通过。ZIP 载荷校验 12/12、私有数据 0、包内版本检查通过。SHA-256 与源码快照见 releases 目录 `v0.8.30-preview.58-validation.md` |
| `v0.8.30-preview.59`（已交付） | SQLite 远端操作历史只读视图 | 网盘管理页按状态筛选和分页浏览操作，按需查看追加事件与 UTC 时间；敏感证据递归掩码，unknown 明确隔离且没有重发/放行入口 | Store/API/WebUI 回归通过；全仓测试、vet、WebUI 25 项、userscript 3 项及三平台构建通过。包内版本/来源快照/清单和 SHA-256 见 releases 目录 `v0.8.30-preview.59-validation.md`；真实账号跨重启历史读取只做只读冒烟 |
| `v0.8.30-preview.60`（已交付） | SQLite 远端操作历史检索 | 按 operation ID/object ID/源目标路径关键字与 UTC 时间范围搜索；保持 SQLite-only、有界分页和现有只读权限 | Store/API/UI 功能完成；全仓测试、vet、WebUI 27 项、userscript 3 项和三平台打包通过。真实账号筛选 GET 分别按 ID、fs_id、路径和 UTC 时间命中预期结果，未执行云端写操作。ZIP 与源码快照 SHA-256 见 releases 目录 `v0.8.30-preview.60-validation.md` |
| `v0.8.30-preview.61`（已交付） | 远端操作历史摘要导出 | 沿用当前状态/关键字/UTC 时间筛选，在单个 SQLite 只读事务快照导出 JSON；上限 5,000 条，超限拒绝整批；不含事件证据或账号指纹 | 全仓测试/vet、WebUI 29 项、userscript 3 项、三平台包门禁、ZIP 清单/源码快照通过；真实账号受保护 GET 导出 19 条、精确 ID 筛选 1 条，无云端写操作。SHA-256 与记录见 `v0.8.30-preview.61-validation.md` |
| `v0.8.30-preview.62`（已交付） | 单条远端操作脱敏证据导出 | 从历史记录导出一笔操作摘要和完整追加事件；使用 SQLite 一致快照、最多 100 条事件及与现有事件 API 相同的递归敏感字段遮蔽；第 101 条出现时拒绝整份文件 | Store/API/UI 全路径、全仓 Go 测试/vet、WebUI 32 项、userscript 3 项及三平台打包通过；真实账号只读 GET 导出两条升序事件并验证递归脱敏。SHA-256 与记录见 `v0.8.30-preview.62-validation.md` |
| `v0.8.30-preview.63`（已交付） | 本地目录迁移路径别名隔离 | 解析已存在的 junction/符号链接父目录后校验迁移源、目标与 SQLite 状态目录边界；普通下载路径更新也执行状态目录物理路径检查，拒绝在受保护目录中建目录或迁移 | junction 定向回归覆盖迁移及非迁移设置；全仓 Go 测试/vet、WebUI 32 项、userscript 3 项和 Windows AMD64/Linux AMD64/Linux ARM64 打包通过。ZIP SHA-256 与校验记录见 `v0.8.30-preview.63-validation.md`。NAS/SMB 占用重试与中断恢复仍需目标环境验收 |
| `v0.8.30-preview.64`（已交付） | 只读错误诊断与历史错误本地化 | 只读云端 GET 失败时显示可安全重试提示；远端操作历史列表、导出及事件读取失败时显示中文提示且不泄露底层错误；写操作未知结果仍明确禁止盲目重发 | 两项独立用户可见改进已实现并有 API 回归；全仓 Go/vet、WebUI 32 项、userscript 3 项、三平台构建和 ZIP 检查通过。NAS/SMB 迁移、占用重试和中断重启恢复继续列为外部环境验收项，本机盘测试不作替代 |
| `v0.8.30-preview.65`（已交付） | 远端操作历史按类型筛选 | 在本地 SQLite 历史中筛选新建目录、移动、重命名、删除；列表与摘要导出共用当前条件；拒绝未支持类型 | Store/API/UI 定向回归、全仓 Go/vet、WebUI/userscript、三平台构建和 ZIP 检查通过。校验值见 `v0.8.30-preview.65-validation.md` |
| `v0.8.30-preview.66`（已交付） | 本地目录迁移证据导出 | 在 SQLite 一致只读快照中导出最近一次本地目录迁移摘要与有界 UTC 事件证据，供 NAS/SMB 故障核对；不读写目录、不访问网盘 | Store/API/UI、跨重开读取、超限拒绝、敏感字段遮蔽及三平台发布门禁通过。校验值见 `v0.8.30-preview.66-validation.md` |
| `v0.8.30-preview.67`（已交付） | 历史迁移记录浏览 | 在网页分页浏览全部本地迁移摘要，并选择任一条记录导出对应完整证据；仅本地 SQLite 只读 | 全仓 Go/vet、WebUI 34/34、userscript 3/3、三平台构建与 ZIP 检查通过；SHA-256 和边界见 `v0.8.30-preview.67-validation.md` |
| `v0.8.30-preview.68`（已交付） | 本地迁移历史检索 | 按迁移状态、源/目标路径关键字检索 SQLite 历史并保留稳定分页；结果只读，记录导出仍绑定迁移 ID | 全仓 Go/vet、WebUI 35/35、userscript 3/3、三平台构建与 ZIP 检查通过；SHA-256 和边界见 `v0.8.30-preview.68-validation.md` |
| `v0.8.30-preview.69`（已交付） | 筛选结果摘要导出 | 将迁移历史当前状态/路径筛选结果按单一 SQLite 快照导出为有界 JSON 摘要；不包含事件，逐条证据导出保留 | 全仓 Go/vet、WebUI 36/36、userscript 3/3、三平台构建和 ZIP 检查通过；SHA-256 与验收边界见 `v0.8.30-preview.69-validation.md` |
| `v0.8.30-preview.70`（已交付） | 本地迁移事件时间线 | 在历史记录中展开查看单条迁移的 UTC 状态事件，分页加载并对证据敏感字段遮蔽；仍为只读 SQLite | 全仓 Go/vet、WebUI 37/37、userscript 3/3、三平台构建和 ZIP 检查通过；SHA-256 与验收边界见 `v0.8.30-preview.70-validation.md` |
| `v0.8.30-preview.71`（已交付） | 按 UTC 日期筛选迁移历史 | 按开始 UTC 日期起止范围筛选，复用状态/路径条件、稳定分页及摘要导出；严格校验日期与起止关系 | 全仓 Go/vet、WebUI 38/38、userscript 3/3、三平台构建和 ZIP 检查通过；SHA-256 与验收边界见 `v0.8.30-preview.71-validation.md` |
| `v0.8.30-preview.72`（已交付） | 独立只读远端操作历史页 | 从网盘管理页直达历史专页；复用 SQLite 分页、筛选、摘要/事件证据导出，不提供网盘或历史变更入口 | 登录保护路由、WebUI 40/40、userscript 3/3、全仓 Go/vet、三平台构建和 ZIP 检查通过；SHA-256 与边界见 `v0.8.30-preview.72-validation.md` |
| `v0.8.30-preview.73`（已交付） | 远端操作事件按状态筛选 | 单笔事件时间线按状态筛选，sequence 游标继续限定 operation ID 与筛选条件；只读 SQLite | 全仓 Go/vet、WebUI 41/41、userscript 3/3、三平台构建和 ZIP 检查通过；SHA-256 与边界见 `v0.8.30-preview.73-validation.md` |
| `v0.8.30-preview.74`（已交付） | 远端操作事件 UTC 日期筛选与筛选证据导出 | 单笔事件按 UTC 起止日和状态筛选；分页沿用条件；单 SQLite 快照导出命中证据，最多 100 条；读取错误时保留筛选控件 | 全仓 Go/vet、WebUI 43/43、userscript 3/3、三平台构建与 ZIP 检查通过；SHA-256 和外部验收边界见 `v0.8.30-preview.74-validation.md` |
| `v0.8.30-preview.75`（已交付） | 单任务事件证据下载 | 对指定任务的全部本地事件做一致快照导出；最多 100 条，超限整份拒绝；递归脱敏；任务列表时间线提供用户点击下载 | 全仓 Go/vet、WebUI 45/45、userscript 3/3、三平台构建与 ZIP 检查通过；SHA-256 和证据见 `v0.8.30-preview.75-validation.md` |
| `v0.8.30-preview.76`（已交付） | 下载任务列表状态筛选与路径搜索 | WebSocket/轮询页复用服务器端状态与云端路径/文件名过滤，保留分页/排序/自动刷新条件；命中总数与全队列统计分开 | 全仓 Go/vet、WebUI 47/47、userscript 3/3、三平台构建和 ZIP/源码快照校验通过；SHA-256 `743C64CC3B78CB6F6CD294026A09C7F2DB634E161D47316DDA6B78E13A728B25`，详见 `v0.8.30-preview.76-validation.md` |
| `v0.8.30-preview.77`（已交付） | 只读网盘请求失败诊断 | 手动添加预检和目录读取对 DNS/超时/连接失败提供固定安全提示；HTTP/百度错误码保留；unknown 写操作策略不变 | 全仓 Go/vet、WebUI 48/48、userscript 3/3、三平台构建和 ZIP/源码快照校验通过；SHA-256 `A5B22C137CB164D6195C329E782B982FF672E79D5CF5A5BC5316B7BFB524C641`，详见 `v0.8.30-preview.77-validation.md` |
| `v0.8.30-preview.78`（已交付） | 网盘移动目标目录选择器 | 通过实时目录列表分页选择目标；仅在显式确认后调用既有移动接口；取消、失败和过期列表响应不触发写操作 | 全仓 Go/vet、WebUI 51/51、userscript 3/3、三平台构建通过；ZIP 载荷 12/12 匹配、无私有运行数据；SHA-256 和验证边界见 `v0.8.30-preview.78-validation.md` |
| `v0.8.30-preview.79`（已交付） | 重复文件预览选择保留副本 | 用户可为每个重复组临时指定保留项；刷新预览恢复服务端默认；不新增远端调用，删除仍需确认并复核 SHA-256 | 全仓 Go/vet、WebUI 53/53、userscript 3/3、三平台构建及 ZIP/源码快照/敏感数据核验通过；SHA-256 与边界见 `v0.8.30-preview.79-validation.md` |
| `v0.8.30-preview.80`（已交付） | 重复文件预览分页 | 每页渲染 25 个重复组；跨页保留当前预览选择；全量批量删除仍覆盖全部预览组；不改变 API/远端语义 | 全仓 Go/vet、WebUI 54/54、userscript 3/3、三平台构建及 ZIP/源码快照/敏感数据核验通过；验证记录见 `v0.8.30-preview.80-validation.md`。仅降低 DOM 渲染量，不降低 API 响应体和内存数据量 |
| `v0.8.30-preview.81`（已交付） | 重复文件预览服务端分页 | SQLite/API 每次返回最多 25 组和完整计数；跨页 keeper 选择；批量删除逐组从第一页重读，逐项重新核验，错误/unknown/计数变化停止 | 全仓测试/vet、WebUI 56/56、userscript 3/3、三平台构建、ZIP 载荷 12/12 和私有数据检查通过；验证见 `v0.8.30-preview.81-validation.md`。未做真实账号删除 |
| `v0.8.30-preview.82`（已交付） | 重复扫描事件历史 | 复用 SQLite append-only 状态事件，新增账号隔离的只读 API 及按需分页时间线；不改扫描和远端操作 | 全仓 Go 测试/vet、WebUI 59/59、userscript 3/3、三平台构建及 ZIP/来源快照/私有数据检查通过；见 `v0.8.30-preview.82-validation.md` |
| `v0.8.30-preview.83`（已交付） | 远端操作历史稳定游标分页 | SQLite/API 按 `(created_at_utc, operation_id)` keyset 分页；历史页游标前进/返回；旧 offset API 兼容，新增顶部记录不改变已访问页边界 | 全仓 Go 测试/vet、WebUI 60/60、userscript 3/3、三平台构建及 ZIP 载荷 12/12、源码快照和私有数据检查通过；见 `v0.8.30-preview.83-validation.md`。总数实时变化，状态筛选不冻结既有记录状态 |
| `v0.8.30-preview.84`（已交付） | 本地缓存搜索结果导航 | 文件结果可打开缓存父目录，目录结果可直接打开自身；保留单独的实时目录核对入口和缓存覆盖/观测提示，缓存入口不调用百度 API | 全仓 Go 测试/vet、WebUI 60/60、userscript 3/3、Windows AMD64/Linux AMD64/Linux ARM64 打包与 ZIP 载荷/源码快照/私有数据检查通过；详见 `v0.8.30-preview.84-validation.md` |
| `v0.8.30-preview.85`（已交付） | 重复扫描文件/字节级持久进度 | SQLite 持久记录已完成校验文件与字节，续跑正确合并已完成和剩余量，页面同时展示两种进度；schema 16→17 原子迁移 | 全仓 Go 测试/vet、WebUI 61/61、userscript 3/3、三平台构建通过；ZIP 载荷 12/12、源码快照和私有数据检查通过；详见 `v0.8.30-preview.85-validation.md`。未执行真实账号大扫描或远端删除 |
| `v0.8.30-preview.86`（已交付） | 目录缓存增量核对与在线冲突修正 | 刷新期间保留最后完整目录快照，完整读取后核对并只发布变化；未变化目录复用身份匹配的 SHA-256；confirmed 远端结果与旧缓存冲突时按线上结果补偿缓存并记录 UTC 证据 | 全仓 Go 测试/vet、WebUI 61/61、userscript 3/3、三平台构建及 ZIP/源码快照/私有数据检查通过；详见 `v0.8.30-preview.86-validation.md`。只做本地/mock，未访问真实账号 |
| `v0.8.30-preview.87`（已交付） | 目录缓存刷新可审计历史 | SQLite append-only 事件记录开始/恢复/暂停/失败/完成和逐目录发布；同一事务保证状态/缓存与事件一致；认证只读 API 稳定游标分页；管理页按需查看本机 UTC 与百度目录列表读取完成 UTC | 全仓 Go/vet、WebUI 63/63、userscript 3/3、三平台构建、ZIP 13 项/12 项载荷校验和私有数据路径检查通过；SHA-256 与来源快照核验见 `v0.8.30-preview.87-validation.md`。仅本地/mock，不访问真实账号 |
| `v0.8.30-preview.88`（已交付） | 目录缓存刷新运行历史 | 按授权账号列出曾启动的刷新运行，按不可变启动事件序号稳定分页；显示最新事件摘要，选择运行后复用事件时间线；新增只读 API/UI 与 SQLite 19→20 索引迁移 | 全仓 Go/vet、WebUI 63/63、userscript 3/3、三平台构建、ZIP 13 项/12 项载荷校验和私有数据检查通过；哈希与快照见 `v0.8.30-preview.88-validation.md`。仅本地/mock |
| `v0.8.30-preview.89`（已交付） | 单轮刷新运行证据导出 | 为选定运行导出同一 SQLite 只读快照中的摘要和完整事件，限制 100 条并整份拒绝超限；账号隔离，不导出指纹，用户显式下载 | 全仓 Go/vet、WebUI 65/65、userscript 3/3、三平台构建、ZIP 13 项/12 项载荷和私有路径检查通过；见 `v0.8.30-preview.89-validation.md`。仅本地/mock |
| `v0.8.30-preview.90`（已交付） | 刷新运行历史筛选 | 按运行最新状态及启动 UTC 日期范围筛选；SQLite 服务端筛选，保持不可变启动序号游标，调整/清除筛选重置第一页 | 全仓 Go/vet、WebUI 66/66、userscript 3/3、三平台构建、ZIP 13 项/12 项载荷和私有路径检查通过；见 `v0.8.30-preview.90-validation.md`。仅本地/mock |
| `v0.8.30-preview.91`（已交付） | 导出筛选后的刷新运行摘要 | 使用当前状态/UTC 日期筛选导出最多 5,000 条运行摘要；同一 SQLite 只读快照，超限拒绝整份，显式下载 | 全仓 Go/vet、WebUI 67/67、userscript 3/3、三平台构建和 ZIP 检查通过；详见 `v0.8.30-preview.91-validation.md` |
| `v0.8.30-preview.92`（已交付） | 目录刷新变更摘要 | 按稳定 fs_id 对比完整旧缓存与本次完整直系清单，记录新增/移除/元数据变化计数；不完整基线为未知；刷新历史和 JSON 证据展示；schema 20→21 原子升级 | 全仓 Go/vet、WebUI 68/68、userscript 3/3、三平台构建、ZIP 13 项/12 项载荷及源码快照检查通过；详见 `v0.8.30-preview.92-validation.md`。仅本地/mock |
| `v0.8.30-preview.93`（已交付） | 目录刷新逐项变更明细 | 在完整旧基线上保存新增、删除、元数据变化及唯一同路径身份变化的前后快照；记录本机/百度 UTC 与身份匹配的 SHA-256；明细与目录发布事务提交，页面按需分页查看；附事件类型筛选导航；schema 21→22 | 全仓 Go/vet、WebUI 70/70、userscript 3/3、三平台构建、ZIP 13 项/12 项载荷、来源快照和私有数据核验通过；详见 `v0.8.30-preview.93-validation.md`。仅本地/mock，未访问真实百度/NAS |

`.53`–`.63` 的逐版实现和验证证据集中记录在 `docs/ai/progress.md`。`.59` 完成 SQLite 操作历史查看，`.60` 增加 SQLite 本地关键字和 UTC 时间范围检索；unknown 结果仍隔离。后续新 preview 至少累计 5 条独立功能/改进/Bug，按同主题合并候选批次后再编号。NAS/SMB 迁移、占用重试和中断恢复仍需目标环境验收；约 120 GiB 重复扫描和 32 个空目录删除仍需用户在页面审阅并明确确认。

`.77` 不替代目标 NAS/SMB 验收；包内验证记录说明这些外部验收仍未完成。2026-10-30 已按用户授权核对队列空闲后，将唯一 `.nas-downloader` 从 `.63` 移动到 `.77` 并完成真实账号只读启动验收，没有复制授权数据。

截至 `.112` 已交付；各包构建、ZIP 校验和范围证据见对应 validation 记录。本轮真实账号进程仍运行 `.100`，健康状态为 `total_tasks=0`、`downloading=0`、`pending=0`、`source_unknown=0`。尝试按既有授权停止 `.100` 并将唯一 `.nas-downloader` 移至 `.112` 时，运行环境策略拒绝了停止/移动命令，因此未改变运行数据目录、未重启服务，也未对 `.112` 做真实账号验收。未执行云端写操作、下载或大规模哈希扫描。百度下载节点 Range 行为和 NAS/SMB 迁移、长任务恢复仍未实机验收；重复文件扫描和空目录删除仍需用户在页面审阅并明确确认后执行。



